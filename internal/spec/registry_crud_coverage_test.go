package spec

import "testing"

// chatCRUDContract pins the per-noun chat CRUD coverage for the Agent
// Platform Registry resource types (plan #73 item 12).
//
// The Albus catalog (yottactl tools-catalog) is generated from this
// Registry, and the operate-by-chat brain calls `<noun>.<verb>` catalog
// tools to do CRUD. So a noun's Verbs here ARE the set of CRUD operations
// the chat surface can perform on that resource type. Encoding the expected
// set makes coverage explicit and self-documenting:
//
//   - agents / workflows / tools / guardrail-policies — full CRUD.
//   - prompts — versioned: create + delete, no in-place update.
//   - models / orchestrators / mcp-gateways — managed catalogs: read + create,
//     no update/delete by design.
//
// The two assertions below guard both directions, which is the point of the
// item-12 guard — "prevents a resource silently lacking a working chat path
// for any verb":
//
//  1. every contract noun exists with EXACTLY these verbs (a verb silently
//     added or dropped fails here), and
//  2. every Agent Platform noun is covered by the contract (a new Registry
//     resource type can't ship without declaring its chat CRUD coverage).
var chatCRUDContract = map[string][]Verb{
	"deployments":        {Get, Delete},
	"agents":             {List, Get, Create, Update, Delete},
	"workflows":          {List, Get, Create, Update, Delete},
	"tools":              {List, Get, Create, Update, Delete},
	"guardrail-policies": {List, Get, Create, Update, Delete},
	"prompts":            {List, Get, Create, Update, Delete},
	"models":             {List, Get, Create, Update, Delete},
	"model-credentials":  {List, Get, Create, Delete},
	// Plan #194: `orchestrators` is the canonical noun; `runtimes` remains a
	// deprecated alias with the identical verb set for the compatibility
	// window, so both are declared and both must keep the same coverage.
	"orchestrators": {List, Get, Create},
	"runtimes":      {List, Get, Create},
	"mcp-gateways":  {List, Get, Create, Update, Delete},
	// LLM gateways (plan #109) — full CRUD, like mcp-gateways' operator surface
	// but writable.
	"llm-gateways": {List, Get, Create, Update, Delete},
	// Templates + configs (plan #112). Update is PUT (an action), so the CRUD
	// verb set stops at list/get/create/delete.
	"agent-templates":    {List, Get, Create, Delete},
	"agent-configs":      {List, Get, Create, Delete},
	"workflow-templates": {List, Get, Create, Delete},
	"workflow-configs":   {List, Get, Create, Delete},
	// Deployment targets (plan #111 §7) — list/create/delete + a set-default
	// action; no get-by-id or update on the backend.
	"deployment-targets": {List, Create, Delete},
}

func TestRegistry_ChatCRUDContract(t *testing.T) {
	byName := make(map[string]Noun, len(Registry))
	for _, n := range Registry {
		byName[n.Name] = n
	}

	// 1. Every contract noun exists with exactly the expected verbs.
	for name, want := range chatCRUDContract {
		n, ok := byName[name]
		if !ok {
			t.Errorf("contract noun %q is missing from Registry", name)
			continue
		}
		if !sameVerbSet(n.Verbs, want) {
			t.Errorf("noun %q verbs = %v, want %v (update chatCRUDContract or restore the verb)", name, n.Verbs, want)
		}
	}

	// 2. Every Agent Platform CRUD noun is accounted for in the contract.
	//    Actions-only nouns (no Verbs — e.g. `github`, a read-only repo file
	//    fetch) aren't CRUD resources, so they're out of the CRUD contract.
	for _, n := range Registry {
		if n.Group != "Agent Platform" || len(n.Verbs) == 0 {
			continue
		}
		if _, ok := chatCRUDContract[n.Name]; !ok {
			t.Errorf("Agent Platform noun %q has no chatCRUDContract entry — declare its chat CRUD coverage (plan #73 item 12)", n.Name)
		}
	}
}

// sameVerbSet compares two verb lists as sets (order-insensitive; nouns never
// repeat a verb).
func sameVerbSet(got, want []Verb) bool {
	if len(got) != len(want) {
		return false
	}
	gm := make(map[Verb]bool, len(got))
	for _, v := range got {
		gm[v] = true
	}
	for _, v := range want {
		if !gm[v] {
			return false
		}
	}
	return true
}
