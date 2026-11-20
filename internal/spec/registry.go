// Package spec is the declarative noun registry that drives the
// whole yottactl command tree AND the tools-catalog JSON. Plan #60
// Track B (B1 + B2).
//
// One table — Registry — enumerates every platform noun with its
// REST base path, the verbs it supports, and its grouping. The
// command factory (internal/command) builds a cobra command per
// noun from this; the catalog emitter walks the same table. Adding
// a noun = one row here, and it shows up in both the CLI and the
// agent's tool catalog. This is why there is no per-noun package —
// the registry is the single source.
package spec

// Verb is a CRUD operation a noun supports.
type Verb string

const (
	List   Verb = "list"
	Get    Verb = "get"
	Create Verb = "create"
	Update Verb = "update"
	Delete Verb = "delete"
)

// Noun is one registry row.
type Noun struct {
	Name        string // command name, e.g. "agents"
	Parent      string // optional grouping command, e.g. "context" → `yottactl context resources`
	Group       string // help-text section, e.g. "Agent Platform"
	Path        string // REST base path, e.g. "/v1/agent-platform/agents"
	IDParam     string // path key for get/update/delete ("id" or "name"); "" = collection-only
	Verbs       []Verb
	Description string // richer OpenAPI/agent docs; falls back to Short when empty
	// Actions are custom (non-CRUD) operations on the noun — run/observe/
	// decision flows that don't fit the list/get/create/update/delete shape
	// (e.g. POST /workflows/{id}/run). Each emits a `<noun>.<verb>` catalog
	// tool + a `yottactl <noun> <verb>` subcommand, just like a CRUD verb.
	Actions []Action
	Short   string // one-line description
	Stub    bool   // true for post-MVP-01 nouns (backend returns [] / not-implemented today)
	// Unshipped suppresses this noun from catalog.json — the model-facing tool
	// surface — while leaving it a normal `yottactl` command and a normal
	// registry row. Set it when the backing Service is not deployed in any
	// environment: the model cannot call the operation, so advertising it only
	// buys an error envelope and a wasted turn (plan #189). A human operator
	// running yottactl still gets a real, legible transport error; the model
	// simply is never offered the tool. Clear the flag when the Service ships
	// and the operations reappear automatically — nothing else to remember.
	//
	// Distinct from Stub: Stub means "backend exists but returns []/
	// not-implemented"; Unshipped means "there is no reachable backend at all".
	Unshipped bool
	// SupersededBy names the noun that replaced this one during a rename's
	// compatibility window (plan #194: `runtimes` → `orchestrators`). The
	// noun stays a fully working `yottactl` command and stays in the
	// generated OpenAPI — flagged `deprecated: true` — so humans and
	// existing scripts keep their path, and the spec tells them where to go.
	//
	// What it does NOT stay in is the model-facing surface: Albus's
	// ScopeCatalog and yotta-mcp's generated read surface both drop it.
	// Advertising two names for one operation doubles the tool list and
	// makes selection worse, which is the failure mode plan #194 §6 calls
	// out by name.
	//
	// Distinct from Unshipped: Unshipped means "no backend"; SupersededBy
	// means "working backend, better name".
	SupersededBy string
	// VerbPaths moves individual CRUD verbs off Path while the rest stay
	// (plan #244 WP2b-3: workflows.create reaches the canonical draft-first
	// POST before canonical list/get/update/delete exist). Every consumer
	// reads a verb's route through VerbPath — the cobra command, the tools
	// catalog and so Albus, and the generated OpenAPI — so they cannot
	// disagree about where one operation goes. Get/update/delete append
	// "/{id}" to it as they do to Path.
	VerbPaths map[Verb]string
}

// VerbPath is the REST base path verb v uses: its VerbPaths entry, or Path.
func (n Noun) VerbPath(v Verb) string {
	if p, ok := n.VerbPaths[v]; ok && p != "" {
		return p
	}
	return n.Path
}

// Action is a custom operation on a noun (beyond CRUD). It carries the full
// REST path template (with up to two {IDParam}/{IDParam2} placeholders the
// CLI substitutes from positional args), the HTTP method, and whether it
// takes a -f body.
type Action struct {
	BodyContentType string            // non-JSON body read verbatim from -f
	Headers         map[string]string // required catalog argument -> HTTP header name
	Verb            string            // sub-verb: the tool suffix + cobra subcommand ("run", "runs", "run-status", ...)
	Method          string            // GET / POST / PATCH / DELETE
	Path            string            // FULL REST path template; "{IDParam}"/"{IDParam2}" are replaced by the positional args
	IDParam         string            // first positional arg name (and placeholder in Path); "" = no positional
	IDParam3        string            // optional third positional path identifier
	IDParam2        string            // optional second positional arg (e.g. detach/member ops with two path ids)
	Body            bool              // takes a -f request body
	Short           string            // one-line description
	Description     string            // richer OpenAPI/agent docs; falls back to Short when empty
	// MCPExclude drops this GET action from yotta-mcp's registry-generated
	// read surface (internal/yotta_mcp/generated.go filters catalog.json
	// for Method=="GET" && !MCPExclude) even though it's still a normal
	// `yottactl <noun> <verb>` command and a normal tools-catalog.json
	// entry for human/operator use. Exists for reveal-shaped GET routes
	// (plan #96's secrets.value) where a human operator hitting it via the
	// CLI is fine but auto-exposing it as an agent-callable MCP tool would
	// let any agent with MCP access exfiltrate secret plaintext with no
	// additional gate — the opposite of yotta-mcp's "read-only platform-
	// introspection" posture. Non-GET actions never reach the catalog's
	// MCP filter in the first place, so this only matters for GET.
	MCPExclude bool
	// SupersededBy names the action that replaced this one — the
	// action-level twin of Noun.SupersededBy, with identical semantics
	// (kept in yottactl + OpenAPI, dropped from the model-facing surface).
	SupersededBy string
}

func crud() []Verb    { return []Verb{List, Get, Create, Update, Delete} }
func rc() []Verb      { return []Verb{List, Get, Create} }
func rcu() []Verb     { return []Verb{List, Get, Create, Update} }
func rcd() []Verb     { return []Verb{List, Get, Create, Delete} }
func listGet() []Verb { return []Verb{List, Get} }

// GroupDoc is product-area metadata emitted into the tools catalog so OpenAPI
// can render stable API sections (tags) without importing this CLI package.
type GroupDoc struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// GroupDocs describes every API area the registry can emit. Order matters:
// OpenAPI renderers use top-level tag order for navigation.
var GroupDocs = []GroupDoc{
	{Name: "Account", Description: "Inspect the current account's commercial entitlement and operate the license installed in this YottaBot cluster."},
	{Name: "Agent Platform", Description: "Define, govern, run, and observe agents, workflows, tools, model catalogs, orchestrators, gateways, templates, configs, and related execution assets."},
	{Name: "Context", Description: "Manage the universal resource registry and discovery handles for cloud accounts, GitHub organizations, Kubernetes clusters, and Yotta-managed resources."},
	{Name: "Identity", Description: "Manage human, machine, and agent principals plus groups, roles, policies, resource grants, credentials, sessions, approvals, audit, and events."},
	{Name: "Infrastructure", Description: "Operate the platform infrastructure products: YottaKeys/KMS keys, native Secrets Manager, Blob Storage, and Artifact Registry."},
	{Name: "Observability", Description: "Query and configure logs, metrics, alerts, dashboards, streams, destinations, and routing surfaces used to observe agentic systems."},
	{Name: "SRE Platform", Description: "Coordinate incidents, investigations, runbooks, postmortems, and change-operation records across humans and agents."},
	{Name: "Security Platform", Description: "Investigate security incidents, run governed passive-by-default assessments (pen tests / threat analysis), and persist evidence-backed security findings. Incidents live in Issues; this product owns investigations, hops, findings, and assessments."},
	{Name: "Drone Platform", Description: "Manage drone fleets, pilots, and drone workflows as a vertical product layered on the shared Agent Platform, Identity, and Observability substrate."},
	{Name: "Issues", Description: "Track backlog items, projects and tickets as operator-visible execution queues."},
	{Name: "Documents", Description: "Author, browse, search, version, and govern Markdown documents in a directory tree — a lean, AI-native knowledge base agents can read, cite, and update."},
	{Name: "Yotta Turbo", Description: "Govern compressed vector-index retrieval collections: create indexes over memory/documents/evidence, upsert items, and run account-scoped, allowlist-filtered nearest-neighbor queries so agents receive the smallest permission-filtered evidence for a task."},
	{Name: "Apply/Generate", Description: "Post-MVP apply/generate surfaces for migration playbooks and generation manifests. Stub routes are omitted from the live OpenAPI contract until implemented."},
}

// workflowRunActions is the run/observe/decision surface of the workflows noun
// (plan #73 Phase 3.5 — "add missing workflow run/observe routes to OpenAPI
// before building workflow run/observe skills"). The backend has had these
// since plan #10; this exposes them in the catalog so Albus can compose them.
func workflowRunActions() []Action {
	const base = "/v1/agent-platform/workflows"
	return []Action{
		// Plan #244 WP11 step 8: the canonical executions route. The cobra
		// command is bespoke (command/workflows_run.go resolves the placement);
		// this entry is what the catalog, Albus and the API docs describe.
		{Verb: "run", Method: "POST", Path: WorkflowsCanonicalPath + "/{id}/executions", IDParam: "id",
			Short: "Start a run of a workflow's published version (publish first; a draft never runs)"},
		{Verb: "runs", Method: "GET", Path: base + "/{id}/runs", IDParam: "id", Short: "List the runs of a workflow"},
		{Verb: "run-status", Method: "GET", Path: base + "/runs/{run_id}", IDParam: "run_id", Short: "Get a workflow run (status + result)"},
		{Verb: "run-steps", Method: "GET", Path: base + "/runs/{run_id}/steps", IDParam: "run_id", Short: "List the step-runs of a workflow run"},
		{Verb: "cancel-run", Method: "POST", Path: base + "/runs/{run_id}/cancel", IDParam: "run_id", Short: "Cancel a running workflow run"},
		{Verb: "approvals", Method: "GET", Path: base + "/approvals", IDParam: "", Short: "List workflow steps awaiting human approval (account-wide)"},
		{Verb: "step-decision", Method: "POST", Path: base + "/step-runs/{step_run_id}/decision", IDParam: "step_run_id", Body: true, Short: "Approve or reject a step awaiting human approval"},
		{Verb: "cancel-step-run", Method: "POST", Path: base + "/step-runs/{step_run_id}/cancel", IDParam: "step_run_id", Short: "Cancel one in-flight workflow step-run"},
	}
}

// WorkflowsCanonicalPath is the canonical workflows collection (plan #244).
const WorkflowsCanonicalPath = "/v1/automation-platform/workflows"

// workflowDraftActions is the canonical draft and version-lifecycle surface
// of the workflows noun (plan #244 WP2b-3). A definition change is a draft
// write (the bespoke `workflows update`), then an explicit publish.
func workflowDraftActions() []Action {
	const base = WorkflowsCanonicalPath
	return []Action{
		{Verb: "draft", Method: "GET", Path: base + "/{id}/draft", IDParam: "id",
			Short: "Get a workflow's editable draft",
			Description: "The workflow's single editable draft: its v2 definition, revision and canonical digest, " +
				"plus publish-mode warnings. A 404 carrying adoption_state means the workflow predates drafts and " +
				"has not been adopted yet (pending) or could not be (failed, with adoption_reason)."},
		{Verb: "publish", Method: "POST", Path: base + "/{id}/publish", IDParam: "id", Body: true,
			Short: "Publish the draft as a new immutable version",
			Description: "Makes the current draft an immutable, digest-pinned version and the one that runs. It is " +
				"conditional on the draft revision (If-Match) and idempotent (Idempotency-Key); yottactl supplies " +
				"both, from the draft it reads unless if_match names the revision you reviewed. The body is " +
				`{"reason": "…", "if_match": "draft-<revision>"}, both optional (send {} for neither): reason is ` +
				"recorded in the publish history as why this version exists, and if_match publishes exactly the " +
				"revision you read, so a draft changed since is refused rather than published. Publish needs " +
				"publish authority, which editing a draft does not grant."},
		{Verb: "deactivate", Method: "POST", Path: base + "/{id}/deactivate", IDParam: "id",
			Short: "Block new runs of the active version (the workflow becomes unavailable)",
			Description: "Blocks new ad-hoc executions and deployment activation. The active version is kept, so " +
				"reactivate restores exactly what ran before."},
		{Verb: "reactivate", Method: "POST", Path: base + "/{id}/reactivate", IDParam: "id",
			Short: "Allow new runs of the active version again"},
		{Verb: "unpublish", Method: "POST", Path: base + "/{id}/unpublish", IDParam: "id",
			Short: "Clear the active version, returning the workflow to draft",
			Description: "Refused (409 version_in_use) while a deployment or an unfinished execution pins the " +
				"version. The version itself is never deleted."},
		// The node-type registry, read-only (plan #244 WP1). A v2 draft is
		// written in these types, and until 2026-09-26 nothing in the catalog
		// could say what a node's parameters are: asked for an approval step,
		// Albus found no source for the type or its fields and declined rather
		// than guess. These sit on the workflows noun so a workspace scoped to
		// workflows offers them.
		{Verb: "node-types", Method: "GET", Path: NodeTypesPath, IDParam: "",
			Short: "List the node types a workflow graph can use",
			Description: "The platform's node-type registry: each type's id (e.g. yotta.human_approval, " +
				"yotta.no_op, yotta.agent), display name, category, whether a client may author it, and its " +
				"parameters, inputs and outputs. Read it before writing or editing a v2 definition."},
		{Verb: "node-type", Method: "GET", Path: NodeTypesPath + "/{type}", IDParam: "type",
			Short: "Get one node type: its parameters, inputs and outputs",
			Description: "One node type from the registry by its type id (e.g. yotta.human_approval): the " +
				"parameters a node of that type takes — names, types, which are required, allowed values — and " +
				"its inputs and outputs. Use it to write a node's parameters exactly."},
	}
}

