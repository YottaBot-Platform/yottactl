package command

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func findSubcommand(parent *cobra.Command, name string) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

// `agents can` is mounted under the agents noun and takes exactly three
// positional args (agent-id, verb, resource-id).
func TestAgentsCanCmd_MountedWithThreeArgs(t *testing.T) {
	agents := findSubcommand(NewRoot(), "agents")
	if agents == nil {
		t.Fatal("agents command missing from root")
	}
	can := findSubcommand(agents, "can")
	if can == nil {
		t.Fatal("`agents can` subcommand missing")
	}
	if err := can.Args(can, []string{"only-one"}); err == nil {
		t.Error("`agents can` should reject fewer than 3 args")
	}
	if err := can.Args(can, []string{"agent-1", "read", "res-1"}); err != nil {
		t.Errorf("`agents can` with 3 args should be accepted: %v", err)
	}
}

// `can` is a bespoke human/debug verb — it must NOT leak into the
// tools-catalog the agents consume (the registry stays CRUD-only).
func TestAgentsCan_NotInToolsCatalog(t *testing.T) {
	for _, tl := range BuildCatalog().Tools {
		if strings.HasPrefix(tl.Name, "agents.can") {
			t.Errorf("agents.can must stay out of the tools catalog, found %q", tl.Name)
		}
	}
}
