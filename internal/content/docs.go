package content

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

var docLangs = []string{"zh", "en"}

type Doc struct {
	ID    string
	Title Localized
	Paths map[string]string
	Topic string
}

func (d Doc) Path() string {
	for _, lang := range docLangs {
		if p, ok := d.Paths[lang]; ok {
			return p
		}
	}
	return d.ID
}

func (d Doc) Body(lang string) (string, error) {
	p, ok := d.Paths[lang]
	if !ok {
		p, ok = d.Paths["en"]
	}
	if !ok {
		return "", fmt.Errorf("doc %s has no %s body", d.ID, lang)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", p, err)
	}
	return string(b), nil
}

func loadDocs(dir string) ([]Doc, error) {
	docsDir := filepath.Join(dir, "docs")
	if _, err := os.Stat(docsDir); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	docs := map[string]*Doc{}
	var errs []error
	walkErr := filepath.WalkDir(docsDir, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			return nil
		}
		name := strings.TrimSuffix(entry.Name(), ".md")
		base, lang, ok := cutLast(name, ".")
		if !ok || !slices.Contains(docLangs, lang) {
			errs = append(errs, fmt.Errorf("%s: name: expected <name>.<zh|en>.md", p))
			return nil
		}
		rel, err := filepath.Rel(docsDir, filepath.Join(filepath.Dir(p), base))
		if err != nil {
			return err
		}
		id := filepath.ToSlash(rel)
		doc, ok := docs[id]
		if !ok {
			doc = &Doc{ID: id, Paths: map[string]string{}, Topic: docTopic(id)}
			docs[id] = doc
		}
		doc.Paths[lang] = p
		title, err := firstHeading(p)
		if err != nil {
			errs = append(errs, err)
			return nil
		}
		switch lang {
		case "zh":
			doc.Title.Zh = title
		case "en":
			doc.Title.En = title
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("read docs directory: %w", walkErr)
	}

	loaded := make([]Doc, 0, len(docs))
	for _, id := range slices.Sorted(maps.Keys(docs)) {
		doc := docs[id]
		for _, lang := range docLangs {
			if _, ok := doc.Paths[lang]; !ok {
				errs = append(errs, fmt.Errorf("%s: doc: %q has no %s version", doc.Path(), id, lang))
			}
		}
		loaded = append(loaded, *doc)
	}
	if err := errors.Join(errs...); err != nil {
		return loaded, err
	}
	return loaded, nil
}

func docTopic(id string) string {
	topic := path.Dir(id)
	if topic == "." {
		return ""
	}
	return topic
}

func cutLast(s, sep string) (before, after string, found bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+len(sep):], true
}

func firstHeading(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", fmt.Errorf("%s: title: %v", p, err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if after, ok := strings.CutPrefix(line, "# "); ok {
			title := strings.TrimSpace(after)
			if title == "" {
				break
			}
			return title, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("%s: title: %v", p, err)
	}
	return "", fmt.Errorf(`%s: title: no "# " heading found`, p)
}
