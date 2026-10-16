package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/attempt"
	"github.com/AmerDwight/network-skill-lab/internal/config"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

const pruneInterval = 24 * time.Hour

func pruneCmd(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("prune", flag.ContinueOnError)
	olderThan := flags.String("older-than", "", "remove finished attempts that ended longer ago than this, e.g. 30d or 720h")
	dryRun := flags.Bool("dry-run", false, "report what would be removed without removing it")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("prune takes no arguments, got %q", flags.Arg(0))
	}
	if *olderThan == "" {
		return errors.New("usage: nsl prune --older-than <duration> [--dry-run]")
	}
	retention, err := config.ParseDuration(*olderThan)
	if err != nil {
		return fmt.Errorf("parse --older-than: %w", err)
	}
	if retention <= 0 {
		return fmt.Errorf("parse --older-than: must be positive, got %q", *olderThan)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { _ = st.Close() }()

	attempts := attempt.New(attempt.Deps{
		Store:   st,
		DataDir: cfg.DataDir,
		Logger:  slog.New(slog.DiscardHandler),
	})
	defer attempts.Close()

	report, err := attempts.Prune(context.Background(), retention, *dryRun)
	if *dryRun {
		for _, id := range report.IDs {
			_, _ = fmt.Fprintln(stdout, id)
		}
		_, _ = fmt.Fprintf(stdout, "prune: would remove %d attempts and free %d bytes\n", report.Attempts, report.Bytes)
	} else {
		_, _ = fmt.Fprintf(stdout, "prune: removed %d attempts and freed %d bytes\n", report.Attempts, report.Bytes)
	}
	return err
}

func startPruning(ctx context.Context, attempts *attempt.Service, retention time.Duration, logger *slog.Logger) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Go(func() {
		ticker := time.NewTicker(pruneInterval)
		defer ticker.Stop()
		for {
			report, err := attempts.Prune(ctx, retention, false)
			if err != nil {
				logger.Error("scheduled prune", "error", err, "attempts", report.Attempts, "bytes", report.Bytes)
			} else {
				logger.Info("scheduled prune", "older_than", retention.String(), "attempts", report.Attempts, "bytes", report.Bytes)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	})
	return func() {
		cancel()
		wg.Wait()
	}
}
