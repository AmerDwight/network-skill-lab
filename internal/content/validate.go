package content

import (
	"fmt"
	"maps"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
)

type localizedField struct {
	name string
	text string
}

func localizedFields(prefix string, l Localized) []localizedField {
	return []localizedField{{prefix + ".zh", l.Zh}, {prefix + ".en", l.En}}
}

const (
	validationSeed         = 1
	defaultPrecheckRetries = 3
	minPrecheckRetries     = 1
	maxPrecheckRetries     = 5
	tutorialMode           = "tutorial"
	k3sServerRole          = "k3s-server"
)

var (
	allowedModes = []string{tutorialMode, "guided", "real"}
	allowedRoles = []string{"ubuntu", "ubuntu-nm", k3sServerRole, "k3s-agent"}
	allowedGens  = []string{GenConst, GenCIDR, GenIPIn, GenChoice, GenInt}
)

var checks = []func(*Lab) []error{
	checkID,
	checkVersion,
	checkModes,
	checkEnvironment,
	checkLevel,
	checkParams,
	checkTicket,
	checkCheckpoints,
	checkScripts,
	checkRoles,
	checkLinks,
	checkSolution,
	checkCases,
	checkPrecheck,
	checkRequires,
	checkTutorial,
	checkK3s,
}

func validate(lab *Lab) []error {
	var errs []error
	for _, check := range checks {
		errs = append(errs, check(lab)...)
	}
	return errs
}

func labErrf(lab *Lab, field, format string, args ...any) error {
	return fmt.Errorf("%s: %s: %s", lab.Dir, field, fmt.Sprintf(format, args...))
}

func checkID(lab *Lab) []error {
	if name := filepath.Base(lab.Dir); lab.Id != name {
		return []error{labErrf(lab, "id", "%q does not match directory name %q", lab.Id, name)}
	}
	return nil
}

func checkVersion(lab *Lab) []error {
	if lab.Version != 1 {
		return []error{labErrf(lab, "version", "must be 1, got %d", lab.Version)}
	}
	return nil
}

func checkModes(lab *Lab) []error {
	if len(lab.Modes) == 0 {
		return []error{labErrf(lab, "modes", "must not be empty")}
	}
	var errs []error
	for _, mode := range lab.Modes {
		if !slices.Contains(allowedModes, mode) {
			errs = append(errs, labErrf(lab, "modes", "%q is not supported, allowed: %v", mode, allowedModes))
		}
	}
	return errs
}

func checkEnvironment(lab *Lab) []error {
	if lab.Environment != "container" {
		return []error{labErrf(lab, "environment", "must be \"container\", got %q", lab.Environment)}
	}
	return nil
}

func checkLevel(lab *Lab) []error {
	if lab.Level < 1 || lab.Level > 5 {
		return []error{labErrf(lab, "level", "must be between 1 and 5, got %d", lab.Level)}
	}
	return nil
}

func checkParams(lab *Lab) []error {
	var errs []error
	for _, param := range lab.Params {
		errs = append(errs, checkParamFields(lab, "params."+param.Name, param)...)
	}
	lab.Params.resolve(validationSeed, func(name string, err error) {
		errs = append(errs, labErrf(lab, "params."+name, "%v", err))
	})
	return errs
}

func checkTicket(lab *Lab) []error {
	params, _ := lab.Params.Resolve(validationSeed)
	var errs []error
	for _, field := range localizedFields("ticket", lab.Ticket) {
		if _, err := Render(field.text, params); err != nil {
			errs = append(errs, labErrf(lab, field.name, "%v", err))
		}
	}
	return errs
}

func checkCheckpoints(lab *Lab) []error {
	params, _ := lab.Params.Resolve(validationSeed)
	var errs []error
	seen := map[string]bool{}
	for i, cp := range lab.Checkpoints {
		field := fmt.Sprintf("checkpoints[%d]", i)
		if seen[cp.Id] {
			errs = append(errs, labErrf(lab, field+".id", "duplicate checkpoint id %q", cp.Id))
		}
		seen[cp.Id] = true
		if _, ok := lab.Topology.Nodes[cp.Node]; !ok {
			errs = append(errs, labErrf(lab, field+".node", "node %q is not declared in topology.yaml", cp.Node))
		}
		for _, title := range localizedFields(field+".title", cp.Title) {
			if _, err := Render(title.text, params); err != nil {
				errs = append(errs, labErrf(lab, title.name, "%v", err))
			}
		}
	}
	return errs
}

