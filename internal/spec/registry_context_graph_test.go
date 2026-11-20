package spec

import (
	"regexp"
	"strings"
	"testing"
)

// Plan #120 step 2 — the Context `resources` noun must expose the one-hop
// graph reads (/children, /relationships, /operations) as catalog actions so
// Albus, which reaches Context through yottactl under the user's JWT, has the
// same safe reads agents already reach through yotta-mcp. These are the
// parity gap this slice closes; guard the contract so a future edit can't
// silently drop them or turn one into a write.
func TestRegistry_ContextResourceGraphReadActions(t *testing.T) {
	var resources *Noun
	for i := range Registry {
		if Registry[i].Name == "resources" && Registry[i].Parent == "context" {
			resources = &Registry[i]
			break
		}
	}
	if resources == nil {
		t.Fatal("context `resources` noun missing from Registry")
	}

	byVerb := make(map[string]Action, len(resources.Actions))
	for _, a := range resources.Actions {
		byVerb[a.Verb] = a
	}

	want := map[string]string{
		"children":      "/v1/context/resources/{id}/children",
		"relationships": "/v1/context/resources/{id}/relationships",
		"operations":    "/v1/context/resources/{id}/operations",
	}
	for verb, path := range want {
		a, ok := byVerb[verb]
		if !ok {
			t.Errorf("context resources action %q missing (Albus loses the one-hop graph read)", verb)
			continue
		}
		if a.Method != "GET" {
			t.Errorf("action %q method = %q, want GET (these are read-only)", verb, a.Method)
		}
		if a.Path != path {
			t.Errorf("action %q path = %q, want %q", verb, a.Path, path)
		}
		if a.IDParam != "id" {
			t.Errorf("action %q IDParam = %q, want id", verb, a.IDParam)
		}
		if a.Body {
			t.Errorf("action %q takes a body; a one-hop read must not", verb)
		}
		// MCPExclude keeps these off yotta-mcp's generated surface (agents
		// already have the curated Context tools) while still emitting the
		// yottactl command + catalog/OpenAPI entry for the Albus path.
		if !a.MCPExclude {
			t.Errorf("action %q must set MCPExclude (agents use the curated yotta-mcp Context tools; the generated dup would bloat every tool list)", verb)
		}
	}
}

// Plan #120 step 5 — the `context graph` noun exposes the bounded traversal
// primitives so Albus (via yottactl under the user's JWT) reaches the same
// blast-radius / trace-path / neighbors the SRE agent reaches via yotta-mcp.
// All POST + read-only (a walk never mutates); a body is required.
func TestRegistry_ContextGraphActions(t *testing.T) {
	var graph *Noun
	for i := range Registry {
		if Registry[i].Name == "graph" && Registry[i].Parent == "context" {
			graph = &Registry[i]
			break
		}
	}
	if graph == nil {
		t.Fatal("context `graph` noun missing from Registry (Albus loses graph access)")
	}
	if len(graph.Verbs) != 0 {
		t.Errorf("graph noun should be actions-only, has verbs %v", graph.Verbs)
	}
	want := map[string]string{
		"blast-radius": "/v1/context/graph/blast-radius",
		"trace-path":   "/v1/context/graph/trace-path",
		"neighbors":    "/v1/context/graph/neighbors",
		"pack":         "/v1/context/graph/pack",
	}
	byVerb := map[string]Action{}
	for _, a := range graph.Actions {
		byVerb[a.Verb] = a
	}
	for verb, path := range want {
		a, ok := byVerb[verb]
		if !ok {
			t.Errorf("context graph action %q missing", verb)
			continue
		}
		if a.Method != "POST" {
			t.Errorf("action %q method = %q, want POST", verb, a.Method)
		}
		if a.Path != path {
			t.Errorf("action %q path = %q, want %q", verb, a.Path, path)
		}
		if !a.Body {
			t.Errorf("action %q must take a body (structured traversal request)", verb)
		}
	}
}

// `context graphs list` must return the INSTANCES, not the three lenses.
//
// It briefly did not: /v1/context/graphs served DEFINITIONS for a bare request
// during the #208 Q9 transition, and the generic List verb sends a bare GET.
// The fix at the time was a ListQuery escape hatch on the noun; the cutover on
// 2026-08-25 deleted the transitional branch, so the bare path means instances
// and the hatch is gone with it.
//
// What must not regress is the noun's SHAPE — plain collection path, no query
// smuggled into `list`. If a second meaning ever reappears on this path, fix
// the server, not the caller.
func TestContextGraphs_ListUsesThePlainCollectionPath(t *testing.T) {
	var graphs *Noun
	for i := range Registry {
		if Registry[i].Name == "graphs" && Registry[i].Parent == "context" {
			graphs = &Registry[i]
		}
	}
	if graphs == nil {
		t.Fatal("context graphs noun is missing")
	}
	if graphs.Path != "/v1/context/graphs" {
		t.Errorf("path = %q, want the plain collection path", graphs.Path)
	}
	if strings.Contains(graphs.Path, "?") {
		t.Error("a query in the noun path means the bare path stopped meaning 'the rows' — " +
			"that belongs in the server, not here")
	}
}

// Every {param} in an Action's Path must be declared as IDParam/IDParam2.
//
// buildAction substitutes on the DECLARED param — it does not parse the path —
// so an action carrying "{id}" without IDParam accepts no arguments and calls a
// URL with a literal "{id}" in it. Three shipped that way in #208 step 7
// (context graphs result/proof/refresh), which meant the disclosure path that
// plan built could be listed and never opened from the CLI. Found by trying to
// refresh a graph and getting `unknown command "<uuid>"`.
func TestActions_DeclareEveryPathParameter(t *testing.T) {
	param := regexp.MustCompile(`\{([a-zA-Z_-]+)\}`)
	for _, n := range Registry {
		for _, a := range n.Actions {
			declared := map[string]bool{}
			if a.IDParam3 != "" {
				declared[a.IDParam3] = true
			}
			if a.IDParam != "" {
				declared[a.IDParam] = true
			}
			if a.IDParam2 != "" {
				declared[a.IDParam2] = true
			}
			for _, m := range param.FindAllStringSubmatch(a.Path, -1) {
				if !declared[m[1]] {
					t.Errorf("%s %s: path carries {%s} but no IDParam declares it — "+
						"the verb will take no argument and call a literal {%s}",
						n.Name, a.Verb, m[1], m[1])
				}
			}
		}
	}
}
