package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/AmerDwight/network-skill-lab/internal/attempt"
	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, healthOf(s.runner.Health(r.Context())))
}

func (s *server) listTopics(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, topicNodesOf(s.content.Topics, s.content.Labs, s.content.Docs, langOf(r)))
}

func (s *server) listLabs(w http.ResponseWriter, r *http.Request) {
	lang := langOf(r)
	topic := r.URL.Query().Get("topic")
	labs := make([]labSummary, 0, len(s.content.Labs))
	for _, lab := range s.content.Labs {
		if topic != "" && !inTopic(lab.Topic, topic) {
			continue
		}
		labs = append(labs, s.labSummaryOf(lab, lang))
	}
	writeJSON(w, http.StatusOK, labs)
}

func (s *server) getLab(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r, "id")
	lab, ok := s.byID[id]
	if !ok {
		s.fail(w, "", fmt.Errorf("%w: %s", attempt.ErrUnknownLab, id))
		return
	}
	writeJSON(w, http.StatusOK, s.labDetailOf(lab, langOf(r)))
}

func (s *server) listDocs(w http.ResponseWriter, r *http.Request) {
	lang := langOf(r)
	docs := make([]docSummary, 0, len(s.content.Docs))
	for _, doc := range s.content.Docs {
		docs = append(docs, docSummary{ID: doc.ID, Title: doc.Title.Get(lang), Topic: doc.Topic})
	}
	writeJSON(w, http.StatusOK, docs)
}

func (s *server) getDoc(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r, "*")
	doc, ok := s.docs[id]
	if !ok {
		s.fail(w, "", fmt.Errorf("%w: %s", attempt.ErrUnknownDoc, id))
		return
	}
	lang := langOf(r)
	body, err := doc.Body(lang)
	if err != nil {
		s.fail(w, "", err)
		return
	}
	done, err := s.progress(r)
	if err != nil {
		s.fail(w, "", err)
		return
	}
	writeJSON(w, http.StatusOK, docDetail{
		docSummary: docSummary{ID: doc.ID, Title: doc.Title.Get(lang), Topic: doc.Topic},
		Body:       body,
		Completed:  done[attempt.ProgressKey{Kind: store.ProgressDoc, Ref: doc.ID}],
	})
}

func (s *server) listTracks(w http.ResponseWriter, r *http.Request) {
	lang := langOf(r)
	done, err := s.progress(r)
	if err != nil {
		s.fail(w, "", err)
		return
	}
	tracks := make([]trackSummary, 0, len(s.content.Tracks))
	for _, track := range s.content.Tracks {
		completed := 0
		for _, step := range track.Steps {
			if stepCompleted(step, done) {
				completed++
			}
		}
		tracks = append(tracks, trackSummary{
			ID:        track.ID,
			Title:     track.Title.Get(lang),
			Steps:     len(track.Steps),
			Completed: completed,
		})
	}
	writeJSON(w, http.StatusOK, tracks)
}

func (s *server) getTrack(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r, "id")
	lang := langOf(r)
	for _, track := range s.content.Tracks {
		if track.ID != id {
			continue
		}
		done, err := s.progress(r)
		if err != nil {
			s.fail(w, "", err)
			return
		}
		steps := make([]trackStep, 0, len(track.Steps))
		for _, step := range track.Steps {
			steps = append(steps, s.trackStepOf(step, lang, done))
		}
		writeJSON(w, http.StatusOK, trackDetail{ID: track.ID, Title: track.Title.Get(lang), Steps: steps})
		return
	}
	writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("unknown track %s", id))
}

func (s *server) trackStepOf(step content.TrackStep, lang string, done map[attempt.ProgressKey]bool) trackStep {
	if step.Doc != "" {
		title := step.Doc
		if doc, ok := s.docs[step.Doc]; ok {
			title = doc.Title.Get(lang)
		}
		return trackStep{
			Kind:      store.ProgressDoc,
			Ref:       step.Doc,
			Title:     title,
			Completed: done[attempt.ProgressKey{Kind: store.ProgressDoc, Ref: step.Doc}],
		}
	}
	title := step.Lab
	if lab, ok := s.byID[step.Lab]; ok {
		title = lab.Title.Get(lang)
	}
	out := trackStep{
		Kind:      store.ProgressLab,
		Ref:       step.Lab,
		Title:     title,
		Completed: done[attempt.ProgressKey{Kind: store.ProgressLab, Ref: step.Lab}],
	}
	if step.Mode != "" {
		mode := step.Mode
		out.Mode = &mode
	}
	return out
}

