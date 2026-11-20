package command

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/YottaBot-Platform/yottactl/internal/spec"
)

func rootHelp(t *testing.T, args ...string) string {
	t.Helper()
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append(args, "--help"))
	if err := root.Execute(); err != nil {
		t.Fatalf("yottactl %s --help: %v", strings.Join(args, " "), err)
	}
	return out.String()
}

// yottactl's help is what a customer reads first (plan 333 step 5, 1.0.1):
// it names nothing they don't have (the retired `yotta` terminal, yotta-mcp),
// no milestone of ours, and no command with nothing behind it.
func TestRootHelpSaysNothingInternal(t *testing.T) {
	help := rootHelp(t)
	for _, bad := range []string{"`yotta` terminal", "yotta-mcp", "post-MVP", "stub", "MVP-01"} {
		if strings.Contains(help, bad) {
			t.Errorf("yottactl --help says %q:\n%s", bad, help)
		}
	}
	if !strings.Contains(help, "yottactl login") {
		t.Errorf("yottactl --help doesn't say where to start (yottactl login):\n%s", help)
	}
}

// A noun with no backend (Unshipped) or one that only answers [] (Stub) is
// left out of the help but still runs, so a script that calls it gets the
// server's own error, as before.
func TestHelpLeavesOutNounsWithNothingBehindThem(t *testing.T) {
	help := rootHelp(t)
	hidden := 0
	for _, n := range spec.Registry {
		if n.Parent != "" || !(n.Stub || n.Unshipped) {
			continue
		}
		hidden++
		if regexp.MustCompile(`(?m)^\s+` + regexp.QuoteMeta(n.Name) + `\s`).MatchString(help) {
			t.Errorf("yottactl --help lists %q, which has nothing behind it", n.Name)
		}
		if sub := rootHelp(t, n.Name); !strings.Contains(sub, "yottactl "+n.Name) {
			t.Errorf("yottactl %s --help no longer runs:\n%s", n.Name, sub)
		}
	}
	if hidden == 0 {
		t.Skip("no Stub or Unshipped nouns in the registry")
	}
}

// Every group heading in the help has at least one command under it.
func TestHelpHasNoEmptyGroup(t *testing.T) {
	lines := strings.Split(rootHelp(t), "\n")
	for i, line := range lines {
		if !strings.HasSuffix(line, ":") || strings.HasPrefix(line, " ") || line == "Usage:" || line == "Flags:" || line == "Global Flags:" {
			continue
		}
		if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "  ") {
			t.Errorf("group %q has no commands under it", strings.TrimSuffix(line, ":"))
		}
	}
}
