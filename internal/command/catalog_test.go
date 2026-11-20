package command

import (
	"strings"
	"testing"

	"github.com/YottaBot-Platform/yottactl/internal/spec"
	"github.com/spf13/cobra"
)

// Every registry verb must produce exactly one catalog tool, and
// every tool name must be unique — the agent keys on tool name.
//
// Unshipped nouns are excluded: they are deliberately withheld from the
// model-facing catalog because no backing Service is deployed (plan #189), so
// they contribute zero tools. TestBuildCatalog_UnshippedNounsAreNotAdvertised
// below covers that path directly.
func TestBuildCatalog_OneToolPerVerb_UniqueNames(t *testing.T) {
	cat := BuildCatalog()

	wantTools := 0
	for _, n := range spec.Registry {
		if n.Unshipped {
			continue
		}
		wantTools += len(n.Verbs) + len(n.Actions)
	}
	if len(cat.Tools) != wantTools {
		t.Fatalf("catalog has %d tools, want %d (one per registry verb + action)", len(cat.Tools), wantTools)
	}

	seen := map[string]bool{}
	for _, tool := range cat.Tools {
		if seen[tool.Name] {
			t.Errorf("duplicate tool name %q", tool.Name)
		}
		seen[tool.Name] = true
		if tool.Method == "" {
			t.Errorf("tool %q has no HTTP method", tool.Name)
		}
		if tool.Path == "" {
			t.Errorf("tool %q has no path", tool.Name)
		}
	}
}

// get/update/delete tools must carry the {id} path param; list/create
// must not.
func TestBuildCatalog_PathParams(t *testing.T) {
	cat := BuildCatalog()
	for _, tool := range cat.Tools {
		hasParam := strings.Contains(tool.Path, "{")
		switch {
		case strings.HasSuffix(tool.Name, ".get"),
			strings.HasSuffix(tool.Name, ".update"),
			strings.HasSuffix(tool.Name, ".delete"):
			if !hasParam {
				t.Errorf("%q (%s %s) should have an id path param", tool.Name, tool.Method, tool.Path)
			}
		case strings.HasSuffix(tool.Name, ".list"),
			strings.HasSuffix(tool.Name, ".create"):
			if hasParam {
				t.Errorf("%q (%s %s) should NOT have an id path param", tool.Name, tool.Method, tool.Path)
			}
		}
	}
}

// The workflow run/observe actions must be emitted as catalog tools with the
// exact backend method+path (plan #73 Phase 3.5 — unblock workflow run/observe).
func TestBuildCatalog_WorkflowRunActions(t *testing.T) {
	cat := BuildCatalog()
	byName := toolsByName(cat)
	want := []struct {
		name, method, path, idParam string
		body                        bool
	}{
		{"workflows.run", "POST", "/v1/automation-platform/workflows/{id}/executions", "id", false},
		{"workflows.runs", "GET", "/v1/agent-platform/workflows/{id}/runs", "id", false},
		{"workflows.run-status", "GET", "/v1/agent-platform/workflows/runs/{run_id}", "run_id", false},
		{"workflows.run-steps", "GET", "/v1/agent-platform/workflows/runs/{run_id}/steps", "run_id", false},
		{"workflows.cancel-run", "POST", "/v1/agent-platform/workflows/runs/{run_id}/cancel", "run_id", false},
		{"workflows.step-decision", "POST", "/v1/agent-platform/workflows/step-runs/{step_run_id}/decision", "step_run_id", true},
		{"workflows.cancel-step-run", "POST", "/v1/agent-platform/workflows/step-runs/{step_run_id}/cancel", "step_run_id", false},
	}
	for _, w := range want {
		tool, ok := byName[w.name]
		if !ok {
			t.Errorf("missing catalog tool %q", w.name)
			continue
		}
		if tool.Method != w.method || tool.Path != w.path {
			t.Errorf("%s = %s %s, want %s %s", w.name, tool.Method, tool.Path, w.method, w.path)
		}
		if _, ok := tool.Args[w.idParam]; !ok {
			t.Errorf("%s missing id arg %q (args=%v)", w.name, w.idParam, tool.Args)
		}
		if _, hasBody := tool.Args["body"]; hasBody != w.body {
			t.Errorf("%s body arg = %v, want %v", w.name, hasBody, w.body)
		}
	}
}

