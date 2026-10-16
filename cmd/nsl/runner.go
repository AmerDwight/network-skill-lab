package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/AmerDwight/network-skill-lab/internal/config"
	"github.com/AmerDwight/network-skill-lab/internal/provider/docker"
	"github.com/AmerDwight/network-skill-lab/internal/provider/vm"
	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/AmerDwight/network-skill-lab/internal/runnerclient"
	"github.com/AmerDwight/network-skill-lab/internal/runnerserver"
	"github.com/AmerDwight/network-skill-lab/internal/store"
	"github.com/docker/docker/client"
)

const runnerTokenEnv = "NSL_RUNNER_TOKEN"

func runnerUsage() {
	fmt.Fprint(os.Stderr, `usage: nsl runner <command>

commands:
  --url <ws(s)://host/ws/runner> --name <name> [--max-sandboxes 3] [--provider docker|vm]
        [--instance <id>] [--token-file <path>]
                    connect to a control plane and run sandboxes on this host;
                    the token comes from --token-file or `+runnerTokenEnv+`;
                    --provider vm reads NSL_VM_IMAGE, NSL_VM_DIR, NSL_VM_MGMT_NETWORK,
                    NSL_VM_MEMORY_MIB, NSL_VM_VCPUS and NSL_VM_BOOT_TIMEOUT
  token <name> [--capabilities container] [--capacity 3]
                    register a runner and print its token once
  revoke <name>     reject the runner's token from now on
  list              list registered runners
`)
}

func runnerCmd(args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "token":
			return runnerToken(args[1:], stdout, stderr)
		case "revoke":
			return runnerRevoke(args[1:], stdout)
		case "list":
			return runnerList(args[1:], stdout)
		case "help", "-h", "--help":
			runnerUsage()
			return nil
		}
	}
	return runnerServe(args)
}

func runnerServe(args []string) error {
	flags := flag.NewFlagSet("runner", flag.ContinueOnError)
	url := flags.String("url", "", "control plane runner endpoint, e.g. wss://host/ws/runner")
	name := flags.String("name", "", "runner name, as registered with `nsl runner token`")
	maxSandboxes := flags.Int("max-sandboxes", 3, "how many sandboxes this runner accepts at once")
	providerName := flags.String("provider", "docker", "sandbox provider: docker or vm")
	instance := flags.String("instance", "", "label that scopes garbage collection (default: the runner name)")
	tokenFile := flags.String("token-file", "", "file holding the runner token (default: $"+runnerTokenEnv+")")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		runnerUsage()
		return fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	if *url == "" || *name == "" {
		runnerUsage()
		return errors.New("--url and --name are required")
	}
	if *maxSandboxes < 1 {
		return fmt.Errorf("--max-sandboxes must be at least 1, got %d", *maxSandboxes)
	}
	if *providerName != "docker" && *providerName != "vm" {
		return fmt.Errorf("unknown provider %q, want docker or vm", *providerName)
	}
	if *instance == "" {
		*instance = *name
	}
	token, err := readRunnerToken(*tokenFile)
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	provider, closeProvider, err := newRunnerProvider(*providerName, *instance, cfg, logger)
	if err != nil {
		return err
	}
	defer closeProvider()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info("runner starting", "url", *url, "name", *name, "provider", *providerName, "instance", *instance, "max_sandboxes", *maxSandboxes, "version", version)
	return runnerclient.Run(ctx, runnerclient.Options{
		URL:      *url,
		Token:    token,
		Name:     *name,
		Capacity: *maxSandboxes,
		Version:  version,
		Provider: provider,
		Logger:   logger,
	})
}

func newRunnerProvider(name, instance string, cfg config.Config, logger *slog.Logger) (runner.Runner, func(), error) {
	if name == "vm" {
		p, err := vm.New(vm.Config{
			Image:       cfg.VM.Image,
			Dir:         cfg.VM.Dir,
			MgmtNetwork: cfg.VM.MgmtNetwork,
			MemoryMiB:   cfg.VM.MemoryMiB,
			VCPUs:       cfg.VM.VCPUs,
			BootTimeout: cfg.VM.BootTimeout,
			Instance:    instance,
		}, logger)
		if err != nil {
			return nil, nil, fmt.Errorf("vm provider: %w", err)
		}
		return p, func() {}, nil
	}
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, nil, fmt.Errorf("create docker client: %w", err)
	}
	p := docker.New(cli, docker.Options{Image: cfg.NodeImage, Instance: instance, SystemdTimeout: cfg.SystemdTimeout})
	return p, func() { _ = cli.Close() }, nil
}

func readRunnerToken(path string) (string, error) {
	if path == "" {
		token := strings.TrimSpace(os.Getenv(runnerTokenEnv))
		if token == "" {
			return "", fmt.Errorf("no runner token: set %s or pass --token-file", runnerTokenEnv)
		}
		return token, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read token file: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("token file %s is empty", path)
	}
	return token, nil
}

func runnerToken(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("runner token", flag.ContinueOnError)
	capabilities := flags.String("capabilities", "container", "comma-separated environments this runner provides")
	capacity := flags.Int("capacity", 3, "expected number of concurrent sandboxes; the runner reports its own on connect")
	name, err := parseUserArgs(flags, args, "nsl runner token <name> [--capabilities container] [--capacity 3]")
	if err != nil {
		return err
	}

	st, err := openUserStore()
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	record, token, err := runnerserver.Register(context.Background(), st.Runners, name, splitList(*capabilities), *capacity)
	if errors.Is(err, store.ErrRunnerNameTaken) {
		return fmt.Errorf("runner %q already exists; revoke it and pick another name", name)
	}
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stderr, "registered runner %s (%s); the token below is shown only once:\n", record.Name, strings.Join(record.Capabilities, ","))
	_, err = fmt.Fprintln(stdout, token)
	return err
}

func splitList(value string) []string {
	var items []string
	for item := range strings.SplitSeq(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func runnerRevoke(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("runner revoke", flag.ContinueOnError)
	name, err := parseUserArgs(flags, args, "nsl runner revoke <name>")
	if err != nil {
		return err
	}

	st, err := openUserStore()
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	record, err := st.Runners.ByName(context.Background(), name)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("no such runner %q", name)
	}
	if err != nil {
		return err
	}
	if err := st.Runners.Revoke(context.Background(), record.ID); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "revoked runner %s\n", name)
	return err
}

func runnerList(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("runner list", flag.ContinueOnError)
	names, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if len(names) > 0 {
		return fmt.Errorf("usage: nsl runner list, got %q", names[0])
	}

	st, err := openUserStore()
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	runners, err := st.Runners.List(context.Background())
	if err != nil {
		return err
	}

	writer := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "NAME\tCAPABILITIES\tCAPACITY\tLAST SEEN\tVERSION\tREVOKED")
	for _, r := range runners {
		lastSeen, revoked, version := "-", "-", "-"
		if r.LastSeenAt != nil {
			lastSeen = r.LastSeenAt.Local().Format(userTimeLayout)
		}
		if r.RevokedAt != nil {
			revoked = r.RevokedAt.Local().Format(userTimeLayout)
		}
		if r.Version != "" {
			version = r.Version
		}
		_, _ = fmt.Fprintf(writer, "%s\t%s\t%d\t%s\t%s\t%s\n", r.Name, strings.Join(r.Capabilities, ","), r.Capacity, lastSeen, version, revoked)
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("write runner table: %w", err)
	}
	return nil
}
