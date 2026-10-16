package vm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"golang.org/x/crypto/ssh"
)

const (
	defaultCols    = 80
	defaultRows    = 24
	mgmtIPTimeout  = 30 * time.Second
	sshDialTimeout = 30 * time.Second
	retryInterval  = 500 * time.Millisecond
)

func (p *Provider) OpenTerminal(ctx context.Context, sb runner.SandboxID, node string) (runner.PTY, error) {
	attempt := string(sb)
	name := domainName(p.cfg.Instance, attempt, node)
	ip, err := p.mgmtIP(ctx, name)
	if err != nil {
		if isDomainNotFound(err) {
			return nil, p.missing(ctx, sb, node)
		}
		return nil, err
	}

	dir := sandboxDir(p.cfg.Dir, p.cfg.Instance, attempt)
	config, err := sshConfig(dir)
	if err != nil {
		return nil, err
	}
	client, err := dialSSH(ctx, net.JoinHostPort(ip, "22"), config)
	if err != nil {
		return nil, fmt.Errorf("ssh to %s (%s): %w", node, ip, err)
	}
	t, err := startShell(client)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("start shell on %s: %w", node, err)
	}
	p.logFor(attempt).Info("opened terminal", "node", node, "address", ip)
	return t, nil
}

func sshConfig(dir string) (*ssh.ClientConfig, error) {
	keyPEM, err := os.ReadFile(filepath.Join(dir, userKeyFile))
	if err != nil {
		return nil, fmt.Errorf("read sandbox ssh key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(keyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse sandbox ssh key: %w", err)
	}
	hostLine, err := os.ReadFile(filepath.Join(dir, hostKeyFile))
	if err != nil {
		return nil, fmt.Errorf("read sandbox host key: %w", err)
	}
	hostKey, _, _, _, err := ssh.ParseAuthorizedKey(hostLine)
	if err != nil {
		return nil, fmt.Errorf("parse sandbox host key: %w", err)
	}
	return &ssh.ClientConfig{
		User:              terminalUser,
		Auth:              []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback:   ssh.FixedHostKey(hostKey),
		HostKeyAlgorithms: []string{hostKey.Type()},
		Timeout:           10 * time.Second,
	}, nil
}

func dialSSH(ctx context.Context, addr string, config *ssh.ClientConfig) (*ssh.Client, error) {
	ctx, cancel := context.WithTimeout(ctx, sshDialTimeout)
	defer cancel()
	var dialer net.Dialer
	for {
		client, err := handshake(ctx, &dialer, addr, config)
		if err == nil {
			return client, nil
		}
		var authErr *ssh.ServerAuthError
		if errors.As(err, &authErr) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("%w (last error: %v)", ctx.Err(), err)
		case <-time.After(retryInterval):
		}
	}
}

func handshake(ctx context.Context, dialer *net.Dialer, addr string, config *ssh.ClientConfig) (*ssh.Client, error) {
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return ssh.NewClient(c, chans, reqs), nil
}

func startShell(client *ssh.Client) (*terminal, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 38400, ssh.TTY_OP_OSPEED: 38400}
	if err := session.RequestPty("xterm-256color", defaultRows, defaultCols, modes); err != nil {
		_ = session.Close()
		return nil, err
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		return nil, err
	}
	if err := session.Shell(); err != nil {
		_ = session.Close()
		return nil, err
	}
	return &terminal{client: client, session: session, stdin: stdin, stdout: stdout}, nil
}

type terminal struct {
	client  *ssh.Client
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
}

func (t *terminal) Read(b []byte) (int, error) {
	return t.stdout.Read(b)
}

func (t *terminal) Write(b []byte) (int, error) {
	return t.stdin.Write(b)
}

func (t *terminal) Resize(cols, rows uint16) error {
	if err := t.session.WindowChange(int(rows), int(cols)); err != nil {
		return fmt.Errorf("resize terminal: %w", err)
	}
	return nil
}

func (t *terminal) Close() error {
	_ = t.session.Close()
	return t.client.Close()
}

func (p *Provider) mgmtIP(ctx context.Context, domain string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, mgmtIPTimeout)
	defer cancel()
	for {
		out, err := p.virsh.run(ctx, "domifaddr", domain, "--source", "lease")
		if isDomainNotFound(err) {
			return "", err
		}
		if ip := leaseAddress(out); err == nil && ip != "" {
			return ip, nil
		}
		out, agentErr := p.virsh.run(ctx, "domifaddr", domain, "--source", "agent")
		if ip := agentAddress(out); agentErr == nil && ip != "" {
			return ip, nil
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("no management address for %s: %w", domain, errors.Join(ctx.Err(), err, agentErr))
		case <-time.After(retryInterval):
		}
	}
}

func leaseAddress(out []byte) string {
	for _, f := range ipv4Rows(out) {
		return f[1]
	}
	return ""
}

func agentAddress(out []byte) string {
	for _, f := range ipv4Rows(out) {
		if f[0] == "eth0" {
			return f[1]
		}
	}
	return ""
}

func ipv4Rows(out []byte) [][2]string {
	var rows [][2]string
	for line := range strings.Lines(string(out)) {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[2] != "ipv4" {
			continue
		}
		address, _, _ := strings.Cut(fields[3], "/")
		if strings.HasPrefix(address, "127.") {
			continue
		}
		rows = append(rows, [2]string{fields[0], address})
	}
	return rows
}
