package content

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type Topic struct {
	ID       string    `yaml:"id"`
	Title    Localized `yaml:"title"`
	Children []Topic   `yaml:"children"`
}

type Topics []Topic

func (t Topics) Find(id string) (*Topic, bool) {
	for i := range t {
		if t[i].ID == id {
			return &t[i], true
		}
		if found, ok := Topics(t[i].Children).Find(id); ok {
			return found, true
		}
	}
	return nil, false
}

func loadTopics(dir string) (Topics, bool, error) {
	path := filepath.Join(dir, "topics.yaml")
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	var topics Topics
	if err := decodeFile(path, &topics); err != nil {
		return nil, true, fmt.Errorf("%s: topics: %v", path, err)
	}
	if err := errors.Join(validateTopics(path, "", topics, map[string]bool{})...); err != nil {
		return topics, true, err
	}
	return topics, true, nil
}

func validateTopics(path, parent string, topics Topics, seen map[string]bool) []error {
	var errs []error
	for i := range topics {
		topic := &topics[i]
		field := fmt.Sprintf("topics[%s]", topic.ID)
		switch {
		case topic.ID == "":
			errs = append(errs, fmt.Errorf("%s: topics: an entry under %q has no id", path, parent))
			continue
		case strings.Contains(topic.ID, "/"):
			errs = append(errs, fmt.Errorf("%s: %s: id must not contain %q, nesting builds the full id", path, field, "/"))
			continue
		}
		if parent != "" {
			topic.ID = parent + "/" + topic.ID
			field = fmt.Sprintf("topics[%s]", topic.ID)
		}
		if seen[topic.ID] {
			errs = append(errs, fmt.Errorf("%s: %s: duplicate topic id %q", path, field, topic.ID))
		}
		seen[topic.ID] = true
		for _, title := range localizedFields(field+".title", topic.Title) {
			if title.text == "" {
				errs = append(errs, fmt.Errorf("%s: %s: must not be empty", path, title.name))
			}
		}
		errs = append(errs, validateTopics(path, topic.ID, topic.Children, seen)...)
	}
	return errs
}