// NodeTypesPath is the node-type registry (plan #244 WP1).
const NodeTypesPath = "/v1/automation-platform/node-types"

// tradingBacktestActions is the run-control + observation surface for a
// backtest run (plan #200 A9/A10/A11).
//
// launch is a plain Create on the noun, not an action, because it takes a
// whole envelope body and returns a new run; everything here acts on a run
// that already exists. The three control verbs are separated from the reads
// because they are a different AUTHORITY — trading_backtests:control — and
// keeping them visibly distinct in the CLI mirrors that.
func tradingBacktestActions() []Action {
	const base = "/v1/trading/backtests"
	return []Action{
		{Verb: "pause", Method: "POST", Path: base + "/{id}/pause", IDParam: "id",
			Short:       "Pause a run (stops admitting new shards)",
			Description: "Pause is cooperative: it stops NEW shards being admitted and, once the engine's segment-boundary poll ships, idles running shards at a sealed boundary. It never kills a pod, because that would discard the un-uploaded segment. Shards already running continue until then — use kill to stop work in flight."},
		{Verb: "resume", Method: "POST", Path: base + "/{id}/resume", IDParam: "id",
			Short: "Resume a paused run"},
		{Verb: "kill", Method: "POST", Path: base + "/{id}/kill", IDParam: "id",
			Short:       "Kill a run and cancel its shard Jobs",
			Description: "Marks the run killed and cancels its Kubernetes Jobs. The engine flushes and uploads its partial segment within the termination grace period, so a killed run still yields usable data up to the kill point."},
		{Verb: "bulk-action", Method: "POST", Path: base + "/bulk-action", IDParam: "", Body: true,
			Short: "Pause/resume/kill several runs, with a per-id outcome"},
		{Verb: "segments", Method: "GET", Path: base + "/{id}/segments", IDParam: "id",
			Short: "The run's segment index (object key, records, bytes, hash)"},
		{Verb: "metrics", Method: "GET", Path: base + "/{id}/metrics", IDParam: "id",
			Short:       "The run's computed metrics summary",
			Description: "The computed summary plus the provenance that makes it readable: the resolved fill mode, the fee hash, the engine build and the strategy image digest. An `optimistic` result is labelled as such everywhere it surfaces, so it can never be mistaken for a realistic one."},
		{Verb: "recompute-metrics", Method: "POST", Path: base + "/{id}/recompute-metrics", IDParam: "id",
			Short:       "Recompute a terminal run's metrics from its stored raw data",
			Description: "Re-runs the metrics job against the run's stored segments. Metric definitions change far more often than an engine does, and correcting one must never require re-running a backtest — the raw segments are the durable asset."},
	}
}

// tradingStrategyActions exposes a strategy's immutable versions.
func tradingStrategyActions() []Action {
	const base = "/v1/trading/strategies"
	return []Action{
		{Verb: "versions", Method: "GET", Path: base + "/{id}/versions", IDParam: "id",
			Short: "List a strategy's versions"},
		{Verb: "add-version", Method: "POST", Path: base + "/{id}/versions", IDParam: "id", Body: true,
			Short:       "Pin a new immutable, digest-pinned strategy version",
			Description: "A version pins a digest-pinned container image. A mutable tag is rejected: two concurrent runs of \"the same version\" must execute identical bytes, or a difference in results is indistinguishable from a real strategy difference."},
	}
}

// tradingFeeScheduleActions covers the parts of a cost stack that are not CRUD.
func tradingFeeScheduleActions() []Action {
	const base = "/v1/trading/fee-schedules"
	return []Action{
		{Verb: "components", Method: "POST", Path: base + "/{id}/components", IDParam: "id", Body: true,
			Short:       "Append an effective-dated rate row",
			Description: "Components are append-only: a re-import after a published rate change adds a new effective-dated row and never mutates an old one, because a run priced last month must re-price identically today."},
		{Verb: "clone", Method: "POST", Path: base + "/{id}/clone", IDParam: "id", Body: true,
			Short:       "Fork a schedule into an editable copy",
			Description: "Seeded schedules are immutable: a seeded row asserts a published broker's rates as of a retrieval date, and editing it in place would leave source_url claiming provenance the row no longer has. Clone, then edit the copy."},
		{Verb: "set-default", Method: "POST", Path: base + "/{id}/set-default", IDParam: "id",
			Short: "Make this the account's default schedule"},
		{Verb: "resolve", Method: "GET", Path: base + "/{id}/resolve", IDParam: "id",
			Short:       "What a fill would cost on a date, at a monthly volume",
			Description: "Resolves the cost stack for ?date=&shares=&variant=&side=, composing extra schedules with repeated ?compose_with=. Refuses rather than guesses: an unpriced \"contact for pricing\" tier, two candidate rate rows, or a component priced by two schedules are each an error naming the cause."},
	}
}

// tradingLatencyProfileActions is the default-selection verb.
func tradingLatencyProfileActions() []Action {
	return []Action{
		{Verb: "set-default", Method: "POST", Path: "/v1/trading/latency-profiles/{id}/set-default",
			IDParam: "id", Short: "Make this the account's default latency profile"},
	}
}

// secretsActions is the AWS-parity lifecycle/value/version surface plan
// #96 added on top of the base list/get/create/update/delete CRUD —
// schedule-delete/restore, native value read+write, version history +
// staging, and the read-only Access/Audit tabs. The value-read action is
// MCPExclude: true — see Action.MCPExclude's doc for why a secret-reveal
// GET must never auto-join yotta-mcp's agent-callable read surface even
// though it's a perfectly normal `yottactl secrets value <id>` command.
func secretsActions() []Action {
	const base = "/v1/secrets-manager/secrets"
	return []Action{
		{Verb: "restore", Method: "POST", Path: base + "/{id}/restore", IDParam: "id", Short: "Restore a secret scheduled for deletion"},
		{Verb: "value", Method: "GET", Path: base + "/{id}/value", IDParam: "id", Short: "Reveal a secret's value, by id or name; --query version_id= or version_stage= picks a version", MCPExclude: true},
		{Verb: "put-value", Method: "PUT", Path: base + "/{id}/value", IDParam: "id", Body: true, Short: "Write a new version of a secret's value (native secrets only)"},
		{Verb: "versions", Method: "GET", Path: base + "/{id}/versions", IDParam: "id", Short: "List a secret's version history and staging labels"},
		{Verb: "move-stage", Method: "POST", Path: base + "/{id}/versions/{versionId}/stages", IDParam: "id", IDParam2: "versionId", Body: true, Short: "Move a staging label (current/previous/pending) onto a version"},
		{Verb: "access", Method: "GET", Path: base + "/{id}/access", IDParam: "id", Short: "List principals with an Identity resource grant over this secret (read-only)"},
		{Verb: "audit", Method: "GET", Path: base + "/{id}/audit", IDParam: "id", Short: "List this secret's audit trail"},
		{Verb: "rotate", Method: "POST", Path: base + "/{id}/rotate", IDParam: "id", Body: true, Short: "Manually rotate a secret value"},
		{Verb: "cancel-rotation", Method: "POST", Path: base + "/{id}/cancel-rotation", IDParam: "id", Short: "Cancel a pending secret rotation"},
		{Verb: "rotations", Method: "GET", Path: base + "/{id}/rotations", IDParam: "id", Short: "List a secret's rotation history"},
	}
}

func issuesActions() []Action {
	const base = "/v1/issues/backlog"
	return []Action{
		{Verb: "cancel", Method: "POST", Path: base + "/{id}/cancel", IDParam: "id", Short: "Cancel a backlog item"},
	}
}

// incidentsActions is the non-CRUD surface of the `incidents` noun. Launch
// starts the account's default SRE investigation workflow for an incident
// (plan #103 runway 5/#116): it creates the investigation, mints/links the
// agent principal, writes read grants, and starts yotta-sre-investigation-v1.
// Body: {prompt, time_window?, scope?, mode?, max_hops?}. This is the op the
// Albus sre.investigation.launch skill (runway 17) dispatches through.
func incidentsActions() []Action {
	const base = "/v1/sre-platform/incidents"
	return []Action{
		{Verb: "launch-investigation", Method: "POST", Path: base + "/{id}/investigations/launch", IDParam: "id", Body: true,
			Short: "Launch the default SRE investigation workflow for an incident"},
	}
}

// issueProjectsActions is the non-CRUD surface of the `issue-projects`
// noun (plan #104): archive a project (read-only, tickets retained).
func issueProjectsActions() []Action {
	const base = "/v1/issues/projects"
	return []Action{
		{Verb: "archive", Method: "POST", Path: base + "/{id}/archive", IDParam: "id", Short: "Archive a project (makes it read-only; tickets are retained)"},
	}
}

// ticketsActions is the canonical Issues ticket surface beyond CRUD (plan
// #104): lifecycle transition, project move, epic hierarchy, issue↔issue
// links, comments (+ version history), the activity timeline, and the
// account-wide Roadmap. get/{id} accepts an issue id OR an issue key.
func ticketsActions() []Action {
	const base = "/v1/issues/tickets"
	return []Action{
		{Verb: "transition", Method: "POST", Path: base + "/{id}/transition", IDParam: "id", Body: true, Short: "Move a ticket to a new status (validated against the status machine)"},
		{Verb: "move", Method: "POST", Path: base + "/{id}/move", IDParam: "id", Body: true, Short: "Move a ticket to another project (re-keys it; old key aliased)"},
		{Verb: "children", Method: "GET", Path: base + "/{id}/children", IDParam: "id", Short: "List an epic's child tickets"},
		{Verb: "add-child", Method: "POST", Path: base + "/{id}/children", IDParam: "id", Body: true, Short: "Create a child ticket under an epic"},
		{Verb: "set-parent", Method: "PATCH", Path: base + "/{id}/parent", IDParam: "id", Body: true, Short: "Re-parent a ticket under an epic (or detach)"},
		{Verb: "links", Method: "GET", Path: base + "/{id}/links", IDParam: "id", Short: "List a ticket's issue↔issue links"},
		{Verb: "add-link", Method: "POST", Path: base + "/{id}/links", IDParam: "id", Body: true, Short: "Link a ticket to another (blocks/relates_to/duplicates/created_from)"},
		{Verb: "remove-link", Method: "DELETE", Path: base + "/{id}/links/{link_id}", IDParam: "id", IDParam2: "link_id", Short: "Remove a link from a ticket"},
		{Verb: "comments", Method: "GET", Path: base + "/{id}/comments", IDParam: "id", Short: "List a ticket's comments"},
		{Verb: "add-comment", Method: "POST", Path: base + "/{id}/comments", IDParam: "id", Body: true, Short: "Add a comment to a ticket"},
		{Verb: "edit-comment", Method: "PATCH", Path: "/v1/issues/comments/{comment_id}", IDParam: "comment_id", Body: true, Short: "Edit a comment (previous body kept in version history)"},
		{Verb: "delete-comment", Method: "DELETE", Path: "/v1/issues/comments/{comment_id}", IDParam: "comment_id", Short: "Soft-delete a comment (version history retained)"},
		{Verb: "comment-versions", Method: "GET", Path: "/v1/issues/comments/{comment_id}/versions", IDParam: "comment_id", Short: "List a comment's version history"},
		{Verb: "timeline", Method: "GET", Path: base + "/{id}/timeline", IDParam: "id", Short: "List a ticket's activity timeline"},
		{Verb: "roadmap", Method: "GET", Path: "/v1/issues/roadmap", Short: "Account-wide Gantt-style epic roadmap (epics + children)"},
	}
}

// documentsActions is the non-CRUD surface of the `documents` noun (docs):
// deterministic search, the Markdown body get/put (each put writes a new
// immutable version), move/archive/restore, and the version + event reads.
func documentsActions() []Action {
	const base = "/v1/documents/docs"
	return []Action{
		{Verb: "search", Method: "GET", Path: "/v1/documents/search", Short: "Search documents (deterministic full-text + filters)"},
		{Verb: "content", Method: "GET", Path: base + "/{id}/content", IDParam: "id", Short: "Read a document's current Markdown body"},
		{Verb: "put-content", Method: "PUT", Path: base + "/{id}/content", IDParam: "id", Body: true, Short: "Save a new Markdown body (creates a version)"},
		{Verb: "move", Method: "POST", Path: base + "/{id}/move", IDParam: "id", Body: true, Short: "Move a document to another directory"},
		{Verb: "archive", Method: "POST", Path: base + "/{id}/archive", IDParam: "id", Short: "Archive a document"},
		{Verb: "restore", Method: "POST", Path: base + "/{id}/restore", IDParam: "id", Short: "Restore an archived document"},
		{Verb: "versions", Method: "GET", Path: base + "/{id}/versions", IDParam: "id", Short: "List a document's versions"},
		{Verb: "version", Method: "GET", Path: base + "/{id}/versions/{version_id}", IDParam: "id", IDParam2: "version_id", Short: "Read a specific version's body"},
		{Verb: "version-restore", Method: "POST", Path: base + "/{id}/versions/{version_id}/restore", IDParam: "id", IDParam2: "version_id", Short: "Restore an old version as a new version"},
		{Verb: "events", Method: "GET", Path: base + "/{id}/events", IDParam: "id", Short: "List a document's events"},
	}
}

