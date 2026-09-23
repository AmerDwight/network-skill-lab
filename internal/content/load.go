package content

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

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
	loadCases(&lab)
	if lab.Precheck != nil && lab.Precheck.Retries == 0 {
		lab.Precheck.Retries = defaultPrecheckRetries
	}
	return &lab, nil
}

func loadCases(lab *Lab) {
	for i := range lab.Cases {
		entry := &lab.Cases[i]
		entry.ID = caseID(entry.File)
		if entry.File == "" {
			continue
		}
		var file caseFile
		if err := decodeFile(filepath.Join(lab.Dir, filepath.FromSlash(entry.File)), &file); err != nil {
			lab.caseErrs = append(lab.caseErrs, labErrf(lab, fmt.Sprintf("cases[%d].file", i), "%v", err))
			continue
		}
		entry.Params, entry.SetupEnv = file.Params, file.SetupEnv
	}
}

func caseID(file string) string {
	name := path.Base(path.Clean(file))
	return strings.TrimSuffix(name, path.Ext(name))
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
