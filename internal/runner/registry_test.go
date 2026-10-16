package runner

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type stubRunner struct {
	caps []Env
}

func (s stubRunner) Capabilities() []Env { return s.caps }
func (stubRunner) Provision(context.Context, SandboxSpec) (SandboxID, error) {
	return "", nil
}
func (stubRunner) OpenTerminal(context.Context, SandboxID, string) (PTY, error) { return nil, nil }
func (stubRunner) Exec(context.Context, SandboxID, string, []string, ExecOptions) (ExecResult, error) {
	return ExecResult{}, nil
}
func (stubRunner) Destroy(context.Context, SandboxID) error { return nil }
func (stubRunner) GC(context.Context) error                 { return nil }
func (stubRunner) Health(context.Context) Health            { return Health{OK: true} }

type stubRemotes struct {
	members []Member
	runners map[string]Runner
}

func (s stubRemotes) Members() []Member { return append([]Member(nil), s.members...) }

func (s stubRemotes) Get(name string) (Runner, bool) {
	r, ok := s.runners[name]
	return r, ok
}

func container(name string, capacity int) Member {
	return Member{Name: name, Capabilities: []Env{EnvContainer}, Capacity: capacity, Connected: true}
}

func TestScheduleLeastLoaded(t *testing.T) {
	members := []Member{container("local", 3), container("lab-02", 4), container("lab-01", 2)}
	tests := []struct {
		name   string
		active map[string]int
		want   string
	}{
		{"all idle picks the first name", map[string]int{}, "lab-01"},
		{"lowest share wins", map[string]int{"lab-01": 1, "lab-02": 1, "local": 1}, "lab-02"},
		{"full runner is skipped", map[string]int{"lab-01": 2, "lab-02": 3, "local": 2}, "local"},
		{"equal share ties by name", map[string]int{"lab-01": 1, "lab-02": 2, "local": 2}, "lab-01"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Schedule(members, tt.active, EnvContainer)
			if err != nil {
				t.Fatalf("Schedule() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Schedule() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestScheduleFiltersCapabilityAndConnection(t *testing.T) {
	vm := Member{Name: "kvm-01", Capabilities: []Env{EnvContainer, EnvVM}, Capacity: 1, Connected: true}
	offline := Member{Name: "kvm-00", Capabilities: []Env{EnvVM}, Capacity: 5}
	members := []Member{container("local", 3), vm, offline}

	got, err := Schedule(members, map[string]int{}, EnvVM)
	if err != nil || got != "kvm-01" {
		t.Fatalf("Schedule(vm) = %q, %v, want kvm-01", got, err)
	}
}

func TestScheduleAllBusy(t *testing.T) {
	members := []Member{container("local", 1), container("lab-01", 2), {Name: "kvm-01", Capabilities: []Env{EnvVM}, Capacity: 1, Connected: true}}
	_, err := Schedule(members, map[string]int{"local": 1, "lab-01": 2}, EnvContainer)

	var busy BusyError
	if !errors.As(err, &busy) {
		t.Fatalf("Schedule() error = %v, want BusyError", err)
	}
	if !errors.Is(err, ErrBusy) {
		t.Error("BusyError does not unwrap to ErrBusy")
	}
	want := []Load{{Name: "lab-01", Active: 2, Capacity: 2}, {Name: "local", Active: 1, Capacity: 1}}
	if !reflect.DeepEqual(busy.Runners, want) {
		t.Errorf("busy runners = %+v, want %+v", busy.Runners, want)
	}
}

func TestScheduleNoCapableRunner(t *testing.T) {
	offline := Member{Name: "kvm-01", Capabilities: []Env{EnvVM}, Capacity: 1}
	for _, members := range [][]Member{nil, {container("local", 3), offline}} {
		_, err := Schedule(members, map[string]int{}, EnvVM)
		if !errors.Is(err, ErrNoRunner) {
			t.Errorf("Schedule(%v) error = %v, want ErrNoRunner", members, err)
		}
		if errors.Is(err, ErrBusy) {
			t.Error("a missing capability reads as busy")
		}
	}
}

func TestRegistryWithLocalRunner(t *testing.T) {
	local := stubRunner{caps: []Env{EnvContainer}}
	remote := stubRunner{caps: []Env{EnvVM}}
	reg := NewRegistry(local, 2, stubRemotes{
		members: []Member{
			{Name: "zeta", Capabilities: []Env{EnvVM}, Capacity: 1, Connected: true},
			{Name: LocalName, Capabilities: []Env{EnvVM}, Capacity: 9, Connected: true},
			{Name: "alpha", Capabilities: []Env{EnvContainer}, Capacity: 3},
		},
		runners: map[string]Runner{"zeta": remote, LocalName: remote},
	})

	want := []Member{
		{Name: LocalName, Capabilities: []Env{EnvContainer}, Capacity: 2, Connected: true},
		{Name: "alpha", Capabilities: []Env{EnvContainer}, Capacity: 3},
		{Name: "zeta", Capabilities: []Env{EnvVM}, Capacity: 1, Connected: true},
	}
	if got := reg.Members(); !reflect.DeepEqual(got, want) {
		t.Errorf("Members() = %+v, want %+v", got, want)
	}
	if got, ok := reg.Get(LocalName); !ok || !reflect.DeepEqual(got, Runner(local)) {
		t.Errorf("Get(local) = %v, %v, want the in-process runner", got, ok)
	}
	if got, ok := reg.Get("zeta"); !ok || !reflect.DeepEqual(got, Runner(remote)) {
		t.Errorf("Get(zeta) = %v, %v", got, ok)
	}
	if _, ok := reg.Get("alpha"); ok {
		t.Error("Get(alpha) found a runner that is not connected")
	}
}

func TestRegistryWithoutLocalRunner(t *testing.T) {
	reg := NewRegistry(nil, 3, nil)
	if got := reg.Members(); len(got) != 0 {
		t.Errorf("Members() = %+v, want none", got)
	}
	if _, ok := reg.Local(); ok {
		t.Error("Local() reports a runner")
	}
	if _, ok := reg.Get(LocalName); ok {
		t.Error("Get(local) found a runner")
	}

	reg = NewRegistry(nil, 3, stubRemotes{members: []Member{container("lab-01", 2)}})
	if got := reg.Members(); len(got) != 1 || got[0].Name != "lab-01" {
		t.Errorf("Members() = %+v, want only lab-01", got)
	}
}