// documentDirectoriesActions exposes the directory browse listing + archive
// (create/get are CRUD verbs on the noun).
func documentDirectoriesActions() []Action {
	return []Action{
		{Verb: "list", Method: "GET", Path: "/v1/documents/directory", Short: "List a directory's child directories + documents"},
		{Verb: "archive", Method: "POST", Path: "/v1/documents/directories/{id}/archive", IDParam: "id", Short: "Archive a directory"},
	}
}

// yottaturboCollectionsActions is the item + query + durability surface of a
// Yotta Turbo collection (plan #146) beyond CRUD.
func yottaturboCollectionsActions() []Action {
	const base = "/v1/yottaturbo/collections"
	return []Action{
		{Verb: "upsert-items", Method: "POST", Path: base + "/{id}/items", IDParam: "id", Body: true, Short: "Upsert items (id + vector) into a collection"},
		{Verb: "delete-items", Method: "DELETE", Path: base + "/{id}/items", IDParam: "id", Body: true, Short: "Delete items from a collection by external id"},
		{Verb: "query", Method: "POST", Path: base + "/{id}/query", IDParam: "id", Body: true, Short: "Nearest-neighbor query (account-scoped, allowlist-filtered)"},
		{Verb: "stats", Method: "GET", Path: base + "/{id}/stats", IDParam: "id", Short: "Live item count + backend for a collection"},
		{Verb: "checkpoint", Method: "POST", Path: base + "/{id}/checkpoint", IDParam: "id", Short: "Checkpoint the compressed index (no-op on durable backends)"},
		{Verb: "rebuild", Method: "POST", Path: base + "/{id}/rebuild", IDParam: "id", Short: "Rebuild the compressed index (no-op on durable backends)"},
	}
}

// githubActions is the actions-only surface of the github noun: a read-only
// repo file fetch. The path is a wildcard tail (<owner>/<name>/<file-path>) so
// it fits one positional {spec}; optional ?ref=<branch|sha>. Plan #81 item 2.
func githubActions() []Action {
	return []Action{
		{Verb: "get-file", Method: "GET", Path: "/v1/agent-platform/github/file/{spec}", IDParam: "spec", Short: "Read a repo file: <owner>/<name>/<file-path> (via the account's GitHub App)"},
	}
}

// parentHandleActions exposes the non-CRUD poll trigger of the
// parent-handles noun (POST /v1/context/parent-handles/{id}/poll) so Albus
// can compose register -> poll -> verify in one governed action (the AWS
// discovery skill, plan #73 item 21). The backend endpoint has existed
// since plan #60; this surfaces it in the catalog + as a subcommand.
func parentHandleActions() []Action {
	return []Action{
		{Verb: "poll", Method: "POST", Path: "/v1/context/parent-handles/{id}/poll", IDParam: "id", Short: "Trigger an immediate discovery poll of a parent handle"},
	}
}

// contextSettingsActions — the account-scoped Context settings singleton
// (default discovery scrape cadence), plan #131 step 9. GET/PUT, no id.
func contextSettingsActions() []Action {
	return []Action{
		{Verb: "show", Method: "GET", Path: "/v1/context/settings", Short: "Read the account's Context settings (default scrape cadence)"},
		{Verb: "save", Method: "PUT", Path: "/v1/context/settings", Body: true, Short: "Set the account's Context default scrape cadence"},
	}
}

// contextResourceActions surfaces the Context resource one-hop graph reads
// that the backend has served since plan #60 but the yottactl catalog never
// exposed: /children, /relationships, /operations. Before plan #120, Albus —
// which reaches Context through yottactl under the user's JWT, not through
// MCP — could not walk the resource graph at all, while agents could via
// yotta-mcp. This closes that parity gap (plan #120 step 2). All three are
// GET / read-only, gated on context_resources:read exactly like the backend
// routes.
//
// MCPExclude: true on all three — agents already reach these via the curated
// yotta-mcp Context tools (list_context_resource_children /
// list_context_resource_relationships / get_context_resource_operations, plan
// #86 Track A), so re-emitting them into yotta-mcp's registry-generated read
// surface would just duplicate those tools in every agent's tool list. They
// still land in catalog.json + OpenAPI and as `yottactl context resources
// <verb>` subcommands, which is the Albus path this slice is for.
func contextResourceActions() []Action {
	return []Action{
		{Verb: "children", Method: "GET", Path: "/v1/context/resources/{id}/children", IDParam: "id", MCPExclude: true, Short: "List a Context resource's direct children (walk the containment tree)"},
		{Verb: "relationships", Method: "GET", Path: "/v1/context/resources/{id}/relationships", IDParam: "id", MCPExclude: true, Short: "List a Context resource's directed edges (owned_by/backed_by/consumes_secret/routes_to/...)"},
		{Verb: "operations", Method: "GET", Path: "/v1/context/resources/{id}/operations", IDParam: "id", MCPExclude: true, Short: "Read a Context resource's operation capability set (capability_verbs + permitted_verbs)"},
	}
}

// contextGraphActions exposes the bounded Context graph-traversal
// primitives (plan #120 step 5) so Albus — reaching Context through
// yottactl under the user's JWT — can walk the topology/dependency/
// secret-trust graph the same way the SRE agent does through yotta-mcp.
// All POST (structured body: resource_id, relations, classes, direction,
// max_depth, max_nodes), read-only, gated on context_resources:read. The
// context service runs each walk account-scoped + cost-bounded. (No
// MCPExclude needed — POST actions never enter yotta-mcp's GET-only
// generated read surface; agents use the curated blast_radius/trace_path/
// neighbors MCP tools.)
// contextGraphInstanceActions — the progressive-disclosure path over a
// stored graph, in the order an agent should walk it: list (handles) →
// get (summary) → result (the graph) → proof (one edge's evidence).
// Opening result before reading the summary loads a graph to decide
// whether you wanted it, which is the cost the inventory exists to avoid.
func contextGraphInstanceActions() []Action {
	return []Action{
		// IDParam is REQUIRED whenever Path carries a {param}: buildAction
		// substitutes on IDParam, not by parsing the path, so an action that
		// declares the placeholder and not the param takes no arguments and
		// calls a URL with a literal "{id}" in it. All three of these shipped
		// that way in #208 step 7, which meant the disclosure path the plan
		// built — list, then summary, then result, then proof — could be
		// listed and never opened from the CLI.
		{Verb: "result", Method: "GET", Path: "/v1/context/graphs/{id}/result", IDParam: "id", Short: "Render one stored graph from its own saved query — no origin/depth to supply"},
		{Verb: "proof", Method: "GET", Path: "/v1/context/graphs/{id}/proof", IDParam: "id", Short: "Source evidence for ONE edge (from_id + to_id + relation) — open it to verify a claim, not to browse"},
		{Verb: "refresh", Method: "POST", Path: "/v1/context/graphs/{id}/refresh", IDParam: "id", Short: "Mark a graph due for recompute; returns refreshing plus the last known summary"},
	}
}

func contextGraphActions() []Action {
	return []Action{
		{Verb: "blast-radius", Method: "POST", Path: "/v1/context/graph/blast-radius", Body: true, Short: "What depends on / consumes a resource (inbound trust+dependency walk) — the secret blast radius"},
		{Verb: "trace-path", Method: "POST", Path: "/v1/context/graph/trace-path", Body: true, Short: "Shortest path between two Context resources over the relationship graph"},
		// Prefer `graph` over hand-rolled relation lists: {"resource_id":"...",
		// "graph":"service"} resolves the class union, direction, and depth
		// from the catalog, so a caller never has to know that Service means
		// dependency+trust. Explicit relations/classes still win where a
		// narrower walk is wanted.
		{Verb: "neighbors", Method: "POST", Path: "/v1/context/graph/neighbors", Body: true, Short: "Bounded walk from one resource — pass graph=service|network|request (see `context graph-definitions list`), or hand-filter with relations/classes"},
		// service_dependencies is the DEPENDENCY-ONLY subset of the Service
		// graph, not a synonym for it: the graph also spans trust (secrets).
		// Kept as the answer-shaped shortcut existing skills call by name.
		{Verb: "pack", Method: "POST", Path: "/v1/context/graph/pack", Body: true, Short: "Answer-shaped context pack (kind=secret_blast_radius|resource_summary|service_dependencies|network_path). service_dependencies is the dependency-only subset of graph=service, which also spans trust"},
	}
}

// serviceAccountActions — a service account's secrets tokens (plan 298), for what
// can only send a fixed header, External Secrets above all.
func serviceAccountActions() []Action {
	const base = "/v1/identity/machines/identities"
	return []Action{
		{Verb: "access-tokens", Method: "GET", Path: base + "/{id}/access-tokens", IDParam: "id", Short: "List a service account's secrets tokens"},
		{Verb: "create-access-token", Method: "POST", Path: base + "/{id}/access-tokens", IDParam: "id", Body: true, Short: "Make a token that reads only the secrets this service account holds a grant on; shown once", MCPExclude: true},
		{Verb: "revoke-access-token", Method: "DELETE", Path: base + "/{id}/access-tokens/{tokenId}", IDParam: "id", IDParam2: "tokenId", Short: "Revoke a service account's secrets token"},
	}
}

// machineCredentialActions — the Universal-Logout-equivalent kill switch
// on machine (client) credentials (plan #83 Step 1, backend from plan #7).
func machineCredentialActions() []Action {
	return []Action{
		{Verb: "revoke-all", Method: "POST", Path: "/v1/identity/machines/credentials/revoke-all", Body: true, Short: "Revoke all machine credentials for a user (kill switch)"},
	}
}

// humanSessionActions — revoke a human login session by jti (plan #83 Step 4).
func humanSessionActions() []Action {
	return []Action{
		{Verb: "revoke", Method: "POST", Path: "/v1/identity/humans/sessions/{jti}/revoke", IDParam: "jti", Short: "Revoke a human login session by jti"},
	}
}

// approvalDecisionActions — reviewer verbs on the Governance approval
// queue (plan #83 Step 6); backend routes as /{id}/{decision}.
func approvalDecisionActions() []Action {
	return []Action{
		{Verb: "approve", Method: "POST", Path: "/v1/identity/governance/approvals/{id}/approve", IDParam: "id", Short: "Approve a pending approval"},
		{Verb: "reject", Method: "POST", Path: "/v1/identity/governance/approvals/{id}/reject", IDParam: "id", Short: "Reject a pending approval"},
		{Verb: "cancel", Method: "POST", Path: "/v1/identity/governance/approvals/{id}/cancel", IDParam: "id", Short: "Cancel a pending approval"},
	}
}

// roleAttachActions — link/unlink policies + human groups to a role
// (plan #83 Step 2b). Detach uses the two-positional-arg action form.
func roleAttachActions() []Action {
	return []Action{
		{Verb: "attach-policy", Method: "POST", Path: "/v1/identity/access/roles/{id}/policies", IDParam: "id", Body: true, Short: "Attach a policy to a role (body {policy_id})"},
		{Verb: "detach-policy", Method: "DELETE", Path: "/v1/identity/access/roles/{id}/policies/{policyId}", IDParam: "id", IDParam2: "policyId", Short: "Detach a policy from a role"},
		{Verb: "attach-group", Method: "POST", Path: "/v1/identity/access/roles/{id}/groups", IDParam: "id", Body: true, Short: "Attach a human group to a role (body {group_id})"},
		{Verb: "detach-group", Method: "DELETE", Path: "/v1/identity/access/roles/{id}/groups/{groupId}", IDParam: "id", IDParam2: "groupId", Short: "Detach a human group from a role"},
	}
}

// groupMemberActions — add/remove a principal to/from a typed group
// (plan #83 Step 2b); both use the two-positional-arg action form
// (POST/DELETE .../{id}/members/{principalId}).
func groupMemberActions(base string) []Action {
	return []Action{
		{Verb: "add-member", Method: "POST", Path: base + "/{id}/members/{principalId}", IDParam: "id", IDParam2: "principalId", Short: "Add a principal to the group"},
		{Verb: "remove-member", Method: "DELETE", Path: base + "/{id}/members/{principalId}", IDParam: "id", IDParam2: "principalId", Short: "Remove a principal from the group"},
	}
}

// ── plan #112 second slice: missing public REST route helpers ────────

// llmGatewayActions — the LLM inference gateway usage rollup (plan #109),
// on top of the base CRUD.
func llmGatewayActions() []Action {
	const base = "/v1/agent-platform/llm-gateways"
	return []Action{
		{Verb: "usage", Method: "GET", Path: base + "/{id}/usage", IDParam: "id", Short: "Month-to-date usage + spend rollup for an LLM gateway"},
		// Plan #132 Track E — the gateway's upstream provider routes (Anthropic,
		// OpenAI/ChatGPT, …) that live behind the Yotta control point.
		{Verb: "routes", Method: "GET", Path: base + "/{id}/routes", IDParam: "id",
			Short:       "List a gateway's upstream provider routes",
			Description: "List the upstream provider routes behind a Yotta LLM Gateway — the per-provider endpoint/auth/credential/status/health records the gateway routes model invocations through. Credential refs are returned as copyable references, never raw keys."},
	}
}

