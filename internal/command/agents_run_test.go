package command

import "testing"

// `agents run` is mounted under the agents noun and takes exactly one
// positional arg (the agent id).
func TestAgentsRunCmd_MountedWithOneArg(t *testing.T) {
	agents := findSubcommand(NewRoot(), "agents")
	if agents == nil {
		t.Fatal("agents command missing from root")
	}
	run := findSubcommand(agents, "run")
	if run == nil {
		t.Fatal("`agents run` subcommand missing")
	}
	if err := run.Args(run, []string{}); err == nil {
		t.Error("`agents run` should require an agent id")
	}
	if err := run.Args(run, []string{"agent-1"}); err != nil {
		t.Errorf("`agents run agent-1` should be accepted: %v", err)
	}
	if err := run.Args(run, []string{"a", "b"}); err == nil {
		t.Error("`agents run` should reject more than one arg (no way to fake a workflow_step_run_id)")
	}
}

// A direct agent run (the naked-run enqueue) must NOT be part of the
// tools-catalog vocabulary Albus composes from — Albus runs agents through
// workflows, not naked runs. Read-only agent-run introspection (agents.runs /
// agents.run-detail, plan #112) is fine and deliberately included, so this
// guard pins the exact bespoke `agents.run` verb rather than the whole
// agents.run* namespace.
func TestAgentsRun_NotInToolsCatalog(t *testing.T) {
	for _, tl := range BuildCatalog().Tools {
		if tl.Name == "agents.run" {
			t.Errorf("agents.run must stay out of the tools catalog, found %q", tl.Name)
		}
	}
}
