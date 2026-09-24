package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/api"
	"github.com/AmerDwight/network-skill-lab/internal/attempt"
	"github.com/AmerDwight/network-skill-lab/internal/checker"
	"github.com/AmerDwight/network-skill-lab/internal/config"
	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/lint"
	"github.com/AmerDwight/network-skill-lab/internal/provider/docker"
	"github.com/AmerDwight/network-skill-lab/internal/recorder"
	"github.com/AmerDwight/network-skill-lab/internal/store"
	"github.com/AmerDwight/network-skill-lab/internal/web"
	"github.com/docker/docker/client"
)

var version = "dev"

const shutdownTimeout = 10 * time.Second

var errFindings = errors.New("content has errors")

func main() {
	if err := run(os.Args[1:]); err != nil {
		if !errors.Is(err, errFindings) {
			fmt.Fprintf(os.Stderr, "nsl: %v\n", err)
		}
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return errors.New("missing command")
	}

	switch args[0] {
	case "serve":
		return serve(args[1:])
	case "content":
		return contentCmd(args[1:])
	case "user":
		return userCmd(args[1:], os.Stdin, os.Stdout)
	case "version":
		fmt.Println(version)
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: nsl <command>

commands:
  serve          run the HTTP server
  content lint   validate a content directory
  user           manage local accounts: add, passwd, disable, enable, list
  version        print the build version
`)
}

func contentCmd(args []string) error {
	if len(args) == 0 || args[0] != "lint" {
		usage()
		return errors.New("usage: nsl content lint [dir]")
	}

	flags := flag.NewFlagSet("content lint", flag.ContinueOnError)
	asJSON := flags.Bool("json", false, "print findings as JSON")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return fmt.Errorf("content lint takes at most one directory, got %q", flags.Arg(1))
	}

	dir := flags.Arg(0)
	if dir == "" {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		dir = cfg.ContentDir
	}

	findings, err := lint.Run(dir)
	if err != nil {
		return fmt.Errorf("lint content: %w", err)
	}
	if *asJSON {
		if findings == nil {
			findings = []lint.Finding{}
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(findings); err != nil {
			return fmt.Errorf("encode findings: %w", err)
		}
	} else {
		for _, finding := range findings {
			fmt.Println(finding.Line())
		}
	}
	if lint.ExitCode(findings) != 0 {
		return errFindings
	}
	return nil
}

func serve(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("serve takes no arguments, got %q", args[0])
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	loaded, err := content.LoadAll(cfg.ContentDir)
	if err != nil {
		return fmt.Errorf("load content: %w", err)
	}
	logger.Info("content loaded", "labs", len(loaded.Labs), "docs", len(loaded.Docs), "topics", len(loaded.Topics), "tracks", len(loaded.Tracks), "dir", cfg.ContentDir)

	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			logger.Error("close store", "error", err)
		}
	}()
	logger.Info("store opened", "path", st.Path())

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("create docker client: %w", err)
	}
	defer func() {
		if err := cli.Close(); err != nil {
			logger.Error("close docker client", "error", err)
		}
	}()

	provider := docker.New(cli, docker.Options{Image: cfg.NodeImage})
	attempts := attempt.New(attempt.Deps{
		Store:       st,
		Runner:      provider,
		Content:     loaded,
		Image:       cfg.NodeImage,
		RunnerID:    "docker",
		IdleTimeout: cfg.IdleTimeout,
		Logger:      logger,
	})
	defer attempts.Close()

	checks := checker.New(checker.Deps{
		Store:    st,
		Runner:   provider,
		Attempts: attempts,
		Interval: cfg.CheckInterval,
		Logger:   logger,
	})
	defer checks.Close()
	attempts.SetSweeper(checks)

	recordings := recorder.New(recorder.Deps{
		Store:    st,
		Runner:   provider,
		Attempts: attempts,
		DataDir:  cfg.DataDir,
		Logger:   logger,
	})
	defer recordings.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := attempts.Recover(ctx); err != nil {
		return fmt.Errorf("recover attempts: %w", err)
	}

	checks.Start(ctx)
	recordings.Start(ctx)

	handler := web.NewRouter(api.New(api.Deps{
		Attempts: attempts,
		Content:  loaded,
		Store:    st,
		Runner:   provider,
		Recorder: recordings,
		Logger:   logger,
	}))

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		logger.Info("server listening", "addr", cfg.Listen, "version", version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- fmt.Errorf("listen and serve: %w", err)
			return
		}
		errc <- nil
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		stop()
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return <-errc
}