func checkScripts(lab *Lab) []error {
	errs := checkExecutable(lab, "setup", lab.Setup)
	for i, cp := range lab.Checkpoints {
		errs = append(errs, checkExecutable(lab, fmt.Sprintf("checkpoints[%d].script", i), cp.Script)...)
	}
	return errs
}

func checkExecutable(lab *Lab, field, rel string) []error {
	if rel == "" {
		return []error{labErrf(lab, field, "must not be empty")}
	}
	info, err := os.Stat(filepath.Join(lab.Dir, rel))
	if err != nil {
		return []error{labErrf(lab, field, "%v", err)}
	}
	if !info.Mode().IsRegular() {
		return []error{labErrf(lab, field, "%s is not a regular file", rel)}
	}
	if info.Mode().Perm()&0o111 == 0 {
		return []error{labErrf(lab, field, "%s is not executable", rel)}
	}
	return nil
}

func checkRoles(lab *Lab) []error {
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(lab.Topology.Nodes)) {
		if role := lab.Topology.Nodes[name].Role; !slices.Contains(allowedRoles, role) {
			errs = append(errs, labErrf(lab, "nodes."+name+".role", "%q is not supported, allowed: %v", role, allowedRoles))
		}
	}
	return errs
}

func checkLinks(lab *Lab) []error {
	params, _ := lab.Params.Resolve(validationSeed)
	var errs []error
	for i, link := range lab.Topology.Links {
		field := fmt.Sprintf("links[%d]", i)
		var nodes []string
		for j, endpoint := range link.Endpoints {
			if _, ok := lab.Topology.Nodes[endpoint.Node]; !ok {
				errs = append(errs, labErrf(lab, fmt.Sprintf("%s.endpoints[%d]", field, j), "node %q is not declared in topology.yaml", endpoint.Node))
			}
			if endpoint.Iface == "" || endpoint.Iface == "eth0" {
				errs = append(errs, labErrf(lab, fmt.Sprintf("%s.endpoints[%d]", field, j), "interface %q is reserved for the management network", endpoint.Iface))
			}
			if slices.Contains(nodes, endpoint.Node) {
				errs = append(errs, labErrf(lab, field+".endpoints", "node %q appears twice on the same link", endpoint.Node))
			}
			nodes = append(nodes, endpoint.Node)
		}
		errs = append(errs, checkAddresses(lab, field, link, nodes, params)...)
	}
	return errs
}

func checkAddresses(lab *Lab, field string, link Link, nodes []string, params map[string]string) []error {
	rendered, err := Render(link.Subnet, params)
	if err != nil {
		return []error{labErrf(lab, field+".subnet", "%v", err)}
	}
	subnet, err := netip.ParsePrefix(rendered)
	if err != nil {
		return []error{labErrf(lab, field+".subnet", "%v", err)}
	}

	var errs []error
	for _, node := range slices.Sorted(maps.Keys(link.Addresses)) {
		if !slices.Contains(nodes, node) {
			errs = append(errs, labErrf(lab, field+".addresses", "node %q is not an endpoint of this link", node))
		}
	}
	for _, node := range nodes {
		value, ok := link.Addresses[node]
		if !ok {
			errs = append(errs, labErrf(lab, field+".addresses", "node %q has no address", node))
			continue
		}
		rendered, err := Render(value, params)
		if err != nil {
			errs = append(errs, labErrf(lab, field+".addresses."+node, "%v", err))
			continue
		}
		address, err := netip.ParsePrefix(rendered)
		if err != nil {
			errs = append(errs, labErrf(lab, field+".addresses."+node, "%v", err))
			continue
		}
		if !subnet.Contains(address.Addr()) {
			errs = append(errs, labErrf(lab, field+".addresses."+node, "%s is outside subnet %s", rendered, subnet))
		}
	}
	return errs
}

func checkSolution(lab *Lab) []error {
	var errs []error
	for _, field := range localizedFields("solution", lab.Solution) {
		if field.text == "" {
			errs = append(errs, labErrf(lab, field.name, "must not be empty"))
			continue
		}
		info, err := os.Stat(filepath.Join(lab.Dir, field.text))
		if err != nil {
			errs = append(errs, labErrf(lab, field.name, "%v", err))
			continue
		}
		if !info.Mode().IsRegular() {
			errs = append(errs, labErrf(lab, field.name, "%s is not a regular file", field.text))
		}
	}
	return errs
}
