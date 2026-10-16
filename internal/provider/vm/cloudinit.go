package vm

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
	"golang.org/x/crypto/ssh"
)

const (
	terminalUser      = "nsl"
	bootstrapEnvPath  = "/etc/nsl/bootstrap.env"
	cloudConfigHeader = "#cloud-config\n"
)

type keyPair struct {
	private []byte
	public  string
}

func newKeyPair(comment string) (keyPair, error) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return keyPair{}, fmt.Errorf("generate ed25519 key: %w", err)
	}
	block, err := ssh.MarshalPrivateKey(private, comment)
	if err != nil {
		return keyPair{}, fmt.Errorf("marshal private key: %w", err)
	}
	sshPublic, err := ssh.NewPublicKey(public)
	if err != nil {
		return keyPair{}, fmt.Errorf("marshal public key: %w", err)
	}
	authorized := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPublic)))
	return keyPair{private: pem.EncodeToMemory(block), public: authorized}, nil
}

type seed struct {
	instanceID string
	hostname   string
	userKey    string
	hostKey    keyPair
	env        map[string]string
}

type cloudUser struct {
	Name              string   `yaml:"name"`
	Shell             string   `yaml:"shell"`
	Sudo              string   `yaml:"sudo"`
	LockPasswd        bool     `yaml:"lock_passwd"`
	SSHAuthorizedKeys []string `yaml:"ssh_authorized_keys"`
}

type cloudFile struct {
	Path        string `yaml:"path"`
	Owner       string `yaml:"owner"`
	Permissions string `yaml:"permissions"`
	Content     string `yaml:"content"`
}

type cloudConfig struct {
	Hostname       string            `yaml:"hostname"`
	ManageEtcHosts bool              `yaml:"manage_etc_hosts"`
	SSHPwauth      bool              `yaml:"ssh_pwauth"`
	SSHDeleteKeys  bool              `yaml:"ssh_deletekeys"`
	SSHGenKeyTypes []string          `yaml:"ssh_genkeytypes"`
	SSHKeys        map[string]string `yaml:"ssh_keys"`
	Users          []cloudUser       `yaml:"users"`
	WriteFiles     []cloudFile       `yaml:"write_files"`
}

func (s seed) userData() ([]byte, error) {
	cfg := cloudConfig{
		Hostname:       s.hostname,
		ManageEtcHosts: true,
		SSHDeleteKeys:  true,
		SSHGenKeyTypes: []string{},
		SSHKeys: map[string]string{
			"ed25519_private": string(s.hostKey.private),
			"ed25519_public":  s.hostKey.public,
		},
		Users: []cloudUser{{
			Name:              terminalUser,
			Shell:             "/bin/bash",
			Sudo:              "ALL=(ALL) NOPASSWD:ALL",
			LockPasswd:        true,
			SSHAuthorizedKeys: []string{s.userKey},
		}},
		WriteFiles: []cloudFile{{
			Path:        bootstrapEnvPath,
			Owner:       "root:root",
			Permissions: "0600",
			Content:     envFile(s.env),
		}},
	}
	body, err := yaml.MarshalWithOptions(cfg, yaml.UseLiteralStyleIfMultiline(true))
	if err != nil {
		return nil, fmt.Errorf("render user-data: %w", err)
	}
	return append([]byte(cloudConfigHeader), body...), nil
}

func (s seed) metaData() []byte {
	return fmt.Appendf(nil, "instance-id: %s\nlocal-hostname: %s\n", s.instanceID, s.hostname)
}

func networkConfig() []byte {
	return []byte("version: 2\nethernets:\n  eth0:\n    dhcp4: true\n    dhcp-identifier: mac\n")
}

func envFile(env map[string]string) string {
	var b strings.Builder
	for _, key := range slices.Sorted(maps.Keys(env)) {
		fmt.Fprintf(&b, "%s=%s\n", key, shellQuote(env[key]))
	}
	return b.String()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