// agentReadActions — the agent introspection reads (grants + resolved tool
// manifest) plus the live worker-state projections (instances / runs) and the
// agent-runtime session reads. All GET, read-only. Modeled as actions
// (not nouns) so the agent-runtime sessions don't collide with the
// Identity `agent-sessions` noun name in the CRUD-contract guards.
//
// The trace reads left this list with plan #203 A2: traces are a Logs
// surface now, registered as the `traces` noun under Observability.
// /v1/agent-platform/traces survives as a deprecated alias for hand-written
// callers, but the model-facing catalog advertises one path, not two — the
// #194 pattern.
func agentReadActions() []Action {
	const base = "/v1/agent-platform/agents"
	return []Action{
		{Verb: "grants", Method: "GET", Path: base + "/{id}/grants", IDParam: "id", Short: "List an agent's Identity resource grants"},
		{Verb: "tool-manifest", Method: "GET", Path: base + "/{id}/tool-manifest", IDParam: "id", Short: "Read an agent's resolved tool manifest"},
		{Verb: "instances", Method: "GET", Path: base + "/instances", Short: "List running agent instances (live worker state)"},
		{Verb: "instance", Method: "GET", Path: base + "/instances/{id}", IDParam: "id", Short: "Get one agent instance"},
		{Verb: "runs", Method: "GET", Path: base + "/runs", Short: "List agent runs"},
		{Verb: "run-detail", Method: "GET", Path: base + "/runs/{id}", IDParam: "id", Short: "Get one agent run"},
		{Verb: "sessions", Method: "GET", Path: "/v1/agent-platform/sessions", Short: "List agent runtime sessions"},
		{Verb: "session", Method: "GET", Path: "/v1/agent-platform/sessions/{id}", IDParam: "id", Short: "Get one agent runtime session"},
	}
}

// agentTemplateActions — versioned agent templates (update is PUT on the
// backend, so it rides as an action rather than the PATCH Update verb).
func agentTemplateActions() []Action {
	const base = "/v1/agent-platform/agent-templates"
	return []Action{
		{Verb: "update", Method: "PUT", Path: base + "/{id}", IDParam: "id", Body: true, Short: "Update an agent template"},
		{Verb: "version", Method: "GET", Path: "/v1/agent-platform/agent-template-versions/{id}", IDParam: "id", Short: "Get an agent-template version by id"},
		{Verb: "versions", Method: "GET", Path: base + "/{id}/versions", IDParam: "id", Short: "List an agent template's versions"},
		{Verb: "add-version", Method: "POST", Path: base + "/{id}/versions", IDParam: "id", Body: true, Short: "Publish a new agent-template version"},
		{Verb: "configs", Method: "GET", Path: base + "/{id}/configs", IDParam: "id", Short: "List configs derived from an agent template"},
	}
}

func agentConfigActions() []Action {
	const base = "/v1/agent-platform/agent-configs"
	return []Action{
		{Verb: "update", Method: "PUT", Path: base + "/{id}", IDParam: "id", Body: true, Short: "Update an agent config"},
		{Verb: "render", Method: "POST", Path: base + "/{id}/render", IDParam: "id", Short: "Render an agent config into a concrete agent spec"},
	}
}

func workflowTemplateActions() []Action {
	const base = "/v1/agent-platform/workflow-templates"
	return []Action{
		{Verb: "update", Method: "PUT", Path: base + "/{id}", IDParam: "id", Body: true, Short: "Update a workflow template"},
		{Verb: "version", Method: "GET", Path: "/v1/agent-platform/workflow-template-versions/{id}", IDParam: "id", Short: "Get a workflow-template version by id"},
		{Verb: "versions", Method: "GET", Path: base + "/{id}/versions", IDParam: "id", Short: "List a workflow template's versions"},
		{Verb: "add-version", Method: "POST", Path: base + "/{id}/versions", IDParam: "id", Body: true, Short: "Publish a new workflow-template version"},
		{Verb: "configs", Method: "GET", Path: base + "/{id}/configs", IDParam: "id", Short: "List configs derived from a workflow template"},
	}
}

func workflowConfigActions() []Action {
	const base = "/v1/agent-platform/workflow-configs"
	return []Action{
		{Verb: "update", Method: "PUT", Path: base + "/{id}", IDParam: "id", Body: true, Short: "Update a workflow config"},
		{Verb: "render", Method: "POST", Path: base + "/{id}/render", IDParam: "id", Short: "Render a workflow config into a concrete workflow spec"},
	}
}

// catalogActions — the installable-asset catalog browse + install (plan #112).
// install takes two path params: {kind} (models|orchestrators|tools|
// mcp-gateways|llm-gateways) and {id} (the catalog row). `runtimes` is still
// accepted as the deprecated spelling of `orchestrators` (plan #194).
func catalogActions() []Action {
	const base = "/v1/agent-platform/catalog"
	return []Action{
		{Verb: "models", Method: "GET", Path: base + "/models", Short: "Browse installable models in the platform catalog"},
		{Verb: "orchestrators", Method: "GET", Path: base + "/orchestrators", Short: "Browse installable orchestrators in the platform catalog"},
		{Verb: "runtimes", Method: "GET", Path: base + "/runtimes", Short: "Browse installable runtimes (deprecated — use `orchestrators`)", SupersededBy: "orchestrators"},
		{Verb: "tools", Method: "GET", Path: base + "/tools", Short: "Browse installable tools in the platform catalog"},
		{Verb: "mcp-gateways", Method: "GET", Path: base + "/mcp-gateways", Short: "Browse installable MCP gateways in the platform catalog"},
		{Verb: "llm-gateways", Method: "GET", Path: base + "/llm-gateways", Short: "Browse installable LLM gateways in the platform catalog"},
		{Verb: "install", Method: "POST", Path: base + "/{kind}/{id}/install", IDParam: "kind", IDParam2: "id", Short: "Install a catalog entry into the account"},
	}
}

// deploymentTargetActions — the set-default op on the runtime deployment
// targets (plan #111 §7). Promoted from a bespoke CLI-only read to the full
// registry surface (list/create/delete + this action).
func deploymentTargetActions() []Action {
	return []Action{
		{Verb: "set-default", Method: "POST", Path: "/v1/agent-platform/deployment-targets/{id}/default", IDParam: "id", Short: "Make a deployment target the account default"},
	}
}

// modelBindingActions — bind a managed model to an LLM gateway + provider
// model id (plan #109/#112).
func modelBindingActions() []Action {
	return []Action{
		{Verb: "bind", Method: "PATCH", Path: "/v1/agent-platform/models/{id}/binding", IDParam: "id", Body: true, Short: "Bind a model to an LLM gateway + provider model id"},
		// Plan #132 step 6 — discover the models a customer's upstream credential
		// can run and upsert them into the registry. Body: {gateway_id, provider}.
		{Verb: "sync", Method: "POST", Path: "/v1/agent-platform/models/sync", Body: true,
			Short:       "Sync models from a gateway upstream route (provider discovery)",
			Description: "Discover the models the account's upstream credential for a provider can run, through a Yotta LLM Gateway upstream route, and upsert them into the model registry bound to that gateway. Resolves the route credential server-side through the credential broker; never exposes the provider key. Models the provider no longer returns are marked unavailable, not deleted. Body: {\"gateway_id\": \"<llm-gateway id>\", \"provider\": \"anthropic|openai|openai_compatible\"}."},
	}
}

// agentPlatformSettingsActions — the account-scoped agent-platform settings
// singleton (GET/PUT). Named show/save so the action tool names don't collide
// with the id-param CRUD suffix guard.
func agentPlatformSettingsActions() []Action {
	return []Action{
		{Verb: "show", Method: "GET", Path: "/v1/agent-platform/settings", Short: "Read the account's agent-platform settings"},
		{Verb: "save", Method: "PUT", Path: "/v1/agent-platform/settings", Body: true, Short: "Replace the account's agent-platform settings"},
	}
}

// providerActions — the YottaKeys/KMS provider catalog browse (providers CRUD
// is the noun's verbs; there is no routes surface for KMS providers).
// keyRecordActions — the OpenPGP artifact-signing surface (plan #124 step 8).
//
// These exist so a RELEASE PIPELINE can reach signing without a bespoke
// client: `yottactl kms keys openpgp-certificate <id>` fetches what gets
// registered with the Terraform Registry, and `openpgp-sign` produces the
// detached signature over SHA256SUMS. That is the whole point of #124 for
// #218 — the release workflow calls Yotta Keys instead of holding a private
// GPG key in a GitHub Actions secret.
//
// Body: true on the sign action because the bytes being signed ARE the body.
// An OpenPGP signature hashes the message together with the signature's own
// trailer, so a pre-computed digest cannot be substituted for the file. It is
// true on openpgp-revocation for a duller reason: the optional
// {"reason","reason_text"} JSON. An empty body is valid and is the call made
// at key creation.
//
// openpgp-revocation GENERATES a revocation certificate; it does not revoke.
// The key keeps signing and no record changes. Revocation happens when the
// output is published. Store it outside the environment and record the location in
// the key record's revocation_ref — see .ai/runbooks/release-key-revocation.md.
func keyRecordActions() []Action {
	const base = "/v1/kms/keys"
	return []Action{
		{Verb: "openpgp-certificate", Method: "GET", Path: base + "/{id}/openpgp-certificate", IDParam: "id",
			Short: "Fetch the armored OpenPGP public certificate + fingerprint for an artifact-signing key"},
		{Verb: "openpgp-sign", Method: "POST", Path: base + "/{id}/openpgp-sign", IDParam: "id", Body: true, BodyContentType: "application/octet-stream",
			Short: "Produce an OpenPGP detached signature over the supplied bytes (requires kms:sign)"},
		{Verb: "openpgp-revocation", Method: "POST", Path: base + "/{id}/openpgp-revocation", IDParam: "id", Body: true,
			Short: "Produce a revocation certificate to store offline (-f reason.yaml) — generates only, revokes nothing; requires kms:sign"},
	}
}

func providerActions() []Action {
	return []Action{
		{Verb: "catalog", Method: "GET", Path: "/v1/providers_catalog", Short: "Browse the Yotta Keys provider catalog"},
	}
}

// destinationsActions — the shared telemetry-destinations catalog + routing
// surface (plan #39) that rides on each observability product's destinations
// noun. base is the service-prefixed destinations path.
// metricsReadActions — the metrics-manager read surface beyond the base
// per-agent metrics list (plan #112).
func metricsReadActions() []Action {
	const base = "/v1/metrics-manager"
	return []Action{
		{Verb: "cost-summary", Method: "GET", Path: base + "/cost/summary", Short: "Account cost summary rollup"},
		{Verb: "workflows", Method: "GET", Path: base + "/metrics/workflows", Short: "Per-workflow metrics"},
		{Verb: "mcps", Method: "GET", Path: base + "/metrics/mcps", Short: "Per-MCP-server metrics"},
		{Verb: "series", Method: "GET", Path: base + "/metrics", Short: "Search the metric series catalog"},
	}
}

// alertsActions — alert search + (the base noun gains full CRUD incl. delete).
func alertsActions() []Action {
	return []Action{
		{Verb: "search", Method: "GET", Path: "/v1/alerts-manager/alerts/search", Short: "Search alerts (filtered query)"},
		// An alert's notes (plan 318 Step 1): what people wrote while working it.
		{Verb: "notes", Method: "GET", Path: "/v1/alerts-manager/alerts/{id}/notes", IDParam: "id", Short: "List an alert's notes, oldest first"},
		{Verb: "add-note", Method: "POST", Path: "/v1/alerts-manager/alerts/{id}/notes", IDParam: "id", Body: true, Short: "Add a note to an alert ({\"body\": \"...\"})"},
		// Which route an alert meets first, so which policy it pages (plan 318 Step 3, part 3).
		{Verb: "route", Method: "GET", Path: "/v1/alerts-manager/alerts/{id}/route", IDParam: "id", Short: "Which route an alert meets first, and the labels routes see"},
		// Its page and what happened on it (plan 318 Step 3, part 4).
		{Verb: "pages", Method: "GET", Path: "/v1/alerts-manager/alerts/{id}/pages", IDParam: "id", Short: "An alert's page: whom it notified, level by level, and how it ended"},
	}
}

// alertIncidentsActions — an incident's alerts, and acknowledging or
// resolving it (its alerts as the caller, SRE's incident after; plan 318).
func alertIncidentsActions() []Action {
	const base = "/v1/alerts-manager/incidents"
	return []Action{
		{Verb: "pages", Method: "GET", Path: "/v1/alerts-manager/incidents/{id}/pages", IDParam: "id", Short: "An incident's page: whom it notified, level by level, and how it ended"},
		{Verb: "alerts", Method: "GET", Path: base + "/{id}/alerts", IDParam: "id", Short: "List an incident's alerts, newest first"},
		{Verb: "acknowledge", Method: "POST", Path: base + "/{id}/acknowledge", IDParam: "id", Short: "Acknowledge an incident and its firing alerts"},
		{Verb: "resolve", Method: "POST", Path: base + "/{id}/resolve", IDParam: "id", Short: "Resolve an incident and every alert in it"},
	}
}

// alertRoutesActions — replacing the account's whole list of routes (PUT, as
// PagerDuty's API replaces a router's rules) and trying labels against it
// (plan 318 Step 3, part 3).
func alertRoutesActions() []Action {
	const base = "/v1/alerts-manager/routes"
	return []Action{
		{Verb: "replace", Method: "PUT", Path: base, Body: true, Short: "Replace the account's routes, in order ({\"routes\": [...]})"},
		{Verb: "match", Method: "POST", Path: base + "/match", Body: true, Short: "Which route labels meet first ({\"labels\": {...}})"},
	}
}

