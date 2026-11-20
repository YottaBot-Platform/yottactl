package command

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/YottaBot-Platform/yottactl/internal/client"
	"github.com/YottaBot-Platform/yottactl/internal/spec"
	"github.com/spf13/cobra"
)

// Tool is one entry in the tools-catalog JSON. Plan #60 Track B B2.
// The background agent (and Albus, and future SDKs) load this catalog
// at startup; each tool maps to a `yottactl <noun> <verb>` invocation.
type Tool struct {
	BodyContentType string            `json:"body_content_type,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	Name            string            `json:"name"` // e.g. "agents.list"
	Group           string            `json:"group,omitempty"`
	Summary         string            `json:"summary,omitempty"`
	Description     string            `json:"description"`
	LongDescription string            `json:"long_description,omitempty"`
	Path            string            `json:"path"`   // REST path the verb hits
	Method          string            `json:"method"` // GET/POST/PUT/PATCH/DELETE
	Args            map[string]string `json:"args,omitempty"`
	Stub            bool              `json:"stub,omitempty"`
	// MCPExclude carries spec.Action.MCPExclude through to catalog.json —
	// see that field's doc. Only ever true on GET actions; yotta-mcp's
	// generated.go filters these out of its agent-callable read surface
	// while every other consumer (yottactl, human/operator tooling) keeps
	// treating the tool normally.
	MCPExclude bool `json:"mcp_exclude,omitempty"`
	// SupersededBy carries spec.Noun/Action.SupersededBy through to
	// catalog.json: the name of the tool that replaced this one during a
	// rename's compatibility window (plan #194). Consumers building a
	// MODEL-facing tool list must drop these — Albus's ScopeCatalog and
	// yotta-mcp's generated read surface both do — while yottactl, the
	// OpenAPI contract, and operator tooling keep them.
	SupersededBy string `json:"superseded_by,omitempty"`
}

// UnshippedNoun records a registry noun deliberately withheld from `tools`
// because no backing Service is deployed (plan #189). It is NOT a tool and no
// agent ever sees it — it exists so tooling can reason about the suppression
// without re-deriving it from Go source. In particular the #189 drift check
// needs it: the whole point of Unshipped is that these nouns are absent from
// `tools`, so nothing downstream could otherwise tell a deliberate suppression
// from a noun that was never declared.
type UnshippedNoun struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// Catalog is the whole emitted document.
type Catalog struct {
	Version string          `json:"version"`
	Client  string          `json:"client_version"`
	Groups  []spec.GroupDoc `json:"groups,omitempty"`
	Tools   []Tool          `json:"tools"`
	// Unshipped is the suppression ledger — see UnshippedNoun. Consumers that
	// build an agent tool list MUST read `tools` only.
	Unshipped []UnshippedNoun `json:"unshipped,omitempty"`
}

// BuildCatalog walks the registry and produces the tool tree. This
// is the single source the agent consumes — adding a registry row
// adds a tool here automatically.
func BuildCatalog() Catalog {
	cat := Catalog{Version: "1.0", Client: client.Version, Groups: spec.GroupDocs}
	for _, n := range spec.Registry {
		base := n.Name
		if n.Parent != "" {
			base = n.Parent + "." + n.Name
		}
		// Plan #189: a noun whose backing Service is deployed nowhere is not
		// advertised to the model. It stays a yottactl command and a registry
		// row — this only removes it from the tool surface the agent picks
		// from, so the model is never handed an operation that cannot resolve.
		if n.Unshipped {
			cat.Unshipped = append(cat.Unshipped, UnshippedNoun{Name: base, Path: n.Path})
			continue
		}
		for _, v := range n.Verbs {
			summary := summaryForVerb(v, n)
			path := n.VerbPath(v)
			t := Tool{
				Name:            base + "." + string(v),
				Group:           n.Group,
				Summary:         summary,
				Description:     summary,
				LongDescription: descriptionForVerb(v, n),
				Path:            path,
				Stub:            n.Stub,
				SupersededBy:    supersededToolName(n.SupersededBy, string(v)),
			}
			switch v {
			case spec.List:
				t.Method = "GET"
			case spec.Get:
				t.Method = "GET"
				t.Path = path + "/{" + idArg(n) + "}"
				t.Args = map[string]string{idArg(n): "string"}
			case spec.Create:
				t.Method = "POST"
				t.Args = map[string]string{"body": "object"}
			case spec.Update:
				t.Method = "PATCH"
				t.Path = path + "/{" + idArg(n) + "}"
				t.Args = map[string]string{idArg(n): "string", "body": "object"}
			case spec.Delete:
				t.Method = "DELETE"
				t.Path = path + "/{" + idArg(n) + "}"
				t.Args = map[string]string{idArg(n): "string"}
			}
			cat.Tools = append(cat.Tools, t)
		}
		// Custom (non-CRUD) actions — run/observe/decision flows.
		for _, a := range n.Actions {
			description := a.Description
			if description == "" {
				description = a.Short
			}
			t := Tool{
				Name:            base + "." + a.Verb,
				Group:           n.Group,
				Summary:         a.Short,
				Description:     a.Short,
				LongDescription: description,
				Path:            a.Path,
				Headers:         a.Headers,
				BodyContentType: a.BodyContentType,
				Method:          a.Method,
				Stub:            n.Stub,
				MCPExclude:      a.MCPExclude,
				SupersededBy:    supersededActionTool(base, a, n),
			}
			args := map[string]string{}
			for arg := range a.Headers {
				args[arg] = "string"
			}
			if a.IDParam != "" {
				args[a.IDParam] = "string"
			}
			if a.IDParam2 != "" {
				args[a.IDParam2] = "string"
			}
			if a.IDParam3 != "" {
				args[a.IDParam3] = "string"
			}
			if a.Body {
				args["body"] = "object"
				if a.BodyContentType != "" {
					args["body"] = "string"
				}
			}
			if len(args) > 0 {
				t.Args = args
			}
			cat.Tools = append(cat.Tools, t)
		}
	}
	return cat
}

// supersededToolName turns a successor NOUN/ACTION name into the successor
// TOOL name for this verb ("orchestrators" + "list" → "orchestrators.list"),
// so a consumer reading catalog.json can point a caller at the exact
// replacement rather than at a bare noun. Empty in, empty out.
func supersededToolName(successor, verb string) string {
	if successor == "" {
		return ""
	}
	return successor + "." + verb
}

// supersededActionTool resolves an action's successor tool name. The two
// supersession levels compose differently and must not be conflated:
//
//   - action-level (Action.SupersededBy names a successor VERB on the same
//     noun) → base + "." + successor verb, e.g. catalog.runtimes →
//     catalog.orchestrators;
//   - noun-level (Noun.SupersededBy names a successor NOUN) → successor
//     noun + "." + this verb, e.g. runtimes.run → orchestrators.run.
//
// An action's own marking wins over the noun-wide one.
func supersededActionTool(base string, a spec.Action, n spec.Noun) string {
	if a.SupersededBy != "" {
		return base + "." + a.SupersededBy
	}
	return supersededToolName(n.SupersededBy, a.Verb)
}

func newCatalogCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "tools-catalog",
		Short:   "Print every command as a JSON tool catalog, for agents and MCP servers",
		GroupID: groupID("Core"),
		RunE: func(cmd *cobra.Command, args []string) error {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(BuildCatalog())
		},
	}
}

func summaryForVerb(v spec.Verb, n spec.Noun) string {
	switch v {
	case spec.List:
		return "List " + n.Short
	case spec.Get:
		return "Get " + n.Short
	case spec.Create:
		return "Create " + n.Short
	case spec.Update:
		return "Update " + n.Short
	case spec.Delete:
		return "Delete " + n.Short
	default:
		return fmt.Sprintf("%s %s", v, n.Short)
	}
}

func descriptionForVerb(v spec.Verb, n spec.Noun) string {
	if n.Description != "" {
		return n.Description
	}
	switch v {
	case spec.List:
		return "Returns the caller's tenant-scoped collection of " + n.Short + "."
	case spec.Get:
		return "Returns one tenant-scoped " + n.Short + " record by path identifier."
	case spec.Create:
		return "Creates a new tenant-scoped " + n.Short + " record from the request body."
	case spec.Update:
		return "Updates an existing tenant-scoped " + n.Short + " record by path identifier."
	case spec.Delete:
		return "Deletes or retires an existing tenant-scoped " + n.Short + " record by path identifier."
	default:
		return n.Short
	}
}
