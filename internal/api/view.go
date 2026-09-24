package api

import (
	"maps"
	"slices"
	"strings"
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
	Instance       string `json:"instance"`
	MemAvailableMB int    `json:"mem_available_mb"`
	Error          string `json:"error"`
}

type topicNode struct {
	ID       string      `json:"id"`
	Title    string      `json:"title"`
	Labs     int         `json:"labs"`
	Docs     int         `json:"docs"`
	Children []topicNode `json:"children"`
}

type docRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type labSummary struct {
	ID                   string   `json:"id"`
	Title                string   `json:"title"`
	Topic                string   `json:"topic"`
	Level                int      `json:"level"`
	Modes                []string `json:"modes"`
	EstimatedMinutes     int      `json:"estimated_minutes"`
	RelatedDocs          []docRef `json:"related_docs"`
	HasHiddenCheckpoints bool     `json:"has_hidden_checkpoints"`
}

type docSummary struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Topic string `json:"topic"`
}

type docDetail struct {
	docSummary
	Body      string `json:"body"`
	Completed bool   `json:"completed"`
}

type trackSummary struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Steps     int    `json:"steps"`
	Completed int    `json:"completed"`
}

type trackStep struct {
	Kind      string  `json:"kind"`
	Ref       string  `json:"ref"`
	Title     string  `json:"title"`
	Mode      *string `json:"mode"`
	Completed bool    `json:"completed"`
}

type trackDetail struct {
	ID    string      `json:"id"`
	Title string      `json:"title"`
	Steps []trackStep `json:"steps"`
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
	TopicTitle  string          `json:"topic_title"`
}

type checkpointJSON struct {
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	Status        string  `json:"status"`
	FirstPassedAt *string `json:"first_passed_at"`
}

type resultCheckpointJSON struct {
	checkpointJSON
	Visible bool `json:"visible"`
}

type tutorialStepJSON struct {
	Checkpoint  string `json:"checkpoint"`
	Instruction string `json:"instruction"`
}

type attemptJSON struct {
	ID                string             `json:"id"`
	LabID             string             `json:"lab_id"`
	Mode              string             `json:"mode"`
	Status            string             `json:"status"`
	ErrorMessage      string             `json:"error_message"`
	Lab               labSummary         `json:"lab"`
	Ticket            string             `json:"ticket"`
	Nodes             []nodeJSON         `json:"nodes"`
	Checkpoints       []checkpointJSON   `json:"checkpoints"`
	CheckpointsHidden bool               `json:"checkpoints_hidden"`
	TutorialSteps     []tutorialStepJSON `json:"tutorial_steps"`
	SubmitCount       int                `json:"submit_count"`
	ElapsedMS         int64              `json:"elapsed_ms"`
	StartedAt         *string            `json:"started_at"`
	EndedAt           *string            `json:"ended_at"`
	ServerTime        string             `json:"server_time"`
	CreatedAt         string             `json:"created_at"`
}

type submitCheckpointJSON struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type submitResultJSON struct {
	Passed       bool                   `json:"passed"`
	Checkpoints  []submitCheckpointJSON `json:"checkpoints"`
	HiddenFailed int                    `json:"hidden_failed"`
	SubmitCount  int                    `json:"submit_count"`
}

type resultJSON struct {
	AttemptID    string                 `json:"attempt_id"`
	Status       string                 `json:"status"`
	Lab          labSummary             `json:"lab"`
	ElapsedMS    int64                  `json:"elapsed_ms"`
	CommandCount int                    `json:"command_count"`
	SubmitCount  int                    `json:"submit_count"`
	Checkpoints  []resultCheckpointJSON `json:"checkpoints"`
	Solution     string                 `json:"solution"`
}

func healthOf(h runner.Health) healthJSON {
	return healthJSON{
		OK:             h.OK,
		Docker:         h.Docker,
		Image:          h.Image,
		ImageName:      h.ImageName,
		Instance:       h.Instance,
		MemAvailableMB: h.MemAvailableMB,
		Error:          h.Error,
	}
}

func (s *server) labSummaryOf(lab content.Lab, lang string) labSummary {
	modes := lab.Modes
	if modes == nil {
		modes = []string{}
	}
	related := make([]docRef, 0, len(lab.RelatedDocs))
	for _, id := range lab.RelatedDocs {
		doc, ok := s.docs[id]
		if !ok {
			continue
		}
		related = append(related, docRef{ID: doc.ID, Title: doc.Title.Get(lang)})
	}
	hidden := false
	for _, cp := range lab.Checkpoints {
		if !cp.Visible {
			hidden = true
			break
		}
	}
	return labSummary{
		ID:                   lab.Id,
		Title:                lab.Title.Get(lang),
		Topic:                lab.Topic,
		Level:                lab.Level,
		Modes:                modes,
		EstimatedMinutes:     lab.EstimatedMinutes,
		RelatedDocs:          related,
		HasHiddenCheckpoints: hidden,
	}
}