// alertUsersActions — a person's contact methods (a number for texts, an
// email address) and notification rules (when to use which after they are
// paged), on PagerDuty's user model (plan 318 Step 3, part 5). "me" is the
// caller's own user id.
func alertUsersActions() []Action {
	const base = "/v1/alerts-manager/users"
	return []Action{
		{Verb: "add-contact-method", Method: "POST", Path: base + "/{userId}/contact-methods", IDParam: "userId", Body: true, Short: "Give a person a contact method ({\"type\": \"sms\"|\"email\", \"address\", \"label\"})"},
		{Verb: "replace-contact-method", Method: "PUT", Path: base + "/{userId}/contact-methods/{id}", IDParam: "userId", IDParam2: "id", Body: true, Short: "Change a contact method's address and label"},
		{Verb: "remove-contact-method", Method: "DELETE", Path: base + "/{userId}/contact-methods/{id}", IDParam: "userId", IDParam2: "id", Short: "Remove a contact method and the rules that use it"},
		{Verb: "send-code", Method: "POST", Path: base + "/{userId}/contact-methods/{id}/send-code", IDParam: "userId", IDParam2: "id", Short: "Text or mail a contact method a code that confirms it (plan 318 part 6)"},
		{Verb: "confirm-contact-method", Method: "POST", Path: base + "/{userId}/contact-methods/{id}/confirm", IDParam: "userId", IDParam2: "id", Body: true, Short: "Confirm a contact method with the code sent to it ({\"code\"})"},
		{Verb: "add-notification-rule", Method: "POST", Path: base + "/{userId}/notification-rules", IDParam: "userId", Body: true, Short: "Send to a contact method a delay after the person is paged ({\"contact_method_id\", \"start_delay_minutes\"})"},
		{Verb: "replace-notification-rule", Method: "PUT", Path: base + "/{userId}/notification-rules/{id}", IDParam: "userId", IDParam2: "id", Body: true, Short: "Change a notification rule's delay and contact method"},
		{Verb: "remove-notification-rule", Method: "DELETE", Path: base + "/{userId}/notification-rules/{id}", IDParam: "userId", IDParam2: "id", Short: "Remove a notification rule"},
	}
}

// alertEscalationActions — replacing a whole escalation policy (PUT, as
// PagerDuty's API does, so it rides as an action rather than the PATCH Update
// verb) and whom each level would page (plan 318 Step 3, part 2).
func alertEscalationActions() []Action {
	const base = "/v1/alerts-manager/escalation-policies"
	return []Action{
		{Verb: "replace", Method: "PUT", Path: base + "/{id}", IDParam: "id", Body: true, Short: "Replace an escalation policy, its levels and all"},
		{Verb: "oncall", Method: "GET", Path: base + "/{id}/oncall", IDParam: "id", Short: "Whom each level would page, at --query at= (now)"},
	}
}

// alertSchedulesActions — a schedule's layers and overrides, and who is on
// call (plan 318 Step 3, part 1). oncall takes --query to=<RFC 3339> (and
// optionally from=, now when absent).
func alertSchedulesActions() []Action {
	const base = "/v1/alerts-manager/schedules"
	return []Action{
		{Verb: "oncall", Method: "GET", Path: base + "/{id}/oncall", IDParam: "id", Short: "Who is on call on a schedule, from --query from= (now) to --query to="},
		{Verb: "add-layer", Method: "POST", Path: base + "/{id}/layers", IDParam: "id", Body: true, Short: "Add a rotation layer to a schedule"},
		{Verb: "replace-layer", Method: "PUT", Path: base + "/{id}/layers/{layerId}", IDParam: "id", IDParam2: "layerId", Body: true, Short: "Replace one of a schedule's layers"},
		{Verb: "remove-layer", Method: "DELETE", Path: base + "/{id}/layers/{layerId}", IDParam: "id", IDParam2: "layerId", Short: "Remove a layer from a schedule"},
		{Verb: "add-override", Method: "POST", Path: base + "/{id}/overrides", IDParam: "id", Body: true, Short: "Put one person on a schedule for a stretch of time"},
		{Verb: "remove-override", Method: "DELETE", Path: base + "/{id}/overrides/{overrideId}", IDParam: "id", IDParam2: "overrideId", Short: "Remove an override from a schedule"},
	}
}

// ── SRE Platform (plan #103) ─────────────────────────────────────────

func findingsActions() []Action {
	const base = "/v1/sre-platform/findings"
	return []Action{
		{Verb: "content", Method: "GET", Path: base + "/{id}/content", IDParam: "id", Short: "Read a finding's rendered content body"},
	}
}

// incidentInvestigationActions extends the incidents noun with the
// investigation subtree nested under an incident (list + open).
func incidentInvestigationActions() []Action {
	const base = "/v1/sre-platform/incidents"
	return []Action{
		{Verb: "investigations", Method: "GET", Path: base + "/{id}/investigations", IDParam: "id", Short: "List an incident's investigations"},
		{Verb: "open-investigation", Method: "POST", Path: base + "/{id}/investigations", IDParam: "id", Body: true, Short: "Open a new investigation on an incident"},
	}
}

func investigationActions() []Action {
	const base = "/v1/sre-platform/incidents/investigations"
	return []Action{
		{Verb: "hops", Method: "GET", Path: base + "/{id}/hops", IDParam: "id", Short: "List an investigation's reasoning hops"},
		{Verb: "add-hop", Method: "POST", Path: base + "/{id}/hops", IDParam: "id", Body: true, Short: "Append a reasoning hop to an investigation"},
		{Verb: "add-finding", Method: "POST", Path: base + "/{id}/findings", IDParam: "id", Body: true, Short: "Record a finding on an investigation"},
	}
}

// ── Security Platform (plan #125) ─────────────────────────────────────

func securityInvestigationActions() []Action {
	const base = "/v1/security-platform"
	return []Action{
		{Verb: "launch", Method: "POST", Path: base + "/incidents/{issue_id}/investigations/launch", IDParam: "issue_id", Body: true, Short: "Open a security investigation for an Issues incident ticket"},
		{Verb: "hops", Method: "GET", Path: base + "/investigations/{id}/hops", IDParam: "id", Short: "List a security investigation's hops"},
		{Verb: "add-hop", Method: "POST", Path: base + "/investigations/{id}/hops", IDParam: "id", Body: true, Short: "Append a hop to a security investigation"},
	}
}

func securityFindingsActions() []Action {
	return []Action{
		{Verb: "content", Method: "GET", Path: "/v1/security-platform/findings/{id}/content", IDParam: "id", Short: "Read a security finding's markdown body"},
	}
}

func securityAssessmentActions() []Action {
	const base = "/v1/security-platform"
	return []Action{
		{Verb: "run", Method: "POST", Path: base + "/assessments/{id}/run", IDParam: "id", Short: "Start an assessment run (passive-by-default; active mode needs policy.approved)"},
		{Verb: "run-get", Method: "GET", Path: base + "/assessment-runs/{run_id}", IDParam: "run_id", Short: "Get an assessment run"},
		{Verb: "run-stop", Method: "POST", Path: base + "/assessment-runs/{run_id}/stop", IDParam: "run_id", Short: "Stop an in-flight assessment run"},
	}
}

func postmortemActions() []Action {
	return []Action{
		{Verb: "finalize", Method: "POST", Path: "/v1/sre-platform/postmortems/{id}/finalize", IDParam: "id", Short: "Finalize a post-mortem (locks it)"},
	}
}

// ── Compliance Platform (plan #127) ───────────────────────────────────

func complianceFrameworkActions() []Action {
	return []Action{
		{Verb: "enable", Method: "POST", Path: "/v1/compliance-platform/frameworks/{id}/enable", IDParam: "id", Short: "Enable a framework pack for the account"},
	}
}

func complianceEvidenceTaskActions() []Action {
	return []Action{
		{Verb: "run", Method: "POST", Path: "/v1/compliance-platform/evidence-tasks/{id}/run", IDParam: "id", Body: true, Short: "Run-now: record an evidence run and launch the managed collection workflow (body: audit_id / period_start / period_end, all optional)"},
	}
}

func complianceScheduleActions() []Action {
	return []Action{
		{Verb: "run-now", Method: "POST", Path: "/v1/compliance-platform/schedules/{id}/run-now", IDParam: "id", Short: "Record + launch a collection run for the schedule's task now (does not move the cadence)"},
	}
}

func complianceEvidenceRunActions() []Action {
	return []Action{
		{Verb: "stop", Method: "POST", Path: "/v1/compliance-platform/evidence-runs/{id}/stop", IDParam: "id", Short: "Stop a queued/running evidence collection run"},
		{Verb: "complete", Method: "POST", Path: "/v1/compliance-platform/evidence-runs/{id}/complete", IDParam: "id", Body: true, Short: "Mark an evidence run terminal (completed|failed|needs_review) with a summary"},
	}
}

func complianceEvidenceActions() []Action {
	return []Action{
		{Verb: "review", Method: "PATCH", Path: "/v1/compliance-platform/evidence/{id}/review", IDParam: "id", Body: true, Short: "Record an accept/reject review decision on an evidence item"},
	}
}

func complianceAuditActions() []Action {
	return []Action{
		{Verb: "export", Method: "POST", Path: "/v1/compliance-platform/audits/{id}/exports", IDParam: "id", Body: true, Short: "Build an immutable evidence packet for an audit (body: name / format = zip|json|markdown)"},
	}
}

func complianceExportActions() []Action {
	return []Action{
		// MCPExclude: the download returns package BYTES (zip binaries) — a
		// normal CLI verb but not a sane agent-callable MCP tool result.
		{Verb: "download", Method: "GET", Path: "/v1/compliance-platform/exports/{id}/download", IDParam: "id", MCPExclude: true, Short: "Download an export package (streams the bytes; X-Manifest-SHA256 header for verification)"},
	}
}

func complianceFindingActions() []Action {
	const base = "/v1/compliance-platform/findings"
	return []Action{
		{Verb: "create-issue", Method: "POST", Path: base + "/{id}/issue", IDParam: "id", Short: "Open the optional Issues remediation ticket for a finding (one per finding; linked via issue_id)"},
		{Verb: "launch-investigation", Method: "POST", Path: base + "/{id}/investigations/launch", IDParam: "id", Body: true, Short: "Launch the managed gap investigation for a finding (body: title / prompt, optional)"},
	}
}

func complianceInvestigationActions() []Action {
	const base = "/v1/compliance-platform/investigations"
	return []Action{
		{Verb: "hops", Method: "GET", Path: base + "/{id}/hops", IDParam: "id", Short: "List an investigation's reasoning hops"},
		{Verb: "add-hop", Method: "POST", Path: base + "/{id}/hops", IDParam: "id", Body: true, Short: "Append a citeable reasoning hop to an investigation"},
		{Verb: "complete", Method: "POST", Path: base + "/{id}/complete", IDParam: "id", Body: true, Short: "Mark an investigation terminal (completed|failed|canceled) with a summary"},
	}
}

func runbookActions() []Action {
	const base = "/v1/sre-platform/runbooks"
	return []Action{
		{Verb: "versions", Method: "GET", Path: base + "/{id}/versions", IDParam: "id", Short: "List a runbook's versions"},
		{Verb: "add-version", Method: "POST", Path: base + "/{id}/versions", IDParam: "id", Body: true, Short: "Publish a new runbook version"},
		{Verb: "executions", Method: "GET", Path: base + "/executions", Short: "List runbook executions (account-wide)"},
		{Verb: "execution", Method: "GET", Path: base + "/executions/{id}", IDParam: "id", Short: "Get one runbook execution"},
		{Verb: "execute", Method: "POST", Path: base + "/{id}/executions", IDParam: "id", Body: true, Short: "Trigger a runbook execution"},
	}
}

// ── Identity (plan #112) ─────────────────────────────────────────────

func humanGroupMemberActions() []Action {
	const base = "/v1/identity/humans/groups"
	return []Action{
		{Verb: "members", Method: "GET", Path: base + "/{id}/users", IDParam: "id", Short: "List a human group's active members' user IDs (needs groups:read only)"},
		{Verb: "add-user", Method: "POST", Path: base + "/{id}/users", IDParam: "id", Body: true, Short: "Add a user to a human group"},
		{Verb: "remove-user", Method: "DELETE", Path: base + "/{id}/users/{user-id}", IDParam: "id", IDParam2: "user-id", Short: "Remove a user from a human group"},
	}
}

func humanUserGroupActions() []Action {
	return []Action{
		{Verb: "groups", Method: "GET", Path: "/v1/identity/humans/users/{user-id}/groups", IDParam: "user-id", Short: "List the groups a user belongs to"},
	}
}

func accountActions() []Action {
	const base = "/v1/identity/accounts/current"
	return []Action{
		{Verb: "billing", Method: "GET", Path: base + "/billing", Short: "Read the current account's billing summary"},
		{Verb: "overview", Method: "GET", Path: base + "/overview", Short: "Read the current account's overview rollup"},
		{Verb: "refresh-overview", Method: "POST", Path: base + "/overview/refresh", Short: "Recompute the current account's overview rollup"},
	}
}

func licenseActions() []Action {
	const base = "/v1/licenses"
	return []Action{
		{Verb: "status", Method: "GET", Path: base + "/{id}", IDParam: "id", Short: "Read the locally verified status of an installed license"},
		{Verb: "install", Method: "POST", Path: base + "/install", Body: true, Short: "Verify and install a cluster-bound signed license artifact"},
		{Verb: "refresh-status", Method: "POST", Path: base + "/{id}/refresh-status", IDParam: "id", Short: "Refresh a connected license's signed status assertion"},
	}
}

