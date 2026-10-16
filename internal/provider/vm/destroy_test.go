package vm

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
)

func newState() *libvirtState {
	return &libvirtState{
		domains: map[string]owner{
			"nsl-inst1-att1-web01": {Instance: "inst1", Attempt: "att1"},
			"nsl-inst1-att1-db01":  {Instance: "inst1", Attempt: "att1"},
			"nsl-inst1-att2-web01": {Instance: "inst1", Attempt: "att2"},
			"nsl-inst2-att1-web01": {Instance: "inst2", Attempt: "att1"},
			"nsl-vm-build-123":     {},
		},
		networks: map[string]owner{
			"nsl-inst1-att1-l1": {Instance: "inst1", Attempt: "att1"},
			"nsl-inst1-att2-l1": {Instance: "inst1", Attempt: "att2"},
			"nsl-inst2-att1-l1": {Instance: "inst2", Attempt: "att1"},
		},
		running: map[string]bool{"nsl-inst1-att1-web01": true, "nsl-inst1-att2-web01": true, "nsl-inst2-att1-web01": true},
	}
}

func remaining(m map[string]owner) []string {
	var names []string
	for name := range m {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func mkdirs(t *testing.T, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "web01.qcow2"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDestroyRemovesOnlyTheSandbox(t *testing.T) {
	state := newState()
	v := &fakeVirsh{handle: state.handle}
	p := newTestProvider(t, v)
	own := sandboxDir(p.cfg.Dir, "inst1", "att1")
	other := sandboxDir(p.cfg.Dir, "inst1", "att2")
	mkdirs(t, own, other)

	if err := p.Destroy(t.Context(), "att1"); err != nil {
		t.Fatalf("Destroy() error = %v", err)
	}
	if got, want := remaining(state.domains), []string{"nsl-inst1-att2-web01", "nsl-inst2-att1-web01", "nsl-vm-build-123"}; !slices.Equal(got, want) {
		t.Errorf("domains left = %v, want %v", got, want)
	}
	if got, want := remaining(state.networks), []string{"nsl-inst1-att2-l1", "nsl-inst2-att1-l1"}; !slices.Equal(got, want) {
		t.Errorf("networks left = %v, want %v", got, want)
	}
	if _, err := os.Stat(own); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("sandbox dir still present: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("other sandbox dir removed: %v", err)
	}
	for _, call := range v.called("dumpxml") {
		if !strings.HasPrefix(call[1], namePrefix) {
			t.Errorf("dumped a domain nsl does not name: %v", call)
		}
	}

	if err := p.Destroy(t.Context(), "att1"); !errors.Is(err, runner.ErrSandboxNotFound) {
		t.Errorf("second Destroy() error = %v, want ErrSandboxNotFound", err)
	}
}

func TestDestroyWithOnlyFilesLeft(t *testing.T) {
	state := newState()
	p := newTestProvider(t, &fakeVirsh{handle: state.handle})
	dir := sandboxDir(p.cfg.Dir, "inst1", "att9")
	mkdirs(t, dir)
	if err := p.Destroy(t.Context(), "att9"); err != nil {
		t.Fatalf("Destroy() error = %v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("sandbox dir still present: %v", err)
	}
}

func TestDestroyReportsRemovalErrors(t *testing.T) {
	state := newState()
	p := newTestProvider(t, &fakeVirsh{handle: func(args []string) ([]byte, error) {
		if args[0] == "undefine" {
			return nil, errors.New("error: internal error")
		}
		return state.handle(args)
	}})
	if err := p.Destroy(t.Context(), "att1"); err == nil || errors.Is(err, runner.ErrSandboxNotFound) {
		t.Errorf("Destroy() error = %v, want the undefine failure", err)
	}
}

func TestGCCollectsTheInstance(t *testing.T) {
	state := newState()
	p := newTestProvider(t, &fakeVirsh{handle: state.handle})
	mine := []string{sandboxDir(p.cfg.Dir, "inst1", "att1"), sandboxDir(p.cfg.Dir, "inst1", "stale")}
	foreign := sandboxDir(p.cfg.Dir, "inst2", "att1")
	mkdirs(t, append(mine, foreign)...)

	if err := p.GC(t.Context()); err != nil {
		t.Fatalf("GC() error = %v", err)
	}
	if got, want := remaining(state.domains), []string{"nsl-inst2-att1-web01", "nsl-vm-build-123"}; !slices.Equal(got, want) {
		t.Errorf("domains left = %v, want %v", got, want)
	}
	if got, want := remaining(state.networks), []string{"nsl-inst2-att1-l1"}; !slices.Equal(got, want) {
		t.Errorf("networks left = %v, want %v", got, want)
	}
	for _, dir := range mine {
		if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s still present: %v", dir, err)
		}
	}
	if _, err := os.Stat(instanceDir(p.cfg.Dir, "inst1")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("instance dir still present: %v", err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Errorf("another instance's dir was removed: %v", err)
	}
	if err := p.GC(t.Context()); err != nil {
		t.Errorf("second GC() error = %v", err)
	}
}
