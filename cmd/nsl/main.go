package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/attempt"
	"github.com/AmerDwight/network-skill-lab/internal/checker"
	"github.com/AmerDwight/network-skill-lab/internal/config"
	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/provider/docker"
	"github.com/AmerDwight/network-skill-lab/internal/recorder"
	"github.com/AmerDwight/network-skill-lab/internal/store"
	"github.com/AmerDwight/network-skill-lab/internal/web"
	"github.com/docker/docker/client"
)

var version = "dev"

const shutdownTimeout = 10 * time.Second

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "nsl: %v\n", err)
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
  serve     run the HTTP server
  version   print the build version
`)
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

	labs, err := content.Load(cfg.ContentDir)
	if err != nil {
		return fmt.Errorf("load content: %w", err)
	}
	logger.Info("labs loaded", "count", len(labs), "dir", cfg.ContentDir)

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
		Labs:        labs,
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

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           web.NewRouter(),
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