func sessionRevocationActions() []Action {
	return []Action{
		{Verb: "revocations", Method: "GET", Path: "/v1/identity/humans/sessions/revocations", Short: "List human session revocations"},
	}
}

// ── Context (plan #112) ──────────────────────────────────────────────

func contextResourceLockdownActions() []Action {
	const base = "/v1/context/resources"
	return []Action{
		{Verb: "lockdown", Method: "PUT", Path: base + "/{id}/lockdown", IDParam: "id", Body: true, Short: "Restrict a resource's permitted operation verbs (lock it down)"},
		{Verb: "lift-lockdown", Method: "DELETE", Path: base + "/{id}/lockdown", IDParam: "id", Short: "Remove a resource's operation lockdown"},
	}
}

// contextResourceGovernanceActions exposes the resource ownership + approval
// quorum + manual (curated) tag overlay routes (plan #133 §1-3, backend in
// context/cx_owners.go + cx_tags.go) as catalog actions. They ship in the
// backend + the SPA but were never in the registry, so the OpenAPI contract
// (generated from here) didn't cover them — the plan #112 route audit caught
// exactly that. Registering them mirrors how every sibling Context sub-route
// (children/relationships/operations/lockdown) is exposed: a `yottactl context
// resources <verb>` command + an OpenAPI path.
//
// The GET reads set MCPExclude (same posture as children/relationships/
// operations): they stay off yotta-mcp's generated read surface so this fix
// adds no new agent-callable tools — governance/curation stays operator-driven.
// All are gated on context_resources:read (GET) / :write (PUT) at the backend.
func contextResourceGovernanceActions() []Action {
	const base = "/v1/context/resources"
	return []Action{
		{Verb: "governance", Method: "GET", Path: base + "/{id}/governance", IDParam: "id", MCPExclude: true, Short: "Read a resource's governance (owner set + approval quorum)"},
		{Verb: "set-governance", Method: "PUT", Path: base + "/{id}/governance", IDParam: "id", Body: true, Short: "Set a resource's approval quorum (approvals_required)"},
		{Verb: "set-owners", Method: "PUT", Path: base + "/{id}/owners", IDParam: "id", Body: true, Short: "Replace a resource's owner set (reconciles the quorum default)"},
		{Verb: "manual-tags", Method: "GET", Path: base + "/{id}/manual-tags", IDParam: "id", MCPExclude: true, Short: "Read a resource's manual (curated) tag overlay"},
		{Verb: "set-manual-tags", Method: "PUT", Path: base + "/{id}/manual-tags", IDParam: "id", Body: true, Short: "Set a resource's manual (curated) tag overlay"},
	}
}

