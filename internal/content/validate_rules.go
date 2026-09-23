package content

import (
	"fmt"
	"maps"
	"slices"
)

var (
	paramFields = map[string][]string{
		GenConst:  {"value"},
		GenCIDR:   {"base", "prefix"},
		GenIPIn:   {"subnet", "index", "range"},
		GenChoice: {"of"},
		GenInt:    {"min", "max"},
	}
	requiredParamFields = map[string][]string{
		GenConst:  {"value"},
		GenCIDR:   {"base", "prefix"},
		GenIPIn:   {"subnet"},
		GenChoice: {"of"},
		GenInt:    {"min", "max"},
	}
)

func setParamFields(p Param) []string {
	var set []string
	for _, field := range []struct {
		name string
		ok   bool
	}{
		{"value", p.Value != ""},
		{"base", p.Base != ""},
		{"prefix", p.Prefix != 0},
		{"subnet", p.Subnet != ""},
		{"index", p.Index != nil},
		{"range", p.Range != nil},
		{"of", p.Of != nil},
		{"min", p.Min != nil},
		{"max", p.Max != nil},
	} {
		if field.ok {
			set = append(set, field.name)
		}
	}
	return set
}

func checkParamFields(lab *Lab, field string, p Param) []error {
	allowed, ok := paramFields[p.Gen]
	if !ok {
		return []error{labErrf(lab, field, "gen %q is not supported, allowed: %v", p.Gen, allowedGens)}
	}

	var errs []error
	set := setParamFields(p)
	for _, name := range requiredParamFields[p.Gen] {
		if !slices.Contains(set, name) {
			errs = append(errs, labErrf(lab, field, "gen %q requires %q", p.Gen, name))
		}
	}
	for _, name := range set {
		if !slices.Contains(allowed, name) {
			errs = append(errs, labErrf(lab, field, "%q is not a field of gen %q", name, p.Gen))
		}
	}
	if p.Gen == GenIPIn {
		errs = append(errs, checkIPInSelector(lab, field, p)...)
	}
	return errs
}

func checkIPInSelector(lab *Lab, field string, p Param) []error {
	switch {
	case p.Index != nil && p.Range != nil:
		return []error{labErrf(lab, field, "index and range are mutually exclusive")}
	case p.Index == nil && p.Range == nil:
		return []error{labErrf(lab, field, "gen %q requires index or range", GenIPIn)}
	case p.Range != nil && len(p.Range) != 2:
		return []error{labErrf(lab, field, "range must hold exactly two values, got %d", len(p.Range))}
	default:
		return nil
	}
}

func checkCases(lab *Lab) []error {
	errs := slices.Clone(lab.caseErrs)
	seen := map[string]bool{}
	for i, entry := range lab.Cases {
		field := fmt.Sprintf("cases[%d]", i)
		if entry.File == "" {
			errs = append(errs, labErrf(lab, field+".file", "must not be empty"))
			continue
		}
		if entry.Weight <= 0 {
			errs = append(errs, labErrf(lab, field+".weight", "must be a positive integer, got %d", entry.Weight))
		}
		if seen[entry.ID] {
			errs = append(errs, labErrf(lab, field+".file", "duplicate case id %q", entry.ID))
		}
		seen[entry.ID] = true

		own := map[string]bool{}
		for _, param := range entry.Params {
			own[param.Name] = true
			errs = append(errs, checkParamFields(lab, field+".params."+param.Name, param)...)
		}
		mergeParams(lab.Params, entry.Params).resolve(validationSeed, func(name string, err error) {
			if own[name] {
				errs = append(errs, labErrf(lab, field+".params."+name, "%v", err))
			}
		})
	}
	return errs
}

func checkPrecheck(lab *Lab) []error {
	if lab.Precheck == nil {
		return nil
	}
	errs := checkExecutable(lab, "precheck.script", lab.Precheck.Script)
	if lab.Precheck.Retries < minPrecheckRetries || lab.Precheck.Retries > maxPrecheckRetries {
		errs = append(errs, labErrf(lab, "precheck.retries", "must be between %d and %d, got %d", minPrecheckRetries, maxPrecheckRetries, lab.Precheck.Retries))
	}
	return errs
}