// TestBuildCatalog_TwoArgActions — plan #83 Step 2b. Actions with a
// second positional arg (IDParam2) must expose BOTH path ids in the
// catalog Args so Albus knows to pass two arguments (detach role↔policy,
// group member add/remove).
func TestBuildCatalog_TwoArgActions(t *testing.T) {
	cat := BuildCatalog()
	byName := toolsByName(cat)
	want := []struct {
		name, method, path string
		args               []string
	}{
		{"roles.detach-policy", "DELETE", "/v1/identity/access/roles/{id}/policies/{policyId}", []string{"id", "policyId"}},
		{"roles.detach-group", "DELETE", "/v1/identity/access/roles/{id}/groups/{groupId}", []string{"id", "groupId"}},
		{"agents.agent-groups.add-member", "POST", "/v1/identity/agents/groups/{id}/members/{principalId}", []string{"id", "principalId"}},
		{"machines.machine-groups.remove-member", "DELETE", "/v1/identity/machines/groups/{id}/members/{principalId}", []string{"id", "principalId"}},
		{"secrets.move-stage", "POST", "/v1/secrets-manager/secrets/{id}/versions/{versionId}/stages", []string{"id", "versionId"}},
	}
	for _, w := range want {
		tool, ok := byName[w.name]
		if !ok {
			t.Errorf("missing catalog tool %q", w.name)
			continue
		}
		if tool.Method != w.method || tool.Path != w.path {
			t.Errorf("%s = %s %s, want %s %s", w.name, tool.Method, tool.Path, w.method, w.path)
		}
		for _, arg := range w.args {
			if _, ok := tool.Args[arg]; !ok {
				t.Errorf("%s missing arg %q (args=%v)", w.name, arg, tool.Args)
			}
		}
	}
}

func TestBuildCatalog_MetadataAndDescriptions(t *testing.T) {
	cat := BuildCatalog()
	if len(cat.Groups) < 8 {
		t.Fatalf("catalog Groups len = %d, want API area metadata", len(cat.Groups))
	}
	for _, g := range cat.Groups {
		if strings.TrimSpace(g.Name) == "" || strings.TrimSpace(g.Description) == "" {
			t.Errorf("group metadata must have name + description: %#v", g)
		}
	}
	for _, tool := range cat.Tools {
		if tool.Stub {
			continue
		}
		for label, value := range map[string]string{
			"group":            tool.Group,
			"summary":          tool.Summary,
			"description":      tool.Description,
			"long_description": tool.LongDescription,
		} {
			if strings.TrimSpace(value) == "" {
				t.Errorf("%s missing %s", tool.Name, label)
			}
		}
		for _, bad := range []string{"Get one of:", "Create a AI"} {
			if strings.Contains(tool.Summary, bad) || strings.Contains(tool.Description, bad) {
				t.Errorf("%s has mechanical wording %q (summary=%q description=%q)",
					tool.Name, bad, tool.Summary, tool.Description)
			}
		}
	}
}