// Registry is every noun yottactl exposes. Paths mirror the live
// REST routes (plan #39 grammar). Stub nouns are wired so the agent
// never hits a missing command — they return the backend's empty
// list today.
var Registry = []Noun{
	// ── Agent Platform ───────────────────────────────────────────
	{Name: "agents", Group: "Agent Platform", Path: "/v1/agent-platform/agents", IDParam: "id", Verbs: crud(), Actions: agentReadActions(), Short: "AI agents that run on the platform"},
	// Plan #244 WP2b-3: create is the canonical draft-first POST (a v2
	// definition, never published); the other verbs stay on the legacy base
	// until their canonical routes exist. The draft/lifecycle actions are the
	// canonical write path a definition change takes.
	{Name: "workflows", Group: "Agent Platform", Path: "/v1/agent-platform/workflows", IDParam: "id", Verbs: crud(),
		VerbPaths: map[Verb]string{Create: WorkflowsCanonicalPath},
		Actions:   append(workflowRunActions(), workflowDraftActions()...), Short: "Agent workflows (DAGs of steps)",
		Description: "A workflow is a graph of nodes. Its definition is canonical v2 " +
			`({"schema_version":2,"nodes":[…],"connections":{…}}); a v1 {"steps":[…]} document is no longer written. ` +
			"create makes the workflow and its editable draft together and never publishes. update sends metadata " +
			"(name, description, tags, trigger) to the workflow and a definition to its draft: it reads the draft, " +
			"writes it back conditionally, and fails rather than overwrite a draft someone else changed. Nothing runs " +
			"until publish makes the draft an immutable version; deactivate, reactivate and unpublish move the active " +
			"version. A workflow that predates drafts reports adoption_state pending (or failed) until the platform " +
			"adopts it, and its definition cannot change before then."},
	{Name: "tools", Group: "Agent Platform", Path: "/v1/agent-platform/tools", IDParam: "id", Verbs: crud(), Short: "Tool catalog"},
	{Name: "orchestrators", Group: "Agent Platform", Path: "/v1/agent-platform/orchestrators", IDParam: "id", Verbs: rc(), Short: "Agent/workflow orchestration engines",
		Description: "The orchestrator registry: the engines that coordinate an agent or workflow — Yotta Graph (the Yotta-managed default), plus customer-registered frameworks such as LangGraph and LangChain. An orchestrator is NOT where a run executes (that is a deployment target) and NOT the model behind it (that is a model / LLM gateway). Rows carry a status telling operators whether the engine can actually dispatch or is registered configure-only."},
	// Deprecated alias of `orchestrators` (plan #194). Still a working
	// yottactl command and still in the OpenAPI contract, marked deprecated;
	// withheld from the model-facing tool surface so Albus sees one name per
	// operation. Retire with the REST base in step 12.
	{Name: "runtimes", Group: "Agent Platform", Path: "/v1/agent-platform/runtimes", IDParam: "id", Verbs: rc(), Short: "Agent runtimes (deprecated — use `orchestrators`)", SupersededBy: "orchestrators"},
	{Name: "models", Group: "Agent Platform", Path: "/v1/agent-platform/models", IDParam: "id", Verbs: crud(), Actions: modelBindingActions(), Short: "Managed + customer models",
		Description: "The model registry: Anthropic/OpenAI and open-source models an agent can be bound to. Each row carries the model maker (vendor), family/version, the upstream provider adapter, the exact provider_model_id used for dispatch, and its LLM gateway binding. Provider-backed rows are populated by discovery (sync) through a Yotta LLM Gateway upstream route; the seeded catalog is a fallback."},
	// Plan #88 item 7 — tenant-managed model API credentials (BYO keys), so
	// Albus can list them for the chat/guided agent-create credential step and
	// (later) create one. Mirrors the form's models/credentials picker; the
	// backend strips secret_ref on read (json:"-").
	{Name: "model-credentials", Parent: "models", Group: "Agent Platform", Path: "/v1/agent-platform/models/credentials", IDParam: "id", Verbs: rcd(), Short: "Tenant-managed model API credentials (BYO keys)"},
	{Name: "prompts", Group: "Agent Platform", Path: "/v1/agent-platform/prompts", IDParam: "id", Verbs: crud(), Short: "Prompt catalog"},
	{Name: "mcp-gateways", Group: "Agent Platform", Path: "/v1/agent-platform/mcp-gateways", IDParam: "id", Verbs: crud(), Short: "MCP gateway catalog"},
	{Name: "guardrail-policies", Group: "Agent Platform", Path: "/v1/agent-platform/guardrail-policies", IDParam: "id", Verbs: crud(), Short: "Agent guardrail policies"},
	// LLM gateways (plan #109) — the model-inference gateway catalog, sibling to
	// mcp-gateways ("llm" is the gateway type). Full CRUD + a month-to-date usage
	// rollup read.
	{Name: "llm-gateways", Group: "Agent Platform", Path: "/v1/agent-platform/llm-gateways", IDParam: "id", Verbs: crud(), Actions: llmGatewayActions(), Short: "LLM inference gateways",
		Description: "Yotta LLM Gateways — the Yotta-owned control point that authenticates agent attestation JWTs, resolves upstream credentials through the credential broker, routes model invocations to upstream LLM providers, and enforces rate/budget policy. The gateway vendor is YottaBot; upstream providers (Anthropic, OpenAI/ChatGPT, Bedrock, …) are routes BEHIND the gateway, not the gateway vendor. A gateway's provider/endpoint/auth fields are compatibility columns for its primary upstream; the authoritative per-upstream config lives on its routes."},
	// github — actions-only (no CRUD): read a repo file via the account's
	// GitHub App credential (plan #81 item 2), so Albus can ground drafts in
	// real repo contents. GET, so the brain never confirm-gates the read.
	{Name: "github", Group: "Agent Platform", Path: "/v1/agent-platform/github", Verbs: []Verb{}, Actions: githubActions(), Short: "GitHub repo access via the account's GitHub App"},
	// Templates + configs (plan #112) — versioned agent/workflow blueprints and
	// their bound configs. Update is PUT (an action), so the CRUD verb set stops
	// at list/get/create/delete.
	{Name: "agent-templates", Group: "Agent Platform", Path: "/v1/agent-platform/agent-templates", IDParam: "id", Verbs: rcd(), Actions: agentTemplateActions(), Short: "Versioned agent templates"},
	{Name: "agent-configs", Group: "Agent Platform", Path: "/v1/agent-platform/agent-configs", IDParam: "id", Verbs: rcd(), Actions: agentConfigActions(), Short: "Agent configs (template + values)"},
	{Name: "workflow-templates", Group: "Agent Platform", Path: "/v1/agent-platform/workflow-templates", IDParam: "id", Verbs: rcd(), Actions: workflowTemplateActions(), Short: "Versioned workflow templates"},
	{Name: "workflow-configs", Group: "Agent Platform", Path: "/v1/agent-platform/workflow-configs", IDParam: "id", Verbs: rcd(), Actions: workflowConfigActions(), Short: "Workflow configs (template + values)"},
	// Installable-asset catalog (actions-only) + the account-scoped settings
	// singleton (actions-only; show/save avoid the id-param CRUD suffix guard).
	{Name: "catalog", Group: "Agent Platform", Path: "/v1/agent-platform/catalog", Verbs: []Verb{}, Actions: catalogActions(), Short: "Installable platform asset catalog"},
	{Name: "bundle", Parent: "catalog", Group: "Agent Platform", Path: "/v1/automation-platform/catalog", Verbs: []Verb{}, Actions: []Action{{Verb: "status", Method: "GET", Path: "/v1/automation-platform/catalog/status", Short: "Read stored/effective Catalog revision, inventory and loader evidence"}}, Short: "Local Catalog bundle status"},
	{Name: "agent-platform-settings", Group: "Agent Platform", Path: "/v1/agent-platform/settings", Verbs: []Verb{}, Actions: agentPlatformSettingsActions(), Short: "Agent-platform account settings"},
	// Runtime deployment targets (plan #111 §7) — promoted from the bespoke
	// CLI-only read to the full registry surface (list/create/delete + default).
	{Name: "deployment-targets", Group: "Agent Platform", Path: "/v1/agent-platform/deployment-targets", IDParam: "id", Verbs: []Verb{List, Create, Delete}, Actions: deploymentTargetActions(), Short: "Runtime deployment targets (cluster + namespace)"},

	// ── Context ──────────────────────────────────────────────────
	{Name: "resources", Parent: "context", Group: "Context", Path: "/v1/context/resources", IDParam: "id", Verbs: crud(), Actions: append(append(contextResourceActions(), contextResourceLockdownActions()...), contextResourceGovernanceActions()...), Short: "Registered resources (the universal registry)"},
	{Name: "parent-handles", Parent: "context", Group: "Context", Path: "/v1/context/parent-handles", IDParam: "id", Verbs: crud(), Actions: parentHandleActions(), Short: "Top-level discovery entry points (AWS accounts, GH orgs, ...)"},
	{Name: "graph", Parent: "context", Group: "Context", Path: "/v1/context/graph", Verbs: []Verb{}, Actions: contextGraphActions(), Short: "Bounded traversal over the resource relationship graph (blast-radius / trace-path / neighbors)"},
	// Two nouns, and the split is the point (plan #208 step 7).
	//
	// `graph-definitions` is the LENS catalog: which kinds of graph exist,
	// the classes each spans, the question it answers and what it does NOT
	// establish. Read it to choose a lens.
	//
	// `graphs` is the INVENTORY: the graphs that actually exist in this
	// account, each with a stable id. Reach for this one by default — an
	// id is a durable handle, where a hand-built traversal is a query two
	// callers can write differently while believing they mean the same
	// graph. #204 gave the plural to the catalog; the product word
	// "graphs" means the rows, so it now names them.
	{Name: "graph-definitions", Parent: "context", Group: "Context", Path: "/v1/context/graph-definitions", Verbs: []Verb{List}, Short: "Catalog of graph lenses (service / network / request) — what each answers and what it does not"},
	{Name: "graphs", Parent: "context", Group: "Context", Path: "/v1/context/graphs", IDParam: "id", Verbs: []Verb{List, Get, Create}, Actions: contextGraphInstanceActions(), Short: "The graphs that exist in this account — one row per service, VPC, or observed request path, each with a stable id"},
	{Name: "settings", Parent: "context", Group: "Context", Path: "/v1/context/settings", Verbs: []Verb{}, Actions: contextSettingsActions(), Short: "Context account settings (default discovery scrape cadence)"},
	// The `data-sources` compat noun (plan #112) is gone — plan #204 Track
	// F1 retired the cg_* graph it fronted, along with its endpoints. Do not
	// re-add it: Context's discovery entry points are `parent-handles` and
	// its registry is `resources`.

	// ── Identity ─────────────────────────────────────────────────
	// Section-scoped canonical paths (plan #65 Track C — Humans /
	// Machines / Access / Connections). Noun names are kept stable this
	// cutover; only the REST paths moved to /v1/identity/<section>/<leaf>.
	{Name: "users", Group: "Identity", Path: "/v1/identity/humans/users", IDParam: "id", Verbs: crud(), Actions: humanUserGroupActions(), Short: "Human users"},
	{Name: "groups", Group: "Identity", Path: "/v1/identity/humans/groups", IDParam: "id", Verbs: crud(), Actions: humanGroupMemberActions(), Short: "Human groups / teams"},
	{Name: "roles", Group: "Identity", Path: "/v1/identity/access/roles", IDParam: "id", Verbs: crud(), Actions: roleAttachActions(), Short: "Roles"},
	{Name: "policies", Group: "Identity", Path: "/v1/identity/access/policies", IDParam: "id", Verbs: crud(), Short: "Policies (update/delete restricted to custom; system policies immutable)"},
	{Name: "api-tokens", Group: "Identity", Path: "/v1/identity/humans/personal-access-tokens", IDParam: "id", Verbs: rcd(), Short: "Personal access tokens"},
	// Session rosters (plan #83 Step 4) — read-only lists; human sessions add a revoke-by-jti action.
	{Name: "human-sessions", Parent: "humans", Group: "Identity", Path: "/v1/identity/humans/sessions", Verbs: []Verb{List}, Actions: append(humanSessionActions(), sessionRevocationActions()...), Short: "Human login sessions (list + revoke)"},
	{Name: "service-accounts", Group: "Identity", Path: "/v1/identity/machines/identities", IDParam: "id", Verbs: crud(), Actions: serviceAccountActions(), Short: "Machine identities (service accounts)"},
	// Machines → Credentials (plan #83 Step 1) — client_credentials keypairs; no GET/{id} on the backend.
	{Name: "machine-credentials", Parent: "machines", Group: "Identity", Path: "/v1/identity/machines/credentials", IDParam: "id", Verbs: []Verb{List, Create, Delete}, Actions: machineCredentialActions(), Short: "Machine keypair credentials (client_credentials; revoke-all kill switch)"},
	{Name: "sso-providers", Group: "Identity", Path: "/v1/identity/connections/identity-providers", IDParam: "id", Verbs: crud(), Short: "Identity providers (SSO)"},
	// Principal substrate read surface — identity_principals view, agent axis (plan #65 Track D).
	{Name: "agent-identities", Parent: "agents", Group: "Identity", Path: "/v1/identity/agents/identities", Verbs: []Verb{List}, Short: "Agent principals (identity_principals view, kind=agent)"},
	// Typed groups (plan #83 Step 3) — full backend CRUD + member add/remove
	// (POST/DELETE .../{id}/members/{principalId}) via the two-positional-arg
	// action form added in Step 2b.
	{Name: "agent-groups", Parent: "agents", Group: "Identity", Path: "/v1/identity/agents/groups", IDParam: "id", Verbs: crud(), Actions: groupMemberActions("/v1/identity/agents/groups"), Short: "Agent groups / teams"},
	{Name: "machine-groups", Parent: "machines", Group: "Identity", Path: "/v1/identity/machines/groups", IDParam: "id", Verbs: crud(), Actions: groupMemberActions("/v1/identity/machines/groups"), Short: "Machine groups / teams"},
	// Agent/machine sessions + agent credentials (plan #83 Steps 4/5) — read-only projections.
	{Name: "agent-sessions", Parent: "agents", Group: "Identity", Path: "/v1/identity/agents/sessions", Verbs: []Verb{List}, Short: "Agent attestation sessions"},
	{Name: "agent-credentials", Parent: "agents", Group: "Identity", Path: "/v1/identity/agents/credentials", Verbs: []Verb{List}, Short: "Agent credentials (system-minted; read-only)"},
	{Name: "machine-sessions", Parent: "machines", Group: "Identity", Path: "/v1/identity/machines/sessions", Verbs: []Verb{List}, Short: "Machine sessions"},
	// Resource authorization owned by Identity (plan #69 Slice 1 / ADR 0016).
	{Name: "resource-grants", Parent: "access", Group: "Identity", Path: "/v1/identity/access/resource-grants", IDParam: "id", Verbs: rcd(), Short: "Principal → Context-resource grants (read|mutate verb class)"},
	// Access → Assignments (plan #83 Step 2a) — principal/group → role/policy; backend list/create/delete only.
	{Name: "assignments", Parent: "access", Group: "Identity", Path: "/v1/identity/access/assignments", IDParam: "id", Verbs: []Verb{List, Create, Delete}, Short: "Principal/group → role/policy assignments"},
	{Name: "approvals", Parent: "governance", Group: "Identity", Path: "/v1/identity/governance/approvals", Verbs: []Verb{List}, Actions: approvalDecisionActions(), Short: "Approval queue (list + approve/reject/cancel)"},
	// Governance → Audit + Events — append-only filter-views over identity_audit_entries (plan #65 Track E).
	{Name: "audit", Parent: "governance", Group: "Identity", Path: "/v1/identity/governance/audit", Verbs: []Verb{List}, Short: "Identity audit log (append-only; no delete)"},
	{Name: "events", Parent: "governance", Group: "Identity", Path: "/v1/identity/governance/events", Verbs: []Verb{List}, Short: "Identity activity events (filter-view over the audit substrate)"},
	// Permissions catalog (read-only) + the current-account rollups (plan #112).
	{Name: "permissions", Parent: "access", Group: "Identity", Path: "/v1/identity/permissions", Verbs: []Verb{List}, Short: "Permission catalog (all grantable permission strings)"},
	{Name: "account", Group: "Identity", Path: "/v1/identity/accounts/current", Verbs: []Verb{}, Actions: accountActions(), Short: "Current account billing + overview rollups"},
	// Plan #220 Step 9 — cluster-local customer entitlement surface. The
	// command stays available to operators while the disabled chart is staged,
	// but it is withheld from the model-facing catalog until the Service ships.
	{Name: "licenses", Group: "Account", Path: "/v1/licenses", IDParam: "id", Verbs: listGet(), Actions: licenseActions(), Short: "cluster-bound YottaBot licenses", Unshipped: true},

	// ── Infrastructure (KMS / Secrets / Blob / Artifacts) ────────
	// Plan #124 — Yotta Keys unified key records. `update` (PATCH) carries
	// the lifecycle + artifact-signing metadata a key acquires after
	// creation: rotation state, the OpenPGP public certificate, and its
	// fingerprint. The release-signing flow registers a certificate through
	// this verb, so it has to be reachable from yottactl, not just the SPA.
	{Name: "keys", Parent: "kms", Group: "Infrastructure", Path: "/v1/kms/keys", IDParam: "id", Verbs: rcu(), Actions: keyRecordActions(), Short: "Yotta Keys unified key records"},
	// YottaKeys/KMS providers — shared destinations surface mounted at the bare
	// /v1/providers path (no kms/ prefix; plan #112 confirmed the live mount).
	{Name: "providers", Parent: "kms", Group: "Infrastructure", Path: "/v1/providers", IDParam: "id", Verbs: rcd(), Actions: providerActions(), Short: "Yotta Keys key providers"},
	{Name: "secrets", Group: "Infrastructure", Path: "/v1/secrets-manager/secrets", IDParam: "id", Verbs: crud(), Actions: secretsActions(), Short: "Secrets"},
	{Name: "buckets", Parent: "blob", Group: "Infrastructure", Path: "/v1/blob-storage/buckets", Verbs: []Verb{List, Create}, Short: "Blob storage buckets"},
	{Name: "objects", Parent: "blob", Group: "Infrastructure", Path: "/v1/blob-storage/objects", Verbs: []Verb{List, Create}, Short: "Blob storage objects"},
	{Name: "repos", Parent: "artifacts", Group: "Infrastructure", Path: "/v1/artifact-registry/repos", Verbs: []Verb{List, Create}, Short: "Artifact repositories (local)"},
	{Name: "repo-proxies", Parent: "artifacts", Group: "Infrastructure", Path: "/v1/artifact-registry/repos/proxies", Verbs: []Verb{List, Create}, Short: "Artifact proxy repositories"},
	{Name: "repo-virtuals", Parent: "artifacts", Group: "Infrastructure", Path: "/v1/artifact-registry/repos/virtuals", Verbs: []Verb{List, Create}, Short: "Artifact virtual repositories"},

	// ── Observability (Logs / Metrics / Alerts) ──────────────────
	{Name: "logs", Group: "Observability", Path: "/v1/logs-manager/logs", IDParam: "id", Verbs: listGet(), Short: "Log events"},
	// Events — Logs' Events page, Splunk's search (plan 291): one read of the
	// page of rows, their total, the timeline and the fields, for ?search=
	// (field=value, field!=value, "a phrase", words, * anywhere), ?from= and
	// ?to= on logged time, and the page.
	{Name: "events", Group: "Observability", Path: "/v1/logs-manager/events", Verbs: []Verb{List}, Short: "Search events, with their timeline and fields"},
	// Traces — the Logs product's correlated view (plan #203 A2). One row per
	// span tree: `category=agent` for a run/turn, `category=request` for an
	// inbound /v1 call. Registered here rather than under agents because the
	// surface belongs to Logs; the old /v1/agent-platform/traces path is
	// deprecated and deliberately NOT advertised.
	{Name: "traces", Group: "Observability", Path: "/v1/logs-manager/traces", IDParam: "id", Verbs: listGet(), Short: "Traces"},
	// log-streams retired with the log_streams catalog (plan #188 A9): the
	// table held zero rows on every environment — the per-account seed migration
	// 035 promised never shipped — so the CRUD verbs operated on nothing.
	// agent_logs.stream (the per-row SOURCE TAG, a different thing) survives
	// and is filterable via `yottactl logs list --stream=albus`.
	{Name: "alerts", Group: "Observability", Path: "/v1/alerts-manager/alerts", IDParam: "id", Verbs: crud(), Actions: alertsActions(), Short: "Alerts"},
	{Name: "alert-users", Group: "Observability", Path: "/v1/alerts-manager/users", IDParam: "userId", Verbs: rcd(), Actions: alertUsersActions(), Short: "Alerts users: who can be paged, their contact methods and notification rules (plan 318)"},
	{Name: "alert-schedules", Group: "Observability", Path: "/v1/alerts-manager/schedules", IDParam: "id", Verbs: crud(), Actions: alertSchedulesActions(), Short: "On-call schedules (plan 318)"},
	{Name: "alert-escalation-policies", Group: "Observability", Path: "/v1/alerts-manager/escalation-policies", IDParam: "id", Verbs: rcd(), Actions: alertEscalationActions(), Short: "Escalation policies: whom to page, in what order (plan 318)"},
	{Name: "alert-routes", Group: "Observability", Path: "/v1/alerts-manager/routes", Verbs: []Verb{List}, Actions: alertRoutesActions(), Short: "Routes: which escalation policy an alert pages, by its labels (plan 318)"},
	{Name: "alert-incidents", Group: "Observability", Path: "/v1/alerts-manager/incidents", IDParam: "id", Verbs: listGet(), Actions: alertIncidentsActions(), Short: "Incidents alerts were grouped into (SRE's; plan 318)"},
	{Name: "metrics", Group: "Observability", Path: "/v1/metrics-manager/metrics/agents", Verbs: []Verb{List}, Actions: metricsReadActions(), Short: "Agent metrics"},
	{Name: "dashboards", Group: "Observability", Path: "/v1/metrics-manager/dashboards", IDParam: "id", Verbs: listGet(), Short: "Metrics dashboards"},
	// Telemetry destinations + routing (plan #39 shared surface). Only
	// metrics is registered: logs-manager + alerts-manager mount the shared
	// destinations surface at bare, colliding `/v1/destinations*` paths (not
	// service-prefixed), and the SPA calls `/v1/metrics-manager/destinations`
	// on all three services — a live REST-shape bug that #112 (contract
	// hardening) must not fix by changing handler behavior. Registering the
	// logs/alerts destinations waits on the backend+frontend path fix in the
	// follow-up plan; see #112 "Remaining work".
	// Telemetry destinations left the per-product surfaces with plan #11 step
	// 11.1 (migration bot/235): the three tables are dropped and replaced by
	// ONE account-level telemetry_destinations registry. Advertising the old
	// path would hand Albus and yottactl users a call against a table that no
	// longer exists. Re-register at the new path when 11.1's CRUD lands.

	// ── SRE Platform ─────────────────────────────────────────────
	{Name: "incidents", Parent: "sre", Group: "SRE Platform", Path: "/v1/sre-platform/incidents", IDParam: "id", Verbs: crud(), Actions: append(incidentsActions(), incidentInvestigationActions()...), Short: "Incidents"},
	{Name: "investigations", Parent: "sre", Group: "SRE Platform", Path: "/v1/sre-platform/incidents/investigations", IDParam: "id", Verbs: []Verb{List, Get, Update, Delete}, Actions: investigationActions(), Short: "Investigations"},
	{Name: "findings", Parent: "sre", Group: "SRE Platform", Path: "/v1/sre-platform/findings", IDParam: "id", Verbs: []Verb{List, Get, Create, Update}, Actions: findingsActions(), Short: "Investigation findings"},
	{Name: "skill-runs", Parent: "sre", Group: "SRE Platform", Path: "/v1/sre-platform/skill-runs", Verbs: []Verb{List}, Short: "SRE skill runs (read-only)"},
	{Name: "runbooks", Parent: "sre", Group: "SRE Platform", Path: "/v1/sre-platform/runbooks", IDParam: "id", Verbs: []Verb{List, Get, Create, Update}, Actions: runbookActions(), Short: "Runbooks"},
	{Name: "postmortems", Parent: "sre", Group: "SRE Platform", Path: "/v1/sre-platform/postmortems", IDParam: "id", Verbs: []Verb{List, Get, Create, Update}, Actions: postmortemActions(), Short: "Post-mortems"},

	// ── Security Platform (plan #125) ────────────────────────────
	{Name: "investigations", Parent: "security", Group: "Security Platform", Path: "/v1/security-platform/investigations", IDParam: "id", Verbs: []Verb{List, Get, Update}, Actions: securityInvestigationActions(), Description: "Security investigations (plan #125): multi-hop, Context-grounded, evidence-backed. Opened for an Issues incident via `launch`; hops are appended by the investigation workflow. Read-only over customer resources.", Short: "Security investigations"},
	{Name: "findings", Parent: "security", Group: "Security Platform", Path: "/v1/security-platform/findings", IDParam: "id", Verbs: []Verb{List, Get, Create, Update}, Actions: securityFindingsActions(), Description: "Security findings (plan #125): first-class records with severity/confidence/status, affected Context resources, evidence citations, and a recommended remediation. Optional markdown body via `content`. Sourced from an investigation or assessment run.", Short: "Security findings"},
	{Name: "assessments", Parent: "security", Group: "Security Platform", Path: "/v1/security-platform/assessments", IDParam: "id", Verbs: []Verb{List, Get, Create, Update}, Actions: securityAssessmentActions(), Description: "Governed security assessments (plan #125): pen_test | threat_analysis. Passive-by-default — active mode requires an approval in policy. `run` starts a run; runs are stoppable.", Short: "Security assessments (pen test / threat analysis)"},

	// ── Compliance Platform (plan #127) ──────────────────────────
	{Name: "frameworks", Parent: "compliance", Group: "Compliance Platform", Path: "/v1/compliance-platform/frameworks", IDParam: "id", Verbs: []Verb{List}, Actions: complianceFrameworkActions(), Description: "Compliance framework packs (plan #127): SOC 2, ISO 27001, HIPAA, PCI DSS, GDPR, NIST 800-53, CIS, NIST CSF — Yotta-seeded catalog rows (bot/205). `enable` flips a pack on for the account. Evidence support only — not a certification guarantee.", Short: "Compliance framework packs"},
	{Name: "controls", Parent: "compliance", Group: "Compliance Platform", Path: "/v1/compliance-platform/controls", IDParam: "id", Verbs: []Verb{List, Get}, Description: "Normalized compliance controls (plan #127): the internal unit of work framework requirements map onto. `get` includes the control's framework mappings (external refs + mapping strength); `list` accepts ?framework_id= to scope to one pack.", Short: "Normalized controls (mappings on get)"},
	{Name: "audits", Parent: "compliance", Group: "Compliance Platform", Path: "/v1/compliance-platform/audits", IDParam: "id", Verbs: []Verb{List, Get, Create, Update}, Actions: complianceAuditActions(), Description: "Compliance audits (plan #127): a program run over a framework + period (planning → collecting → review → ready → submitted → closed). Evidence runs and items link back to the audit; `export` builds an immutable auditor-ready evidence packet.", Short: "Compliance audits"},
	{Name: "evidence-tasks", Parent: "compliance", Group: "Compliance Platform", Path: "/v1/compliance-platform/evidence-tasks", IDParam: "id", Verbs: []Verb{List, Get, Create, Update}, Actions: complianceEvidenceTaskActions(), Description: "Compliance evidence tasks (plan #127): what proof to collect for a control (source kind, scope, policy). `run` records an evidence run and launches the managed collection workflow (grant-at-launch; degrades to queued + launch_note on un-bootstrapped accounts).", Short: "Evidence tasks (what proof to collect)"},
	{Name: "schedules", Parent: "compliance", Group: "Compliance Platform", Path: "/v1/compliance-platform/schedules", IDParam: "id", Verbs: []Verb{List, Get, Create, Update}, Actions: complianceScheduleActions(), Description: "Compliance evidence schedules (plan #127 step 7): keep evidence fresh on a cadence (daily|weekly|monthly|quarterly|annual) with owner + reviewer. status covers pause/resume/disable; `run-now` records + launches a collection run without moving the cadence. The in-service sweeper records due runs and raises stale-evidence alerts.", Short: "Evidence schedules (keep evidence fresh)"},
	{Name: "evidence-runs", Parent: "compliance", Group: "Compliance Platform", Path: "/v1/compliance-platform/evidence-runs", IDParam: "id", Verbs: []Verb{List, Get}, Actions: complianceEvidenceRunActions(), Description: "Compliance evidence collection runs (plan #127): one collection execution (queued → running → completed|failed|canceled|needs_review). Stoppable; the managed workflow's terminal step completes them.", Short: "Evidence collection runs"},
	{Name: "evidence", Parent: "compliance", Group: "Compliance Platform", Path: "/v1/compliance-platform/evidence", IDParam: "id", Verbs: []Verb{List, Get, Create}, Actions: complianceEvidenceActions(), Description: "Compliance evidence items (plan #127): the collected, cited proof — framework/control mapping, collection period, source, provenance, citations, reviewer state. `review` records an accept/reject decision.", Short: "Evidence items (collected, cited proof)"},
	{Name: "findings", Parent: "compliance", Group: "Compliance Platform", Path: "/v1/compliance-platform/findings", IDParam: "id", Verbs: []Verb{List, Get, Create, Update}, Actions: complianceFindingActions(), Description: "Compliance findings (plan #127 step 8): gaps — missing/stale evidence, failed evidence tasks, control failures — with severity/status/confidence, evidence citations, and a recommendation. `create-issue` opens the optional Issues remediation ticket (COMP project, linked via issue_id — Compliance never grows a second ticket tracker); `launch-investigation` starts the managed gap investigation.", Short: "Compliance findings (gaps)"},
	{Name: "investigations", Parent: "compliance", Group: "Compliance Platform", Path: "/v1/compliance-platform/investigations", IDParam: "id", Verbs: []Verb{List, Get}, Actions: complianceInvestigationActions(), Description: "Compliance gap investigations (plan #127 step 8): explain WHY a gap exists. Launched from a finding; the managed workflow writes citeable hops, updates the finding, and completes the investigation. Read-only over customer resources.", Short: "Gap investigations (why a gap exists)"},
	{Name: "exports", Parent: "compliance", Group: "Compliance Platform", Path: "/v1/compliance-platform/exports", IDParam: "id", Verbs: []Verb{List, Get}, Actions: complianceExportActions(), Description: "Compliance evidence exports (plan #127 step 9): immutable auditor-ready snapshots (zip|json|markdown) with a manifest + per-file sha256 checksums. Built from an audit via `compliance audits export`; packages expire per retention policy — build a new export for a fresh snapshot.", Short: "Evidence exports (immutable packets)"},

	// ── Trading Platform (plan #200) ─────────────────────────────
	//
	// Registered under a `trading` parent so the CLI reads
	// `yottactl trading backtests launch`. Phase A's whole premise is that the
	// API is the primary interface — an agent making one call — so these rows
	// are what make the loop drivable conversationally as well as from a shell.
	//
	// The trading nouns are on the model's catalog because their Service and
	// the /v1/trading gateway route are deployed. The route and an Unshipped
	// flag move together: TestUnshipped_NoGatewayRoute fails a route added
	// ahead of the deploy, and the unshipped-ledger test fails a flag left set
	// after one.
	{Name: "strategies", Parent: "trading", Group: "Trading Platform", Path: "/v1/trading/strategies", IDParam: "id", Verbs: []Verb{List, Get, Create, Update}, Actions: tradingStrategyActions(), Short: "Trading strategies and their immutable versions",
		Description: "The strategy registry. A strategy is the object a user starts from; each of its versions pins a digest-pinned container image, a JSON Schema for that version's parameters, and the engine SDK version it was built against. There is no delete: a version may already have runs whose provenance points at it."},
	{Name: "backtests", Parent: "trading", Group: "Trading Platform", Path: "/v1/trading/backtests", IDParam: "id", Verbs: []Verb{List, Get, Create}, Actions: tradingBacktestActions(), Short: "Backtest runs (launch, control, observe)",
		Description: "A backtest run replays historical market data against a strategy, one Kubernetes Job per (symbol x session-day) shard. Create is the launch: it names a strategy version, a dataset, a deployment target, a time range, symbols, and explicit latency / fill-realism / fee parameters, and it REJECTS rather than degrades — a fill mode the dataset cannot support, an unpriced fee tier, or a missing source object each fail the launch instead of producing a plausible wrong number."},
	{Name: "datasets", Parent: "trading", Group: "Trading Platform", Path: "/v1/trading/datasets", IDParam: "id", Verbs: []Verb{List, Get, Create, Update}, Short: "Historical market-data datasets",
		Description: "A dataset row is not a pointer to files: it declares what its bytes MEAN — exact schema, timestamp semantics, sequence-gap policy, and capability flags. The launch validator reads that rather than inferring fidelity from filenames, so a high-fidelity fill mode is refused on data that cannot support it. Each row also reports the fill modes it can honestly run."},
	{Name: "fee-schedules", Parent: "trading", Group: "Trading Platform", Path: "/v1/trading/fee-schedules", IDParam: "id", Verbs: []Verb{List, Get, Create, Update}, Actions: tradingFeeScheduleActions(), Short: "Broker/venue fee schedules and their effective-dated rates",
		Description: "A schedule is a named container; the rates live in effective-dated component rows, because one published rate card carries components with different effective dates and a year-long backtest crosses them. `covers` distinguishes UNSET from zero: a broker schedule that says nothing about exchange maker rebates is not one where the rebate is zero, and a passive run under it is refused a net-of-fees figure."},
	{Name: "latency-profiles", Parent: "trading", Group: "Trading Platform", Path: "/v1/trading/latency-profiles", IDParam: "id", Verbs: []Verb{List, Get, Create, Update}, Actions: tradingLatencyProfileActions(), Short: "Named latency profiles (feed, decision, order-entry, order-response)",
		Description: "The three-leg latency model a run is simulated under, stored split: feed latency decides what book the strategy sees, order-entry latency decides where it lands in the queue, and order-response latency decides when it can react. Each profile records its provenance — assumed, vendor_quoted, or measured — and a profile claiming `measured` is refused against a dataset with no receive timestamp to measure against."},

	// ── Drone Platform ───────────────────────────────────────────
	{Name: "drones", Parent: "drone", Group: "Drone Platform", Path: "/v1/drone-platform/drones", IDParam: "id", Verbs: crud(), Short: "Drones"},
	{Name: "pilots", Parent: "drone", Group: "Drone Platform", Path: "/v1/drone-platform/pilots", IDParam: "id", Verbs: crud(), Short: "Pilots"},
	{Name: "workflows", Parent: "drone", Group: "Drone Platform", Path: "/v1/drone-platform/workflows", IDParam: "id", Verbs: crud(), Short: "Drone workflows"},

	// ── Issues ───────────────────────────────────────────────────
	{Name: "backlog", Parent: "issues", Group: "Issues", Path: "/v1/issues/backlog", IDParam: "id", Verbs: []Verb{List, Get, Create, Update}, Actions: issuesActions(), Short: "Backlog items and plan-derived work (plan #73 compat surface)"},
	{Name: "issue-projects", Parent: "issues", Group: "Issues", Path: "/v1/issues/projects", IDParam: "id", Verbs: []Verb{List, Get, Create, Update}, Actions: issueProjectsActions(), Description: "Issues projects (plan #104): the container every ticket belongs to; its key seeds human ticket keys (PLAT-1).", Short: "Issues projects"},
	{Name: "tickets", Parent: "issues", Group: "Issues", Path: "/v1/issues/tickets", IDParam: "id", Verbs: []Verb{List, Get, Create, Update}, Actions: ticketsActions(), Description: "Issues tickets (plan #104): typed work items (epic/story/task/bug/incident) with status machine, epic hierarchy, links, comments, activity timeline, and Roadmap. List filters: project_id, project_key, type, status, priority, severity, tag, assignee_user_id, parent_issue_id, q, updated_since. get accepts an id or an issue key.", Short: "Issues tickets (typed work items)"},
	{Name: "documents", Parent: "documents", Group: "Documents", Path: "/v1/documents/docs", IDParam: "id", Verbs: []Verb{List, Get, Create, Update}, Actions: documentsActions(), Short: "Markdown documents (directory tree, versions, search)"},
	{Name: "document-directories", Parent: "documents", Group: "Documents", Path: "/v1/documents/directories", IDParam: "id", Verbs: []Verb{Get, Create}, Actions: documentDirectoriesActions(), Short: "Document directory tree"},

	// ── Yotta Turbo (plan #146) ────────────────────────────────────
	{Name: "collections", Parent: "yottaturbo", Group: "Yotta Turbo", Path: "/v1/yottaturbo/collections", IDParam: "id", Verbs: []Verb{List, Get, Create, Delete}, Actions: yottaturboCollectionsActions(), Description: "Yotta Turbo retrieval collections (plan #146): governed compressed vector indexes. Create pins the embedding model/dim/metric/backend (postgres_jsonb default; pgvector/turbovec plug in behind the same contract). Items carry a stable numeric_id bridge; query runs account-scoped, allowlist-filtered nearest-neighbor search.", Short: "Yotta Turbo retrieval collections (governed vector index)", Unshipped: true},

	// ── Apply / Generate (Track F — post-MVP-01 stubs) ───────────
	{Name: "playbooks", Group: "Apply/Generate", Path: "/v1/migration-playbooks", IDParam: "playbook_id", Verbs: listGet(), Short: "Migration playbooks", Stub: true, Unshipped: true},
}
