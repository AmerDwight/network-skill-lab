package content

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type TrackStep struct {
	Doc  string `yaml:"doc"`
	Lab  string `yaml:"lab"`
	Mode string `yaml:"mode"`
}

type Track struct {
	ID    string      `yaml:"id"`
	Title Localized   `yaml:"title"`
	Steps []TrackStep `yaml:"steps"`

	Path string `yaml:"-"`
}

func loadTracks(dir string) ([]Track, error) {
	tracksDir := filepath.Join(dir, "tracks")
	entries, err := os.ReadDir(tracksDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read tracks directory: %w", err)
	}

	var (
		tracks []Track
		errs   []error
	)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		path := filepath.Join(tracksDir, entry.Name())
		var track Track
		if err := decodeFile(path, &track); err != nil {
			errs = append(errs, fmt.Errorf("%s: track: %v", path, err))
			continue
		}
		track.Path = path
		errs = append(errs, validateTrack(&track, strings.TrimSuffix(entry.Name(), ".yaml"))...)
		tracks = append(tracks, track)
	}
	if err := errors.Join(errs...); err != nil {
		return tracks, err
	}
	return tracks, nil
}

func validateTrack(track *Track, name string) []error {
	var errs []error
	if track.ID != name {
		errs = append(errs, fmt.Errorf("%s: id: %q does not match file name %q", track.Path, track.ID, name))
	}
	for _, title := range localizedFields("title", track.Title) {
		if title.text == "" {
			errs = append(errs, fmt.Errorf("%s: %s: must not be empty", track.Path, title.name))
		}
	}
	if len(track.Steps) == 0 {
		errs = append(errs, fmt.Errorf("%s: steps: must not be empty", track.Path))
	}
	for i, step := range track.Steps {
		field := fmt.Sprintf("steps[%d]", i)
		switch {
		case step.Doc != "" && step.Lab != "":
			errs = append(errs, fmt.Errorf("%s: %s: must set either doc or lab, not both", track.Path, field))
		case step.Doc == "" && step.Lab == "":
			errs = append(errs, fmt.Errorf("%s: %s: must set either doc or lab", track.Path, field))
		case step.Lab != "" && step.Mode == "":
			errs = append(errs, fmt.Errorf("%s: %s: a lab step must set mode", track.Path, field))
		case step.Doc != "" && step.Mode != "":
			errs = append(errs, fmt.Errorf("%s: %s: mode is only valid for lab steps", track.Path, field))
		}
	}
	return errs
}
