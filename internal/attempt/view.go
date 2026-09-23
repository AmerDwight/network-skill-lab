package attempt

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

type Node struct {
	Name string
	Role string
}

type Checkpoint struct {
	Id            string
	Title         content.Localized
	Status        string
	FirstPassedAt *time.Time
}

type View struct {
	Id           string
	LabID        string
	Mode         string
	Status       string
	ErrorMessage string
	SandboxID    string
	Lab          content.Lab
	Params       map[string]string
	Nodes        []Node
	Checkpoints  []Checkpoint
	ElapsedMS    int64
	StartedAt    *time.Time
	EndedAt      *time.Time
	CreatedAt    time.Time
	ServerTime   time.Time

	ticket content.Localized
}

func (v View) Ticket(lang string) string {
	return v.ticket.Get(lang)
}

func newView(att store.Attempt, lab content.Lab, params map[string]string, runs []store.CheckpointRun, now time.Time) (View, error) {
	ticket, err := renderLocalized(lab.Ticket, params)
	if err != nil {
		return View{}, fmt.Errorf("render ticket of lab %s: %w", lab.Id, err)
	}

	byID := make(map[string]store.CheckpointRun, len(runs))
	for _, run := range runs {
		byID[run.CheckpointID] = run
	}

	checkpoints := make([]Checkpoint, 0, len(lab.Checkpoints))
	for _, cp := range lab.Checkpoints {
		if !cp.Visible {
			continue
		}
		title, err := renderLocalized(cp.Title, params)
		if err != nil {
			return View{}, fmt.Errorf("render title of checkpoint %s: %w", cp.Id, err)
		}
		checkpoint := Checkpoint{Id: cp.Id, Title: title, Status: store.CheckpointPending}
		if run, ok := byID[cp.Id]; ok {
			checkpoint.Status = run.LastStatus
			checkpoint.FirstPassedAt = run.FirstPassedAt
		}
		checkpoints = append(checkpoints, checkpoint)
	}

	nodes := make([]Node, 0, len(lab.Topology.Nodes))
	for _, name := range slices.Sorted(maps.Keys(lab.Topology.Nodes)) {
		nodes = append(nodes, Node{Name: name, Role: lab.Topology.Nodes[name].Role})
	}

	return View{
		Id:           att.ID,
		LabID:        att.LabID,
		Mode:         att.Mode,
		Status:       att.Status,
		ErrorMessage: att.ErrorMessage,
		SandboxID:    att.SandboxID,
		Lab:          lab,
		Params:       params,
		Nodes:        nodes,
		Checkpoints:  checkpoints,
		ElapsedMS:    elapsedMS(att, now),
		StartedAt:    att.StartedAt,
		EndedAt:      att.EndedAt,
		CreatedAt:    att.CreatedAt,
		ServerTime:   now,
		ticket:       ticket,
	}, nil
}

func renderLocalized(l content.Localized, params map[string]string) (content.Localized, error) {
	zh, err := content.Render(l.Zh, params)
	if err != nil {
		return content.Localized{}, err
	}
	en, err := content.Render(l.En, params)
	if err != nil {
		return content.Localized{}, err
	}
	return content.Localized{Zh: zh, En: en}, nil
}

func elapsedOf(att store.Attempt, now time.Time) time.Duration {
	if att.StartedAt == nil {
		return 0
	}
	elapsed := now.Sub(*att.StartedAt)
	if elapsed < 0 {
		return 0
	}
	return elapsed
}

func elapsedMS(att store.Attempt, now time.Time) int64 {
	if att.EndedAt != nil {
		return att.ElapsedMS
	}
	return elapsedOf(att, now).Milliseconds()
}
