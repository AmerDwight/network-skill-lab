package content

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
)

type Localized struct {
	Zh string `yaml:"zh"`
	En string `yaml:"en"`
}

func (l Localized) Get(lang string) string {
	switch {
	case lang == "zh" && l.Zh != "":
		return l.Zh
	case l.En != "":
		return l.En
	default:
		return l.Zh
	}
}

type Param struct {
	Name  string
	Gen   string
	Value string
}

type Params []Param

type paramSpec struct {
	Gen   string `yaml:"gen"`
	Value string `yaml:"value"`
}

func (p *Params) UnmarshalYAML(b []byte) error {
	var order yaml.MapSlice
	if err := yaml.Unmarshal(b, &order); err != nil {
		return err
	}
	specs := map[string]paramSpec{}
	if err := yaml.UnmarshalWithOptions(b, &specs, yaml.Strict()); err != nil {
		return err
	}
	params := make(Params, 0, len(order))
	for _, item := range order {
		name, ok := item.Key.(string)
		if !ok {
			return fmt.Errorf("param name %v is not a string", item.Key)
		}
		spec := specs[name]
		params = append(params, Param{Name: name, Gen: spec.Gen, Value: spec.Value})
	}
	*p = params
	return nil
}

func (p Params) Resolve() (map[string]string, error) {
	resolved := make(map[string]string, len(p))
	var errs []error
	for _, param := range p {
		value, err := Render(param.Value, resolved)
		if err != nil {
			errs = append(errs, fmt.Errorf("param %s: %w", param.Name, err))
			continue
		}
		resolved[param.Name] = value
	}
	return resolved, errors.Join(errs...)
}

func ParamsEnv(params map[string]string) map[string]string {
	env := make(map[string]string, len(params))
	for name, value := range params {
		env["NSL_"+strings.ToUpper(name)] = value
	}
	return env
}

type Checkpoint struct {
	Id      string    `yaml:"id"`
	Title   Localized `yaml:"title"`
	Node    string    `yaml:"node"`
	Script  string    `yaml:"script"`
	Visible bool      `yaml:"visible"`
}

type Lab struct {
	Id               string       `yaml:"id"`
	Version          int          `yaml:"version"`
	Title            Localized    `yaml:"title"`
	Topic            string       `yaml:"topic"`
	Level            int          `yaml:"level"`
	Modes            []string     `yaml:"modes"`
	Environment      string       `yaml:"environment"`
	EstimatedMinutes int          `yaml:"estimated_minutes"`
	Ticket           Localized    `yaml:"ticket"`
	Params           Params       `yaml:"params"`
	Setup            string       `yaml:"setup"`
	RelatedDocs      []string     `yaml:"related_docs"`
	Checkpoints      []Checkpoint `yaml:"checkpoints"`
	Solution         Localized    `yaml:"solution"`

	Topology Topology `yaml:"-"`
	Dir      string   `yaml:"-"`
}

type Node struct {
	Role string `yaml:"role"`
}

type Endpoint struct {
	Node  string
	Iface string
}

func (e *Endpoint) UnmarshalYAML(b []byte) error {
	var s string
	if err := yaml.Unmarshal(b, &s); err != nil {
		return err
	}
	node, iface, ok := strings.Cut(s, ":")
	if !ok {
		return fmt.Errorf("endpoint %q must be written as node:iface", s)
	}
	e.Node, e.Iface = node, iface
	return nil
}

type Link struct {
	Endpoints [2]Endpoint       `yaml:"endpoints"`
	Subnet    string            `yaml:"subnet"`
	Addresses map[string]string `yaml:"addresses"`
}

type Topology struct {
	Nodes map[string]Node `yaml:"nodes"`
	Links []Link          `yaml:"links"`
}

func (t Topology) Resolve(params map[string]string) (Topology, error) {
	resolved := Topology{Nodes: maps.Clone(t.Nodes), Links: make([]Link, 0, len(t.Links))}
	var errs []error
	for i, link := range t.Links {
		subnet, err := Render(link.Subnet, params)
		if err != nil {
			errs = append(errs, fmt.Errorf("links[%d].subnet: %w", i, err))
		}
		addresses := make(map[string]string, len(link.Addresses))
		for node, address := range link.Addresses {
			rendered, err := Render(address, params)
			if err != nil {
				errs = append(errs, fmt.Errorf("links[%d].addresses.%s: %w", i, node, err))
				continue
			}
			addresses[node] = rendered
		}
		resolved.Links = append(resolved.Links, Link{Endpoints: link.Endpoints, Subnet: subnet, Addresses: addresses})
	}
	if err := errors.Join(errs...); err != nil {
		return Topology{}, err
	}
	return resolved, nil
}

var placeholder = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

func Render(s string, params map[string]string) (string, error) {
	var errs []error
	out := placeholder.ReplaceAllStringFunc(s, func(match string) string {
		name := placeholder.FindStringSubmatch(match)[1]
		value, ok := params[name]
		if !ok {
			errs = append(errs, fmt.Errorf("unknown param %q", name))
			return match
		}
		return value
	})
	if err := errors.Join(errs...); err != nil {
		return "", err
	}
	return out, nil
}
