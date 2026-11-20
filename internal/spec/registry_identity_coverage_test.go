package spec

import "testing"

// Plan #83 Step 8 — Identity skill-parity guard.
//
// Every Identity nav leaf must have a corresponding yottactl/Albus skill
// (registry noun), and no Identity noun may silently gain or lose a verb.
// This is the Identity analogue of chatCRUDContract: it pins the exact
// verb set per Identity noun and enforces both directions —
//
//  1. every contract noun exists with EXACTLY these verbs, and
//  2. every Group=="Identity" noun with verbs is covered by the contract
//     (a new Identity leaf can't ship without declaring its skill here).
//
// Action-only extras (approvals' approve/reject/cancel, human-sessions'
// revoke, machine-credentials' revoke-all) are covered by the nouns
// existing with their List verb; the actions ride along on the same noun.
var identityNounContract = map[string][]Verb{
	"scim-tokens":         {List, Create, Delete},
	"auth-policies":       {List, Get, Create, Update, Delete},
	"federated-issuers":   {List, Get},
	"federated-policies":  {List, Get},
	"federated-exchanges": {List},
	// Full CRUD resources.
	"users":          {List, Get, Create, Update, Delete},
	"groups":         {List, Get, Create, Update, Delete},
	"roles":          {List, Get, Create, Update, Delete},
	"policies":       {List, Get, Create, Update, Delete},
	"sso-providers":  {List, Get, Create, Update, Delete},
	"agent-groups":   {List, Get, Create, Update, Delete},
	"machine-groups": {List, Get, Create, Update, Delete},
	// Read/create/delete (no in-place update by design).
	"api-tokens":       {List, Get, Create, Delete},
	"service-accounts": {List, Get, Create, Update, Delete},
	"resource-grants":  {List, Get, Create, Delete},
	// Create/delete, no get-by-id on the backend.
	"machine-credentials": {List, Create, Delete},
	"assignments":         {List, Create, Delete},
	// Read-only projections / append-only logs (+ ride-along actions).
	"human-sessions":    {List},
	"machine-sessions":  {List},
	"agent-sessions":    {List},
	"agent-identities":  {List},
	"agent-credentials": {List},
	"approvals":         {List},
	"audit":             {List},
	"events":            {List},
	// Permission catalog (plan #112) — read-only list of grantable permissions.
	"permissions": {List},
}

func TestRegistry_IdentityNounContract(t *testing.T) {
	byName := make(map[string]Noun, len(Registry))
	for _, n := range Registry {
		byName[n.Name] = n
	}

	// 1. Every contract noun exists with exactly the expected verbs.
	for name, want := range identityNounContract {
		n, ok := byName[name]
		if !ok {
			t.Errorf("Identity contract noun %q is missing from Registry", name)
			continue
		}
		if !sameVerbSet(n.Verbs, want) {
			t.Errorf("Identity noun %q verbs = %v, want %v (update identityNounContract or restore the verb)", name, n.Verbs, want)
		}
	}

	// 2. Every Identity noun with CRUD verbs is declared in the contract —
	//    a new Identity leaf can't ship without pinning its skill here.
	for _, n := range Registry {
		if n.Group != "Identity" || len(n.Verbs) == 0 {
			continue
		}
		if _, ok := identityNounContract[n.Name]; !ok {
			t.Errorf("Identity noun %q is not in identityNounContract — every Identity leaf's skill must be pinned (plan #83 Step 8)", n.Name)
		}
	}
}