func stepCompleted(step content.TrackStep, done map[attempt.ProgressKey]bool) bool {
	if step.Doc != "" {
		return done[attempt.ProgressKey{Kind: store.ProgressDoc, Ref: step.Doc}]
	}
	return done[attempt.ProgressKey{Kind: store.ProgressLab, Ref: step.Lab}]
}

func (s *server) progress(r *http.Request) (map[attempt.ProgressKey]bool, error) {
	return s.attempts.ProgressFor(r.Context(), userOf(r).ID)
}

func (s *server) markProgress(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind string `json:"kind"`
		Ref  string `json:"ref"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		s.fail(w, "", err)
		return
	}
	if body.Kind != store.ProgressDoc {
		s.fail(w, "", fmt.Errorf("%w: only %q progress can be marked by hand, got %q", errBadRequest, store.ProgressDoc, body.Kind))
		return
	}

	if err := s.attempts.MarkDocRead(r.Context(), userOf(r).ID, body.Ref); err != nil {
		s.fail(w, "", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) createAttempt(w http.ResponseWriter, r *http.Request) {
	var body struct {
		LabID string `json:"lab_id"`
		Mode  string `json:"mode"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		s.fail(w, "", err)
		return
	}

	view, err := s.attempts.Start(r.Context(), userOf(r).ID, body.LabID, body.Mode)
	if err != nil {
		if !s.failBusy(w, err) {
			s.fail(w, "", err)
		}
		return
	}
	writeJSON(w, http.StatusCreated, s.attemptOf(view, langOf(r)))
}

func (s *server) currentAttempt(w http.ResponseWriter, r *http.Request) {
	view, ok, err := s.attempts.Current(r.Context(), userOf(r).ID)
	if err != nil {
		s.fail(w, "", err)
		return
	}
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, s.attemptOf(view, langOf(r)))
}

func (s *server) getAttempt(w http.ResponseWriter, r *http.Request) {
	view, err := s.readableAttempt(r)
	if err != nil {
		s.fail(w, pathParam(r, "id"), err)
		return
	}
	writeJSON(w, http.StatusOK, s.attemptOf(view, langOf(r)))
}

func (s *server) abandonAttempt(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r, "id")
	view, err := s.attempts.Abandon(r.Context(), userOf(r).ID, id)
	if err != nil {
		s.fail(w, id, err)
		return
	}
	writeJSON(w, http.StatusOK, s.attemptOf(view, langOf(r)))
}

func (s *server) submitAttempt(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r, "id")
	if _, err := s.ownedAttempt(r); err != nil {
		s.fail(w, id, err)
		return
	}
	result, err := s.attempts.Submit(r.Context(), id)
	if err != nil {
		s.fail(w, id, err)
		return
	}
	writeJSON(w, http.StatusOK, submitResultOf(result, langOf(r)))
}

func (s *server) attemptResult(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r, "id")
	lang := langOf(r)

	view, err := s.readableAttempt(r)
	if err != nil {
		s.fail(w, id, err)
		return
	}
	if !terminalStatus(view.Status) {
		s.fail(w, id, fmt.Errorf("%w: %s is %s", attempt.ErrNotTerminal, id, view.Status))
		return
	}

	count, err := s.store.CommandLog.CountByAttempt(r.Context(), id)
	if err != nil {
		s.fail(w, id, err)
		return
	}
	path := filepath.Join(view.Lab.Dir, view.Lab.Solution.Get(lang))
	solution, err := os.ReadFile(path)
	if err != nil {
		s.fail(w, id, fmt.Errorf("read solution %s: %w", path, err))
		return
	}
	writeJSON(w, http.StatusOK, s.resultOf(view, lang, count, string(solution)))
}