func checkRequires(lab *Lab) []error {
	known := make(map[string]Checkpoint, len(lab.Checkpoints))
	for _, cp := range lab.Checkpoints {
		known[cp.Id] = cp
	}

	var errs []error
	for i, cp := range lab.Checkpoints {
		field := fmt.Sprintf("checkpoints[%d].requires", i)
		for _, id := range cp.Requires {
			if _, ok := known[id]; !ok {
				errs = append(errs, labErrf(lab, field, "checkpoint %q does not exist", id))
			}
		}
	}
	if len(errs) > 0 {
		return errs
	}

	state := map[string]int{}
	for _, cp := range lab.Checkpoints {
		if cycle := findRequiresCycle(known, state, cp.Id); cycle != "" {
			errs = append(errs, labErrf(lab, "checkpoints.requires", "cycle through %s", cycle))
			break
		}
	}
	return errs
}

func findRequiresCycle(known map[string]Checkpoint, state map[string]int, id string) string {
	switch state[id] {
	case 1:
		return id
	case 2:
		return ""
	}
	state[id] = 1
	for _, next := range known[id].Requires {
		if cycle := findRequiresCycle(known, state, next); cycle != "" {
			return id + " -> " + cycle
		}
	}
	state[id] = 2
	return ""
}

func checkTutorial(lab *Lab) []error {
	wanted := slices.Contains(lab.Modes, tutorialMode)
	if !wanted {
		if len(lab.Tutorial) > 0 {
			return []error{labErrf(lab, "tutorial", "is only allowed when modes contains %q", tutorialMode)}
		}
		return nil
	}

	var errs []error
	var visible []string
	for i, cp := range lab.Checkpoints {
		if !cp.Visible {
			errs = append(errs, labErrf(lab, fmt.Sprintf("checkpoints[%d].visible", i), "must be true when modes contains %q", tutorialMode))
			continue
		}
		visible = append(visible, cp.Id)
	}
	if len(lab.Tutorial) == 0 {
		return append(errs, labErrf(lab, "tutorial", "must not be empty when modes contains %q", tutorialMode))
	}

	sets := paramSets(lab)
	position := map[string]int{}
	for i, step := range lab.Tutorial {
		field := fmt.Sprintf("tutorial[%d]", i)
		if _, seen := position[step.Checkpoint]; seen {
			errs = append(errs, labErrf(lab, field+".checkpoint", "duplicate step for checkpoint %q", step.Checkpoint))
			continue
		}
		position[step.Checkpoint] = i
		if !slices.Contains(visible, step.Checkpoint) {
			errs = append(errs, labErrf(lab, field+".checkpoint", "%q is not a visible checkpoint", step.Checkpoint))
		}
		for _, instruction := range localizedFields(field+".instruction", step.Instruction) {
			if instruction.text == "" {
				errs = append(errs, labErrf(lab, instruction.name, "must not be empty"))
				continue
			}
			for _, set := range sets {
				if _, err := Render(instruction.text, set.params); err != nil {
					errs = append(errs, set.errf(lab, instruction.name, "%v", err))
				}
			}
		}
	}
	for _, id := range visible {
		if _, ok := position[id]; !ok {
			errs = append(errs, labErrf(lab, "tutorial", "checkpoint %q has no step", id))
		}
	}

	for _, cp := range lab.Checkpoints {
		step, ok := position[cp.Id]
		if !ok {
			continue
		}
		for _, id := range cp.Requires {
			if required, ok := position[id]; ok && required > step {
				errs = append(errs, labErrf(lab, fmt.Sprintf("tutorial[%d].checkpoint", step), "%q comes before %q, which it requires", cp.Id, id))
			}
		}
	}
	return errs
}

func checkK3s(lab *Lab) []error {
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(lab.Topology.Nodes)) {
		node := lab.Topology.Nodes[name]
		if node.K3s == nil {
			continue
		}
		field := "nodes." + name + ".k3s"
		if node.Role != k3sServerRole {
			errs = append(errs, labErrf(lab, field, "is only allowed on role %q, got %q", k3sServerRole, node.Role))
			continue
		}
		for i, component := range node.K3s.Disable {
			if component == "" {
				errs = append(errs, labErrf(lab, fmt.Sprintf("%s.disable[%d]", field, i), "must not be empty"))
			}
		}
	}
	return errs
}
