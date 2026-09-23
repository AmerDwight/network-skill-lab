// Package contenttest gives tests a frozen content tree so that the engine
// suites never depend on the labs shipped in content/.
package contenttest

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/AmerDwight/network-skill-lab/internal/content"
)

func Dir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "testdata")
}

func Load(t testing.TB) *content.Content {
	t.Helper()
	loaded, err := content.LoadAll(Dir())
	if err != nil {
		t.Fatalf("load testdata content: %v", err)
	}
	return loaded
}
