package content

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

type Content struct {
	Labs   []Lab
	Docs   []Doc
	Topics Topics
	Tracks []Track
}

func LoadAll(dir string) (*Content, error) {
	var (
		all  Content
		errs []error
	)

	if _, err := os.Stat(filepath.Join(dir, "labs")); err == nil {
		labs, err := Load(dir)
		if err != nil {
			errs = append(errs, err)
		}
		all.Labs = labs
	} else if !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, fmt.Errorf("%s: labs: %v", filepath.Join(dir, "labs"), err))
	}

	docs, err := loadDocs(dir)
	if err != nil {
		errs = append(errs, err)
	}
	all.Docs = docs

	topics, found, err := loadTopics(dir)
	if err != nil {
		errs = append(errs, err)
	}
	all.Topics = topics

	tracks, err := loadTracks(dir)
	if err != nil {
		errs = append(errs, err)
	}
	all.Tracks = tracks

	if !found && (len(all.Labs) > 0 || len(all.Docs) > 0) {
		errs = append(errs, fmt.Errorf("%s: topics: file is required when the content directory has labs or docs", filepath.Join(dir, "topics.yaml")))
	}

	errs = append(errs, validateRefs(&all)...)
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return &all, nil
}

func validateRefs(all *Content) []error {
	var errs []error

	docs := map[string]Doc{}
	for _, doc := range all.Docs {
		if _, ok := docs[doc.ID]; ok {
			errs = append(errs, fmt.Errorf("%s: id: duplicate doc id %q", doc.Path(), doc.ID))
			continue
		}
		docs[doc.ID] = doc
		if doc.Topic == "" {
			errs = append(errs, fmt.Errorf("%s: topic: a doc must live in a topic directory", doc.Path()))
			continue
		}
		if _, ok := all.Topics.Find(doc.Topic); !ok {
			errs = append(errs, fmt.Errorf("%s: topic: %q is not in topics.yaml", doc.Path(), doc.Topic))
		}
	}

	labs := map[string]Lab{}
	for _, lab := range all.Labs {
		labs[lab.Id] = lab
		if _, ok := all.Topics.Find(lab.Topic); !ok {
			errs = append(errs, labErrf(&lab, "topic", "%q is not in topics.yaml", lab.Topic))
		}
		for i, id := range lab.RelatedDocs {
			if _, ok := docs[id]; !ok {
				errs = append(errs, labErrf(&lab, fmt.Sprintf("related_docs[%d]", i), "%q is not a known doc", id))
			}
		}
	}

	for _, track := range all.Tracks {
		for i, step := range track.Steps {
			field := fmt.Sprintf("steps[%d]", i)
			if step.Doc != "" {
				if _, ok := docs[step.Doc]; !ok {
					errs = append(errs, fmt.Errorf("%s: %s: doc %q does not exist", track.Path, field, step.Doc))
				}
				continue
			}
			lab, ok := labs[step.Lab]
			if !ok {
				errs = append(errs, fmt.Errorf("%s: %s: lab %q does not exist", track.Path, field, step.Lab))
				continue
			}
			if step.Mode != "" && !slices.Contains(lab.Modes, step.Mode) {
				errs = append(errs, fmt.Errorf("%s: %s: lab %q does not support mode %q", track.Path, field, step.Lab, step.Mode))
			}
		}
	}

	return errs
}
