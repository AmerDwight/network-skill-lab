package runner

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

const LocalName = "local"

var (
	ErrNoRunner = errors.New("no connected runner provides this environment")
	ErrBusy     = errors.New("all runners are at capacity")
)

type Member struct {
	Name         string
	Capabilities []Env
	Capacity     int
	Connected    bool
}

type Load struct {
	Name     string
	Active   int
	Capacity int
}

type BusyError struct {
	Runners []Load
}

func (e BusyError) Error() string {
	parts := make([]string, 0, len(e.Runners))
	for _, load := range e.Runners {
		parts = append(parts, fmt.Sprintf("%s %d/%d", load.Name, load.Active, load.Capacity))
	}
	return fmt.Sprintf("%s: %s", ErrBusy, strings.Join(parts, ", "))
}

func (e BusyError) Unwrap() error { return ErrBusy }

type Remotes interface {
	Members() []Member
	Get(name string) (Runner, bool)
}

type Registry struct {
	local         Runner
	localCapacity int
	remotes       Remotes
}

// NewRegistry builds a registry of the in-process runner, which is nil when the host
// has no Docker, and the remote runners, which are nil when no hub is mounted.
func NewRegistry(local Runner, localCapacity int, remotes Remotes) *Registry {
	return &Registry{local: local, localCapacity: localCapacity, remotes: remotes}
}

func (r *Registry) Local() (Runner, bool) {
	return r.local, r.local != nil
}

func (r *Registry) Members() []Member {
	var members []Member
	if r.local != nil {
		members = append(members, Member{
			Name:         LocalName,
			Capabilities: slices.Clone(r.local.Capabilities()),
			Capacity:     r.localCapacity,
			Connected:    true,
		})
	}
	if r.remotes == nil {
		return members
	}
	remote := slices.DeleteFunc(r.remotes.Members(), func(m Member) bool { return m.Name == LocalName })
	slices.SortFunc(remote, func(a, b Member) int { return strings.Compare(a.Name, b.Name) })
	return append(members, remote...)
}

func (r *Registry) Get(name string) (Runner, bool) {
	if name == LocalName {
		return r.Local()
	}
	if r.remotes == nil {
		return nil, false
	}
	return r.remotes.Get(name)
}

// Schedule picks the connected runner with the capability that has the lowest share
// of its capacity in use; ties go to the alphabetically first name.
func Schedule(members []Member, active map[string]int, env Env) (string, error) {
	var capable []Member
	for _, m := range members {
		if m.Connected && slices.Contains(m.Capabilities, env) {
			capable = append(capable, m)
		}
	}
	if len(capable) == 0 {
		return "", fmt.Errorf("%w: %s", ErrNoRunner, env)
	}
	slices.SortFunc(capable, func(a, b Member) int { return strings.Compare(a.Name, b.Name) })

	best := -1
	for i, m := range capable {
		if m.Capacity <= 0 || active[m.Name] >= m.Capacity {
			continue
		}
		if best < 0 || active[m.Name]*capable[best].Capacity < active[capable[best].Name]*m.Capacity {
			best = i
		}
	}
	if best >= 0 {
		return capable[best].Name, nil
	}

	loads := make([]Load, 0, len(capable))
	for _, m := range capable {
		loads = append(loads, Load{Name: m.Name, Active: active[m.Name], Capacity: m.Capacity})
	}
	return "", BusyError{Runners: loads}
}
