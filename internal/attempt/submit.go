package attempt

import (
	"context"
	"fmt"

	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

type SubmitCheckpoint struct {
	Id     string
	Title  content.Localized
	Status string
}

type SubmitResult struct {
	Passed       bool
	Checkpoints  []SubmitCheckpoint
	HiddenFailed int
	SubmitCount  int
}

func (s *Service) Submit(ctx context.Context, id string) (SubmitResult, error) {
	att, err := s.get(ctx, id)
	if err != nil {
		return SubmitResult{}, err
	}
	if att.Mode != ModeReal {
		return SubmitResult{}, fmt.Errorf("%w: %s is in %s mode", ErrModeNotReal, id, att.Mode)
	}
	switch att.Status {
	case store.StatusRunning:
	case store.StatusProvisioning:
		return SubmitResult{}, fmt.Errorf("%w: %s", ErrProvisioning, id)
	default:
		return SubmitResult{}, fmt.Errorf("%w: %s is %s", ErrTerminal, id, att.Status)
	}

	sweeper := s.currentSweeper()
	if sweeper == nil {
		return SubmitResult{}, ErrNoSweeper
	}
	if err := s.store.Attempts.IncrementSubmitCount(ctx, id); err != nil {
		return SubmitResult{}, err
	}

	statuses, err := sweeper.SweepNow(ctx, id)
	if err != nil {
		return SubmitResult{}, err
	}

	view, err := s.Get(ctx, id)
	if err != nil {
		return SubmitResult{}, err
	}

	result := SubmitResult{Passed: true, SubmitCount: view.SubmitCount}
	for _, cp := range view.Checkpoints {
		status := cp.Status
		if swept, ok := statuses[cp.Id]; ok {
			status = swept
		}
		if status != store.CheckpointPass {
			result.Passed = false
			if !cp.Visible {
				result.HiddenFailed++
			}
		}
		if cp.Visible {
			result.Checkpoints = append(result.Checkpoints, SubmitCheckpoint{Id: cp.Id, Title: cp.Title, Status: status})
		}
	}

	if result.Passed {
		if err := s.Finish(ctx, id, store.StatusPassed); err != nil {
			return SubmitResult{}, err
		}
		return result, nil
	}

	s.bus.publish(Event{
		AttemptID:    id,
		Type:         EventSubmit,
		ServerTime:   s.now(),
		SubmitCount:  result.SubmitCount,
		HiddenFailed: result.HiddenFailed,
	})
	return result, nil
}

type ProgressKey struct {
	Kind string
	Ref  string
}

func (s *Service) MarkDocRead(ctx context.Context, userID, docID string) error {
	if _, ok := s.docs[docID]; !ok {
		return fmt.Errorf("%w: %s", ErrUnknownDoc, docID)
	}
	return s.store.Progress.Upsert(ctx, userID, store.ProgressDoc, docID)
}

func (s *Service) ProgressFor(ctx context.Context, userID string) (map[ProgressKey]bool, error) {
	entries, err := s.store.Progress.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	done := make(map[ProgressKey]bool, len(entries))
	for _, entry := range entries {
		done[ProgressKey{Kind: entry.Kind, Ref: entry.Ref}] = true
	}
	return done, nil
}
