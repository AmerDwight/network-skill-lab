package content

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
)

func Load(dir string) ([]Lab, error) {
	labsDir := filepath.Join(dir, "labs")
	entries, err := os.ReadDir(labsDir)
	if err != nil {
		return nil, fmt.Errorf("read labs directory: %w", err)
	}

	var (
		labs []Lab
		errs []error
	)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		lab, err := loadLab(filepath.Join(labsDir, entry.Name()))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		errs = append(errs, validate(lab)...)
		labs = append(labs, *lab)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return labs, nil
}

func loadLab(dir string) (*Lab, error) {
	var lab Lab
	if err := decodeFile(filepath.Join(dir, "lab.yaml"), &lab); err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	if err := decodeFile(filepath.Join(dir, "topology.yaml"), &lab.Topology); err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	lab.Dir = dir
	return &lab, nil
}

func decodeFile(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	if err := yaml.UnmarshalWithOptions(b, v, yaml.Strict()); err != nil {
		return fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	return nil
}
