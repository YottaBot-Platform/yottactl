package command

import "testing"

// `deployment-targets list` is mounted at the root (now registry-driven since
// plan #112 promoted the full surface).
func TestDeploymentTargetsCmd_MountedWithList(t *testing.T) {
	dt := findSubcommand(NewRoot(), "deployment-targets")
	if dt == nil {
		t.Fatal("deployment-targets command missing from root")
	}
	if findSubcommand(dt, "list") == nil {
		t.Error("`deployment-targets list` subcommand missing")
	}
}

// Plan #112 promoted deployment-targets from a bespoke CLI-only read into the
// registry, so its list/create/delete + set-default surface IS now part of the
// tools catalog / OpenAPI (plan #111 §7 is the live runtime-targets surface).
func TestDeploymentTargets_InToolsCatalog(t *testing.T) {
	want := map[string]struct{ method, path string }{
		"deployment-targets.list":        {"GET", "/v1/agent-platform/deployment-targets"},
		"deployment-targets.create":      {"POST", "/v1/agent-platform/deployment-targets"},
		"deployment-targets.delete":      {"DELETE", "/v1/agent-platform/deployment-targets/{id}"},
		"deployment-targets.set-default": {"POST", "/v1/agent-platform/deployment-targets/{id}/default"},
	}
	byName := toolsByName(BuildCatalog())
	for name, w := range want {
		tool, ok := byName[name]
		if !ok {
			t.Errorf("missing catalog tool %q", name)
			continue
		}
		if tool.Method != w.method || tool.Path != w.path {
			t.Errorf("%s = %s %s, want %s %s", name, tool.Method, tool.Path, w.method, w.path)
		}
	}
}
