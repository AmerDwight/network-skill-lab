package lint

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/AmerDwight/network-skill-lab/internal/content"
)

const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

type Finding struct {
	Path     string `json:"path"`
	Rule     string `json:"rule"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
}

func (f Finding) Line() string {
	line := fmt.Sprintf("%s: %s: %s", f.Path, f.Rule, f.Message)
	if f.Severity == SeverityWarning {
		return "warning: " + line
	}
	return line
}

func ExitCode(findings []Finding) int {
	for _, f := range findings {
		if f.Severity == SeverityError {
			return 1
		}
	}
	return 0
}

func Run(dir string) ([]Finding, error) {
	var findings []Finding
	if _, err := content.LoadAll(dir); err != nil {
		findings = append(findings, contentFindings(dir, err)...)
	}

	scripts, err := shellScripts(dir)
	if err != nil {
		return nil, err
	}
	for _, script := range scripts {
		more, err := checkScript(script)
		if err != nil {
			return nil, err
		}
		findings = append(findings, more...)
	}

	slices.SortStableFunc(findings, func(a, b Finding) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Rule, b.Rule), cmp.Compare(a.Message, b.Message))
	})
	return findings, nil
}

func contentFindings(dir string, err error) []Finding {
	var findings []Finding
	for _, line := range strings.Split(err.Error(), "\n") {
		if line == "" {
			continue
		}
		path, rest, ok := strings.Cut(line, ": ")
		if !ok {
			findings = append(findings, Finding{Path: dir, Rule: "content", Message: line, Severity: SeverityError})
			continue
		}
		rule, message, ok := strings.Cut(rest, ": ")
		if !ok {
			findings = append(findings, Finding{Path: path, Rule: "content", Message: rest, Severity: SeverityError})
			continue
		}
		findings = append(findings, Finding{Path: path, Rule: rule, Message: message, Severity: SeverityError})
	}
	return findings
}

func shellScripts(dir string) ([]string, error) {
	var scripts []string
	err := filepath.WalkDir(dir, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sh") {
			scripts = append(scripts, p)
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", dir, err)
	}
	return scripts, nil
}

type sideEffect struct {
	name    string
	pattern *regexp.Regexp
}

var (
	sideEffects = []sideEffect{
		{"ip link set", regexp.MustCompile(`\bip\s+link\s+set\b`)},
		{"systemctl", regexp.MustCompile(`\bsystemctl\b`)},
		{"rm ", regexp.MustCompile(`\brm\s`)},
		{"> redirect", regexp.MustCompile(`(^|[^-=<>!])>`)},
	}
	discardedOutput = regexp.MustCompile(`>>?\s*/dev/null|\d?>&\d`)
	literalIP       = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}(\.\d{1,3})?\b`)
)

func checkScript(path string) ([]Finding, error) {
	if finding, ok, err := bashSyntax(path); err != nil {
		return nil, err
	} else if !ok {
		return []Finding{finding}, nil
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	isCheck := strings.Contains(filepath.ToSlash(path), "/checks/")
	var findings []Finding
	for i, line := range strings.Split(string(b), "\n") {
		code, _, _ := strings.Cut(line, "#")
		if isCheck {
			written := discardedOutput.ReplaceAllString(code, "")
			for _, effect := range sideEffects {
				if effect.pattern.MatchString(written) {
					findings = append(findings, Finding{
						Path:     path,
						Rule:     "checks-side-effect",
						Message:  fmt.Sprintf("line %d uses %q, a check script must not change the node", i+1, effect.name),
						Severity: SeverityWarning,
					})
				}
			}
		}
		if match := literalIP.FindString(code); match != "" {
			findings = append(findings, Finding{
				Path:     path,
				Rule:     "hardcoded-ip",
				Message:  fmt.Sprintf("line %d hardcodes %s, use a param instead", i+1, match),
				Severity: SeverityWarning,
			})
		}
	}
	return findings, nil
}

func bashSyntax(path string) (Finding, bool, error) {
	cmd := exec.Command("bash", "-n", path)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return Finding{}, true, nil
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return Finding{}, false, fmt.Errorf("run bash -n %s: %w", path, err)
	}
	message := strings.TrimSpace(string(out))
	if message == "" {
		message = err.Error()
	}
	return Finding{
		Path:     path,
		Rule:     "bash-syntax",
		Message:  strings.ReplaceAll(message, "\n", "; "),
		Severity: SeverityError,
	}, false, nil
}