func (s *server) labDetailOf(lab content.Lab, lang string) labDetail {
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
	topicTitle := lab.Topic
	if topic, ok := s.content.Topics.Find(lab.Topic); ok {
		topicTitle = topic.Title.Get(lang)
	}
	return labDetail{
		labSummary:  s.labSummaryOf(lab, lang),
		Nodes:       nodes,
		Checkpoints: checkpoints,
		TopicTitle:  topicTitle,
	}
}

func (s *server) attemptOf(view attempt.View, lang string) attemptJSON {
	hidden := view.Mode == attempt.ModeReal
	checkpoints := []checkpointJSON{}
	if !hidden {
		checkpoints = visibleCheckpointsOf(view, lang)
	}
	return attemptJSON{
		ID:                view.Id,
		LabID:             view.LabID,
		Mode:              view.Mode,
		Status:            view.Status,
		ErrorMessage:      view.ErrorMessage,
		Lab:               s.labSummaryOf(view.Lab, lang),
		Ticket:            view.Ticket(lang),
		Nodes:             nodesOf(view),
		Checkpoints:       checkpoints,
		CheckpointsHidden: hidden,
		TutorialSteps:     tutorialStepsOf(view, lang),
		SubmitCount:       view.SubmitCount,
		ElapsedMS:         view.ElapsedMS,
		StartedAt:         formatTimePtr(view.StartedAt),
		EndedAt:           formatTimePtr(view.EndedAt),
		ServerTime:        formatTime(view.ServerTime),
		CreatedAt:         formatTime(view.CreatedAt),
	}
}

func (s *server) resultOf(view attempt.View, lang string, commandCount int, solution string) resultJSON {
	return resultJSON{
		AttemptID:    view.Id,
		Status:       view.Status,
		Lab:          s.labSummaryOf(view.Lab, lang),
		ElapsedMS:    view.ElapsedMS,
		CommandCount: commandCount,
		SubmitCount:  view.SubmitCount,
		Checkpoints:  allCheckpointsOf(view, lang),
		Solution:     solution,
	}
}

func submitResultOf(result attempt.SubmitResult, lang string) submitResultJSON {
	checkpoints := make([]submitCheckpointJSON, 0, len(result.Checkpoints))
	for _, cp := range result.Checkpoints {
		checkpoints = append(checkpoints, submitCheckpointJSON{ID: cp.Id, Title: cp.Title.Get(lang), Status: cp.Status})
	}
	return submitResultJSON{
		Passed:       result.Passed,
		Checkpoints:  checkpoints,
		HiddenFailed: result.HiddenFailed,
		SubmitCount:  result.SubmitCount,
	}
}

func tutorialStepsOf(view attempt.View, lang string) []tutorialStepJSON {
	if view.Mode != attempt.ModeTutorial {
		return nil
	}
	steps := make([]tutorialStepJSON, 0, len(view.TutorialSteps))
	for _, step := range view.TutorialSteps {
		steps = append(steps, tutorialStepJSON{Checkpoint: step.Checkpoint, Instruction: step.Instruction.Get(lang)})
	}
	return steps
}

func topicNodesOf(topics content.Topics, labs []content.Lab, docs []content.Doc, lang string) []topicNode {
	nodes := make([]topicNode, 0, len(topics))
	for _, topic := range topics {
		nodes = append(nodes, topicNode{
			ID:       topic.ID,
			Title:    topic.Title.Get(lang),
			Labs:     countInTopic(topic.ID, len(labs), func(i int) string { return labs[i].Topic }),
			Docs:     countInTopic(topic.ID, len(docs), func(i int) string { return docs[i].Topic }),
			Children: topicNodesOf(topic.Children, labs, docs, lang),
		})
	}
	return nodes
}

func countInTopic(topic string, n int, topicOf func(int) string) int {
	count := 0
	for i := range n {
		if inTopic(topicOf(i), topic) {
			count++
		}
	}
	return count
}

func inTopic(topic, root string) bool {
	return topic == root || strings.HasPrefix(topic, root+"/")
}

func nodesOf(view attempt.View) []nodeJSON {
	nodes := make([]nodeJSON, 0, len(view.Nodes))
	for _, node := range view.Nodes {
		nodes = append(nodes, nodeJSON{Name: node.Name, Role: node.Role})
	}
	return nodes
}

func checkpointOf(cp attempt.Checkpoint, lang string) checkpointJSON {
	return checkpointJSON{
		ID:            cp.Id,
		Title:         cp.Title.Get(lang),
		Status:        cp.Status,
		FirstPassedAt: formatTimePtr(cp.FirstPassedAt),
	}
}

func visibleCheckpointsOf(view attempt.View, lang string) []checkpointJSON {
	checkpoints := make([]checkpointJSON, 0, len(view.Checkpoints))
	for _, cp := range view.Checkpoints {
		if !cp.Visible {
			continue
		}
		checkpoints = append(checkpoints, checkpointOf(cp, lang))
	}
	return checkpoints
}

func allCheckpointsOf(view attempt.View, lang string) []resultCheckpointJSON {
	checkpoints := make([]resultCheckpointJSON, 0, len(view.Checkpoints))
	for _, cp := range view.Checkpoints {
		checkpoints = append(checkpoints, resultCheckpointJSON{checkpointJSON: checkpointOf(cp, lang), Visible: cp.Visible})
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
