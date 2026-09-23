package api

import (
	"maps"
	"slices"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/attempt"
	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/runner"
)

const timeLayout = "2006-01-02T15:04:05.000Z07:00"

type healthJSON struct {
	OK             bool   `json:"ok"`
	Docker         bool   `json:"docker"`
	Image          bool   `json:"image"`
	ImageName      string `json:"image_name"`
	MemAvailableMB int    `json:"mem_available_mb"`
	Error          string `json:"error"`
}

type labSummary struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Topic            string   `json:"topic"`
	Level            int      `json:"level"`
	Modes            []string `json:"modes"`
	EstimatedMinutes int      `json:"estimated_minutes"`
}

type nodeJSON struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

type labCheckpoint struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type labDetail struct {
	labSummary
	Nodes       []nodeJSON      `json:"nodes"`
	Checkpoints []labCheckpoint `json:"checkpoints"`
}

type checkpointJSON struct {
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	Status        string  `json:"status"`
	FirstPassedAt *string `json:"first_passed_at"`
}

type attemptJSON struct {
	ID           string           `json:"id"`
	LabID        string           `json:"lab_id"`
	Mode         string           `json:"mode"`
	Status       string           `json:"status"`
	ErrorMessage string           `json:"error_message"`
	Lab          labSummary       `json:"lab"`
	Ticket       string           `json:"ticket"`
	Nodes        []nodeJSON       `json:"nodes"`
	Checkpoints  []checkpointJSON `json:"checkpoints"`
	ElapsedMS    int64            `json:"elapsed_ms"`
	StartedAt    *string          `json:"started_at"`
	EndedAt      *string          `json:"ended_at"`
	ServerTime   string           `json:"server_time"`
	CreatedAt    string           `json:"created_at"`
}

type resultJSON struct {
	AttemptID    string           `json:"attempt_id"`
	Status       string           `json:"status"`
	Lab          labSummary       `json:"lab"`
	ElapsedMS    int64            `json:"elapsed_ms"`
	CommandCount int              `json:"command_count"`
	Checkpoints  []checkpointJSON `json:"checkpoints"`
	Solution     string           `json:"solution"`
}

func healthOf(h runner.Health) healthJSON {
	return healthJSON{
		OK:             h.OK,
		Docker:         h.Docker,
		Image:          h.Image,
		ImageName:      h.ImageName,
		MemAvailableMB: h.MemAvailableMB,
		Error:          h.Error,
	}
}

func labSummaryOf(lab content.Lab, lang string) labSummary {
	modes := lab.Modes
	if modes == nil {
		modes = []string{}
	}
	return labSummary{
		ID:               lab.Id,
		Title:            lab.Title.Get(lang),
		Topic:            lab.Topic,
		Level:            lab.Level,
		Modes:            modes,
		EstimatedMinutes: lab.EstimatedMinutes,
	}
}

func labDetailOf(lab content.Lab, lang string) labDetail {
	nodes := make([]nodeJSON, 0, len(lab.Topology.Nodes))
	for _, name := range slices.Sorted(maps.Keys(lab.Topology.Nodes)) {
		nodes = append(nodes, nodeJSON{Name: name, Role: lab.Topology.Nodes[name].Role})
	}
	checkpoints := make([]labCheckpoint, 0, len(lab.Checkpoints))
	for _, cp := range lab.Checkpoints {
		if !cp.Visible {
			continue
		}
		checkpoints = append(checkpoints, labCheckpoint{ID: cp.Id, Title: cp.Title.Get(lang)})
	}
	return labDetail{labSummary: labSummaryOf(lab, lang), Nodes: nodes, Checkpoints: checkpoints}
}

func attemptOf(view attempt.View, lang string) attemptJSON {
	return attemptJSON{
		ID:           view.Id,
		LabID:        view.LabID,
		Mode:         view.Mode,
		Status:       view.Status,
		ErrorMessage: view.ErrorMessage,
		Lab:          labSummaryOf(view.Lab, lang),
		Ticket:       view.Ticket(lang),
		Nodes:        nodesOf(view),
		Checkpoints:  checkpointsOf(view, lang),
		ElapsedMS:    view.ElapsedMS,
		StartedAt:    formatTimePtr(view.StartedAt),
		EndedAt:      formatTimePtr(view.EndedAt),
		ServerTime:   formatTime(view.ServerTime),
		CreatedAt:    formatTime(view.CreatedAt),
	}
}

func resultOf(view attempt.View, lang string, commandCount int, solution string) resultJSON {
	return resultJSON{
		AttemptID:    view.Id,
		Status:       view.Status,
		Lab:          labSummaryOf(view.Lab, lang),
		ElapsedMS:    view.ElapsedMS,
		CommandCount: commandCount,
		Checkpoints:  checkpointsOf(view, lang),
		Solution:     solution,
	}
}

func nodesOf(view attempt.View) []nodeJSON {
	nodes := make([]nodeJSON, 0, len(view.Nodes))
	for _, node := range view.Nodes {
		nodes = append(nodes, nodeJSON{Name: node.Name, Role: node.Role})
	}
	return nodes
}

func checkpointsOf(view attempt.View, lang string) []checkpointJSON {
	checkpoints := make([]checkpointJSON, 0, len(view.Checkpoints))
	for _, cp := range view.Checkpoints {
		checkpoints = append(checkpoints, checkpointJSON{
			ID:            cp.Id,
			Title:         cp.Title.Get(lang),
			Status:        cp.Status,
			FirstPassedAt: formatTimePtr(cp.FirstPassedAt),
		})
	}
	return checkpoints
}

func formatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

func formatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	formatted := formatTime(*t)
	return &formatted
}
