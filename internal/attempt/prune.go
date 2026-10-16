package attempt

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"path/filepath"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/store"
)

const pruneBatch = 200

type PruneReport struct {
	Attempts int
	Bytes    int64
	IDs      []string
}

func (s *Service) Prune(ctx context.Context, olderThan time.Duration, dryRun bool) (PruneReport, error) {
	if olderThan <= 0 {
		return PruneReport{}, fmt.Errorf("prune: retention must be positive, got %s", olderThan)
	}
	cutoff := s.now().Add(-olderThan)

	var report PruneReport
	if dryRun {
		candidates, err := s.store.Attempts.ListTerminalEndedBefore(ctx, cutoff, math.MaxInt32)
		if err != nil {
			return report, err
		}
		for _, att := range candidates {
			size, err := s.recordingsSize(att.ID)
			if err != nil {
				return report, err
			}
			report.add(att.ID, size)
		}
		return report, nil
	}

	for {
		batch, err := s.store.Attempts.ListTerminalEndedBefore(ctx, cutoff, pruneBatch)
		if err != nil {
			return report, err
		}
		for _, att := range batch {
			size, err := s.recordingsSize(att.ID)
			if err != nil {
				return report, err
			}
			if err := s.remove(ctx, att.ID); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					continue
				}
				return report, err
			}
			report.add(att.ID, size)
		}
		if len(batch) < pruneBatch {
			return report, nil
		}
	}
}

func (r *PruneReport) add(id string, size int64) {
	r.Attempts++
	r.Bytes += size
	r.IDs = append(r.IDs, id)
}

func (s *Service) recordingsSize(id string) (int64, error) {
	if s.dataDir == "" {
		return 0, nil
	}
	var total int64
	err := filepath.WalkDir(s.recordingsOf(id), func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("size recordings of attempt %s: %w", id, err)
	}
	return total, nil
}