func TestBuildCatalog_KnownPlan112Routes(t *testing.T) {
	byName := toolsByName(BuildCatalog())
	want := map[string]struct {
		method string
		path   string
	}{
		"models.model-credentials.get":    {"GET", "/v1/agent-platform/models/credentials/{id}"},
		"models.model-credentials.delete": {"DELETE", "/v1/agent-platform/models/credentials/{id}"},
		"workflows.cancel-step-run":       {"POST", "/v1/agent-platform/workflows/step-runs/{step_run_id}/cancel"},
		"secrets.rotate":                  {"POST", "/v1/secrets-manager/secrets/{id}/rotate"},
		"secrets.cancel-rotation":         {"POST", "/v1/secrets-manager/secrets/{id}/cancel-rotation"},
		"secrets.rotations":               {"GET", "/v1/secrets-manager/secrets/{id}/rotations"},
		"issues.backlog.list":             {"GET", "/v1/issues/backlog"},
		"issues.backlog.cancel":           {"POST", "/v1/issues/backlog/{id}/cancel"},
	}
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

// The root command must assemble with every GroupID defined on its
// parent — exactly the invariant cobra panics on at runtime if
// violated (the bug caught during 0.5f smoke: a sub-noun under a
// parent command carried a group its parent didn't define).
func TestNewRoot_GroupsValid(t *testing.T) {
	root := NewRoot()
	if root.Use != "yottactl" {
		t.Fatalf("root.Use = %q, want yottactl", root.Use)
	}
	var check func(c *cobra.Command)
	check = func(c *cobra.Command) {
		for _, child := range c.Commands() {
			if child.GroupID != "" && !c.ContainsGroup(child.GroupID) {
				t.Errorf("command %q has GroupID %q not defined on parent %q",
					child.Name(), child.GroupID, c.Name())
			}
			check(child)
		}
	}
	check(root)
}

func toolsByName(cat Catalog) map[string]Tool {
	byName := make(map[string]Tool, len(cat.Tools))
	for _, tool := range cat.Tools {
		byName[tool.Name] = tool
	}
	return byName
}

// TestBuildCatalog_UnshippedNounsAreNotAdvertised covers the plan-#189
// suppression path: a noun whose backing Service is deployed nowhere must not
// reach the model-facing catalog at all. Advertising one costs a turn and
// returns an unactionable error, which is the whole failure class #189 exists
// to close.
//
// The guard that keeps this honest is asserting the flag is actually IN USE.
// If every Unshipped marking were removed, the loop below would iterate zero
// times and pass vacuously while the suppression silently did nothing.
func TestBuildCatalog_UnshippedNounsAreNotAdvertised(t *testing.T) {
	cat := BuildCatalog()

	advertised := map[string]bool{}
	for _, tool := range cat.Tools {
		advertised[tool.Name] = true
	}

	var unshipped int
	for _, n := range spec.Registry {
		if !n.Unshipped {
			continue
		}
		unshipped++
		base := n.Name
		if n.Parent != "" {
			base = n.Parent + "." + n.Name
		}
		for _, v := range n.Verbs {
			if name := base + "." + string(v); advertised[name] {
				t.Errorf("noun %q is marked Unshipped but %q is still advertised to the model", n.Name, name)
			}
		}
		for _, a := range n.Actions {
			if name := base + "." + a.Verb; advertised[name] {
				t.Errorf("noun %q is marked Unshipped but %q is still advertised to the model", n.Name, name)
			}
		}
	}
	if unshipped == 0 {
		t.Skip("no Unshipped nouns in the registry — nothing to assert (every backing Service ships)")
	}
	t.Logf("%d unshipped nouns correctly withheld from the catalog", unshipped)
}

// ── Plan #194: renamed-noun supersession ───────────────────────────────

// A superseded noun stays a real tool in catalog.json (yottactl and the
// OpenAPI contract are built from it) but carries the successor's name so
// model-facing consumers can drop it.
func TestBuildCatalog_SupersededNounsCarryTheirSuccessor(t *testing.T) {
	cat := BuildCatalog()

	byName := map[string]Tool{}
	for _, tool := range cat.Tools {
		byName[tool.Name] = tool
	}

	for _, c := range []struct{ deprecated, successor string }{
		{"runtimes.list", "orchestrators.list"},
		{"runtimes.get", "orchestrators.get"},
		{"runtimes.create", "orchestrators.create"},
		{"catalog.runtimes", "catalog.orchestrators"},
	} {
		dep, ok := byName[c.deprecated]
		if !ok {
			t.Errorf("%q missing from the catalog — the deprecated alias must survive the window", c.deprecated)
			continue
		}
		if dep.SupersededBy != c.successor {
			t.Errorf("%q superseded_by = %q, want %q", c.deprecated, dep.SupersededBy, c.successor)
		}
		succ, ok := byName[c.successor]
		if !ok {
			t.Errorf("successor %q missing from the catalog", c.successor)
			continue
		}
		if succ.SupersededBy != "" {
			t.Errorf("%q is the successor and must not itself be superseded (got %q)", c.successor, succ.SupersededBy)
		}
		if dep.Method != succ.Method {
			t.Errorf("%q and %q disagree on method (%q vs %q) — the alias must be the same operation",
				c.deprecated, c.successor, dep.Method, succ.Method)
		}
	}
}

// The successor must be reachable at the canonical REST path — a rename
// that leaves the new noun pointing at the old base would pass the name
// checks above and still be wrong.
func TestBuildCatalog_OrchestratorToolsUseTheCanonicalPath(t *testing.T) {
	for _, tool := range BuildCatalog().Tools {
		if !strings.HasPrefix(tool.Name, "orchestrators.") {
			continue
		}
		if !strings.Contains(tool.Path, "/v1/agent-platform/orchestrators") {
			t.Errorf("%s path = %q, want the canonical /v1/agent-platform/orchestrators base", tool.Name, tool.Path)
		}
	}
}

// Plan #244 WP2b-3: workflows.create advertises the canonical draft-first POST
// — to Albus through the catalog, and in the OpenAPI generated from it — while
// every other workflows verb, and every other noun, keeps its base. The cobra
// side of the same override is TestWorkflowsCRUDRoutes.
func TestBuildCatalog_WorkflowsCreateIsCanonical(t *testing.T) {
	byName := toolsByName(BuildCatalog())
	for _, w := range []struct{ name, method, path, arg string }{
		{"workflows.create", "POST", "/v1/automation-platform/workflows", "body"},
		{"workflows.list", "GET", "/v1/agent-platform/workflows", ""},
		{"workflows.get", "GET", "/v1/agent-platform/workflows/{id}", "id"},
		{"workflows.update", "PATCH", "/v1/agent-platform/workflows/{id}", "body"},
		{"workflows.delete", "DELETE", "/v1/agent-platform/workflows/{id}", "id"},
		{"workflows.draft", "GET", "/v1/automation-platform/workflows/{id}/draft", "id"},
		// publish takes a body since 2026-09-26: {"reason", "if_match"}. A
		// catalog tool can pass a body but not flags, so without one Albus
		// published with no reason and could not pin the revision it reviewed.
		{"workflows.publish", "POST", "/v1/automation-platform/workflows/{id}/publish", "body"},
		{"workflows.deactivate", "POST", "/v1/automation-platform/workflows/{id}/deactivate", "id"},
		{"workflows.reactivate", "POST", "/v1/automation-platform/workflows/{id}/reactivate", "id"},
		{"workflows.unpublish", "POST", "/v1/automation-platform/workflows/{id}/unpublish", "id"},
		{"workflows.node-types", "GET", "/v1/automation-platform/node-types", ""},
		{"workflows.node-type", "GET", "/v1/automation-platform/node-types/{type}", "type"},
		{"drone.workflows.create", "POST", "/v1/drone-platform/workflows", "body"},
	} {
		tool, ok := byName[w.name]
		if !ok {
			t.Errorf("missing catalog tool %q", w.name)
			continue
		}
		if tool.Method != w.method || tool.Path != w.path {
			t.Errorf("%s = %s %s, want %s %s", w.name, tool.Method, tool.Path, w.method, w.path)
		}
		if _, ok := tool.Args[w.arg]; w.arg != "" && !ok {
			t.Errorf("%s args %v lack %q", w.name, tool.Args, w.arg)
		}
		if _, hasBody := tool.Args["body"]; hasBody && strings.HasPrefix(w.name, "workflows.") && w.arg != "body" {
			t.Errorf("%s takes no body; the lifecycle moves and the reads are header/URL-only", w.name)
		}
		if w.name == "workflows.publish" && !strings.Contains(tool.LongDescription, `"reason"`) {
			t.Errorf("workflows.publish must tell the model what its body carries: %q", tool.LongDescription)
		}
	}
	// The override is per verb, from one place: every CRUD tool's route is
	// VerbPath, so no other noun or verb moved with it.
	for _, n := range spec.Registry {
		if n.Unshipped {
			continue
		}
		base := n.Name
		if n.Parent != "" {
			base = n.Parent + "." + n.Name
		}
		for _, v := range n.Verbs {
			tool := byName[base+"."+string(v)]
			if got := strings.TrimSuffix(tool.Path, "/{"+idArg(n)+"}"); got != n.VerbPath(v) {
				t.Errorf("%s path %q, want VerbPath %q", tool.Name, tool.Path, n.VerbPath(v))
			}
			if len(n.VerbPaths) == 0 && !strings.HasPrefix(tool.Path, n.Path) {
				t.Errorf("%s moved off %q without a VerbPaths entry", tool.Name, n.Path)
			}
		}
	}
}
