package command

import (
	"testing"

	"github.com/spf13/cobra"
)

// A parented noun whose Parent is itself a top-level noun (model-credentials
// under models, agent-credentials under agents) must mount on the REAL parent
// command, not a shadow duplicate. Regression for the bug where the credential
// list command printed the parent's help instead of running (plan #88 — the
// chat/guided credential step then showed no keys because the walkthrough's
// `yottactl models model-credentials list` returned help text, not JSON).
func TestBuildNounCommands_ChildrenMountOnRealParent(t *testing.T) {
	top := BuildNounCommands(&Env{})
	count := map[string]int{}
	parents := map[string]*cobra.Command{}
	for _, c := range top {
		count[c.Use]++
		parents[c.Use] = c
	}
	// No shadow duplicates for the real top-level nouns.
	for _, name := range []string{"models", "agents"} {
		if count[name] != 1 {
			t.Errorf("expected exactly one top-level %q command, got %d (shadow duplicate?)", name, count[name])
		}
	}
	// Each child is reachable under its real/synthetic parent.
	for _, tc := range []struct{ parent, child string }{
		{"models", "model-credentials"},
		{"agents", "agent-credentials"},
		{"context", "resources"}, // synthetic-parent path must still work
	} {
		p := parents[tc.parent]
		if p == nil {
			t.Fatalf("no top-level %q command", tc.parent)
		}
		found := false
		for _, sub := range p.Commands() {
			if sub.Use == tc.child {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q is not a subcommand of %q", tc.child, tc.parent)
		}
	}
}

// Two registry nouns share the bare name `workflows`: agent-platform
// (top-level, /v1/agent-platform/workflows) and drone (under `drone`,
// /v1/drone-platform/workflows). Keying the builder's command map by
// bare name let the later drone entry clobber the agent-platform one,
// so top-level `yottactl workflows` silently hit the empty drone
// endpoint — which broke EVERY Albus agent-platform workflow op
// (list/get/create/run/...), including the demo "open a PR" flow.
// Regression for the (parent,name) keying (nounKey).
func TestBuildNounCommands_WorkflowsNounDoesNotCollide(t *testing.T) {
	top := BuildNounCommands(&Env{})
	var workflows, drone *cobra.Command
	for _, c := range top {
		switch c.Use {
		case "workflows":
			workflows = c
		case "drone":
			drone = c
		}
	}
	if workflows == nil {
		t.Fatal("no top-level `workflows` command")
	}
	// The top-level noun must be the agent-platform one, not drone.
	if got := workflows.Short; got != "Agent workflows (DAGs of steps)" {
		t.Errorf("top-level `workflows` is not the agent-platform noun: Short=%q (drone shadowed it?)", got)
	}
	if got, want := workflows.GroupID, groupID("Agent Platform"); got != want {
		t.Errorf("top-level `workflows` GroupID = %q, want %q (drone noun carries no GroupID)", got, want)
	}
	// Drone's `workflows` must stay reachable under `drone`.
	if drone == nil {
		t.Fatal("no top-level `drone` command")
	}
	found := false
	for _, sub := range drone.Commands() {
		if sub.Use == "workflows" {
			found = true
			if sub.Short != "Drone workflows" {
				t.Errorf("`drone workflows` Short = %q, want %q", sub.Short, "Drone workflows")
			}
		}
	}
	if !found {
		t.Error("`workflows` is not a subcommand of `drone`")
	}
}
