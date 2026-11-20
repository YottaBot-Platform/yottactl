package command

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/YottaBot-Platform/yottactl/internal/client"
	"github.com/YottaBot-Platform/yottactl/internal/spec"
	"github.com/spf13/cobra"
)

// ── workflows update / publish (plan #244 WP2b-3) ────────────────────
//
// A workflow's definition lives in its canonical draft; the workflows row
// keeps a mirror only so legacy readers keep working. So `workflows update`
// is no longer one PATCH:
//
//   - metadata (name, description, tags, trigger, status, …) stays on the
//     legacy PATCH, unchanged;
//   - a definition must be canonical v2, and goes to the draft: GET it for its
//     ETag, PUT it back with If-Match. A draft someone changed in between is a
//     conflict, never an overwrite;
//   - metadata sent with a definition is written only after the draft write
//     succeeds, so a missing or stale draft leaves the workflow untouched;
//   - nothing publishes implicitly: `workflows publish` is its own command.
//
// A workflow that predates drafts reports adoption_state until the platform
// adopts it. Its definition cannot change before then, and this command says
// so instead of falling back to the legacy definition PATCH: a supported
// client writing the old way would keep the cutover's evidence from ever
// reading zero. Albus shells to this same command, so it gets the same rules.
//
// workflows.update and workflows.publish stay registry-declared (catalog,
// OpenAPI); only their cobra commands are bespoke, like `workflows run`.

var (
	errUpdateBodyNotObject = errors.New("the update body must be a JSON object")
	// A lifecycle change is a separate, differently-authorized step; bundling
	// it with a definition would mean activating something nobody published.
	errDefinitionWithStatus = errors.New("a definition and a status change cannot go in one update: " +
		"update the draft first, then run `yottactl workflows publish|deactivate|reactivate|unpublish <id>`")
	errDefinitionWithReason = errors.New("change_reason belongs to the audited legacy repair PATCH " +
		"(.ai/runbooks/workflow-lift-preconditions.md); a draft write carries no reason")
	errDefinitionNotV2 = errors.New(`a workflow definition is canonical v2 ({"schema_version":2,"nodes":[…],"connections":{…}}); ` +
		`a v1 {"steps":[…]} document is no longer written`)
)

// workflowUpdate is what one update body asks for.
type workflowUpdate struct {
	// definition is the new draft document; nil leaves the draft alone.
	definition json.RawMessage
	// metadata is every other member, for the legacy PATCH.
	metadata map[string]any
}

// planWorkflowUpdate splits an update body, refusing — before any request —
// what the draft/publish model cannot carry out as asked.
func planWorkflowUpdate(body any) (workflowUpdate, error) {
	members, ok := body.(map[string]any)
	if !ok {
		return workflowUpdate{}, errUpdateBodyNotObject
	}
	u := workflowUpdate{metadata: map[string]any{}}
	for k, v := range members {
		if k != "definition" {
			u.metadata[k] = v
			continue
		}
		if v == nil {
			// null leaves the definition unchanged, as the PATCH treats it.
			continue
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return workflowUpdate{}, fmt.Errorf("definition: %w", err)
		}
		u.definition = raw
	}
	if u.definition == nil {
		return u, nil
	}
	if status, ok := u.metadata["status"]; ok && status != nil && status != "" {
		return workflowUpdate{}, errDefinitionWithStatus
	}
	if _, ok := u.metadata["change_reason"]; ok {
		return workflowUpdate{}, errDefinitionWithReason
	}
	if !isCanonicalDefinition(u.definition) {
		return workflowUpdate{}, errDefinitionNotV2
	}
	return u, nil
}

// isCanonicalDefinition applies the server's own test for a graph document:
// schema_version 2 or later, or nodes. Anything else is v1 or not a workflow.
func isCanonicalDefinition(raw json.RawMessage) bool {
	var probe struct {
		SchemaVersion int             `json:"schema_version"`
		Nodes         json.RawMessage `json:"nodes"`
	}
	if json.Unmarshal(raw, &probe) != nil {
		return false
	}
	return probe.SchemaVersion >= 2 || (len(probe.Nodes) > 0 && string(probe.Nodes) != "null")
}

func workflowPath(id string) string { return workflowsLegacyPath + "/" + url.PathEscape(id) }
func draftPath(id string) string {
	return spec.WorkflowsCanonicalPath + "/" + url.PathEscape(id) + "/draft"
}

// workflowsLegacyPath is the workflows noun's base: list/get/update/delete
// still live there.
const workflowsLegacyPath = "/v1/agent-platform/workflows"

// errNotAdopted is the local answer to a draft 404 that carries an adoption
// state: the workflow exists but has no draft to write yet.
func errNotAdopted(id string, body []byte) *ExitError {
	var st struct {
		State  string `json:"adoption_state"`
		Reason string `json:"adoption_reason"`
	}
	if json.Unmarshal(body, &st) != nil || st.State == "" {
		return nil
	}
	detail := "adoption_state=" + st.State
	if st.Reason != "" {
		detail += ", adoption_reason=" + st.Reason
	}
	return &ExitError{Code: 5, Msg: fmt.Sprintf("workflow %s has no canonical draft yet (%s): its definition "+
		"cannot change until the platform adopts it, and nothing was written. A metadata-only update still works.", id, detail)}
}

// readDraft GETs a workflow's draft for its ETag, or says why there is none.
func readDraft(env *Env, api *client.Client, id string) (client.Response, error) {
	got, err := api.DoResponse(http.MethodGet, draftPath(id), nil, nil)
	if err != nil {
		return got, &ExitError{Code: 1, Msg: err.Error()}
	}
	if got.Status == http.StatusNotFound {
		if e := errNotAdopted(id, got.Body); e != nil {
			return got, e
		}
	}
	if got.Status != http.StatusOK {
		return got, env.finish(got.Status, got.Body)
	}
	if got.Header.Get("ETag") == "" {
		return got, &ExitError{Code: 1, Msg: "the draft read returned no ETag, so it cannot be written or published safely; nothing was written"}
	}
	return got, nil
}

func runWorkflowUpdate(env *Env, api *client.Client, id string, u workflowUpdate) error {
	if u.definition == nil {
		resp, err := api.DoResponse(http.MethodPatch, workflowPath(id), u.metadata, nil)
		if err != nil {
			return &ExitError{Code: 1, Msg: err.Error()}
		}
		return env.finish(resp.Status, resp.Body)
	}

	ifMatch, err := env.controlHeader("If-Match", "")
	if err != nil {
		return err
	}
	got, err := readDraft(env, api, id)
	if err != nil {
		return err
	}
	if ifMatch == "" {
		ifMatch = got.Header.Get("ETag")
	}
	put, err := api.DoResponse(http.MethodPut, draftPath(id),
		map[string]any{"definition": u.definition}, map[string]string{"If-Match": ifMatch})
	if err != nil {
		return &ExitError{Code: 1, Msg: err.Error()}
	}
	if put.Status == http.StatusPreconditionFailed {
		_ = env.Render(put.Body)
		return &ExitError{Code: 5, Msg: "the draft changed after it was read (HTTP 412), so nothing was written; " +
			"read it again (`yottactl workflows draft " + id + "`) and reapply the change"}
	}
	if put.Status != http.StatusOK {
		return env.finish(put.Status, put.Body)
	}
	etag := put.Header.Get("ETag")
	result := map[string]json.RawMessage{"draft": withETag(put.Body, etag)}

	if len(u.metadata) > 0 {
		patch, err := api.DoResponse(http.MethodPatch, workflowPath(id), u.metadata, nil)
		if err != nil {
			return &ExitError{Code: 1, Msg: fmt.Sprintf("the draft was written (%s), but the metadata update failed: %v", etag, err)}
		}
		if patch.Status < 200 || patch.Status >= 300 {
			_ = env.Render(patch.Body)
			code := exitForStatus(patch.Status)
			if code == 0 {
				code = 1
			}
			return &ExitError{Code: code, Msg: fmt.Sprintf("the draft was written (%s), but the metadata update failed: HTTP %d", etag, patch.Status)}
		}
		result["workflow"] = patch.Body
	}
	out, err := json.Marshal(result)
	if err != nil {
		return &ExitError{Code: 1, Msg: err.Error()}
	}
	return env.finish(http.StatusOK, out)
}

// withETag adds the ETag to a draft response, so a caller holds the exact
// revision it wrote — what `workflows publish --if-match` takes.
func withETag(body []byte, etag string) json.RawMessage {
	var obj map[string]json.RawMessage
	if json.Unmarshal(body, &obj) != nil || obj == nil {
		return body
	}
	b, _ := json.Marshal(etag)
	obj["etag"] = b
	out, err := json.Marshal(obj)
	if err != nil {
		return body
	}
	return out
}

func newWorkflowsUpdateCmd(env *Env) *cobra.Command {
	var file string
	c := &cobra.Command{
		Use:   "update <id> -f <file>",
		Short: "Update a workflow (metadata on the workflow, a definition on its draft)",
		Long: "Update a workflow.\n\n" +
			"Metadata — name, description, tags, trigger, status — goes to the workflow\n" +
			"(PATCH /v1/agent-platform/workflows/{id}). A definition goes to the workflow's\n" +
			"draft: this command reads the draft and writes it back with If-Match, so a\n" +
			"draft someone else changed in between is refused (exit 5), never overwritten.\n" +
			"Pass --header If-Match=<etag> to require the revision you already reviewed.\n" +
			"Metadata sent together with a definition is written only after the draft.\n\n" +
			"The definition must be canonical v2 ({schema_version: 2, nodes, connections}).\n" +
			"Nothing is published: run `yottactl workflows publish <id>` next. A definition\n" +
			"cannot be combined with a status change, and a workflow that has not been\n" +
			"adopted yet (adoption_state pending or failed) cannot have its definition\n" +
			"changed; both are refused before anything is sent.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := readBody(file)
			if err != nil {
				return &ExitError{Code: 4, Msg: err.Error()}
			}
			u, err := planWorkflowUpdate(body)
			if err != nil {
				return &ExitError{Code: 4, Msg: err.Error()}
			}
			api, err := env.Client()
			if err != nil {
				return &ExitError{Code: 2, Msg: err.Error()}
			}
			return runWorkflowUpdate(env, api, args[0], u)
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "", "JSON/YAML file with the update body")
	return c
}

func newWorkflowsPublishCmd(env *Env) *cobra.Command {
	var ifMatch, reason, key, file string
	c := &cobra.Command{
		Use:   "publish <id>",
		Short: "Publish the draft as a new immutable version",
		Long: "Publish the workflow's draft as a new immutable version and make it the one\n" +
			"that runs (POST /v1/automation-platform/workflows/{id}/publish).\n\n" +
			"Publishing is conditional on the draft revision. By default this command\n" +
			"publishes the draft as it reads it now; pass --if-match with the ETag that\n" +
			"`workflows update` printed (or draft-<revision>) to publish exactly the\n" +
			"revision you reviewed — a draft changed since then is refused (exit 5).\n\n" +
			"Each run sends a fresh Idempotency-Key. To make a retry safe after a lost\n" +
			"response, pass the same --idempotency-key both times. The global --header\n" +
			"also accepts Idempotency-Key and If-Match; conflicting values are refused.\n\n" +
			"-f takes the same choices as a body — {\"reason\": \"…\", \"if_match\": \"…\"}, both\n" +
			"optional — which is how the tool catalog, and so Albus, passes them. A flag\n" +
			"given as well wins.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if file != "" {
				fromFile, err := readPublishBody(file)
				if err != nil {
					return &ExitError{Code: 4, Msg: err.Error()}
				}
				if !cmd.Flags().Changed("reason") {
					reason = fromFile.Reason
				}
				if !cmd.Flags().Changed("if-match") {
					ifMatch = fromFile.IfMatch
				}
			}
			var err error
			if ifMatch, err = env.controlHeader("If-Match", ifMatch); err != nil {
				return err
			}
			if key, err = env.controlHeader("Idempotency-Key", key); err != nil {
				return err
			}
			api, err := env.Client()
			if err != nil {
				return &ExitError{Code: 2, Msg: err.Error()}
			}
			return runWorkflowPublish(env, api, args[0], ifMatch, reason, key)
		},
	}
	c.Flags().StringVar(&ifMatch, "if-match", "", "publish only this draft revision: the ETag `workflows update` printed, or draft-<revision>")
	c.Flags().StringVar(&reason, "reason", "", "why this version is published (recorded in the publish history)")
	c.Flags().StringVar(&key, "idempotency-key", "", "reuse a key to make a retried publish return the first result")
	c.Flags().StringVarP(&file, "file", "f", "", `JSON/YAML body: {"reason": "…", "if_match": "…"}, both optional`)
	return c
}

// publishBody is what `workflows publish -f` accepts. The tool catalog
// advertises publish with a body because a catalog tool can pass a body but not
// flags; without one, Albus could publish but never say why (plan #244 WP2b
// follow-up, 2026-09-26).
type publishBody struct {
	Reason  string
	IfMatch string
}

// readPublishBody parses the -f file. An unknown field is refused, not ignored:
// a misspelled "reasom" would otherwise publish with no reason and look fine.
func readPublishBody(file string) (publishBody, error) {
	raw, err := readBody(file)
	if err != nil {
		return publishBody{}, err
	}
	m, ok := raw.(map[string]any)
	if raw != nil && !ok {
		return publishBody{}, fmt.Errorf("the publish body must be an object: {\"reason\": \"…\", \"if_match\": \"…\"}")
	}
	var out publishBody
	for k, v := range m {
		s, isString := v.(string)
		if !isString {
			return publishBody{}, fmt.Errorf("publish body field %q must be a string", k)
		}
		switch k {
		case "reason":
			out.Reason = s
		case "if_match":
			out.IfMatch = s
		default:
			return publishBody{}, fmt.Errorf("publish body field %q is not one of reason, if_match", k)
		}
	}
	return out, nil
}

func runWorkflowPublish(env *Env, api *client.Client, id, ifMatch, reason, key string) error {
	if ifMatch == "" {
		got, err := readDraft(env, api, id)
		if err != nil {
			return err
		}
		ifMatch = got.Header.Get("ETag")
	}
	if key == "" {
		var err error
		if key, err = newIdempotencyKey(); err != nil {
			return &ExitError{Code: 1, Msg: err.Error()}
		}
	}
	var body any
	if reason != "" {
		body = map[string]string{"reason": reason}
	}
	resp, err := api.DoResponse(http.MethodPost, spec.WorkflowsCanonicalPath+"/"+url.PathEscape(id)+"/publish", body,
		map[string]string{"If-Match": ifMatch, "Idempotency-Key": key})
	if err != nil {
		return &ExitError{Code: 1, Msg: err.Error()}
	}
	if resp.Status == http.StatusPreconditionFailed {
		_ = env.Render(resp.Body)
		return &ExitError{Code: 5, Msg: "the draft is no longer " + ifMatch + " (HTTP 412), so nothing was published; " +
			"read the draft again and publish the revision you mean"}
	}
	return env.finish(resp.Status, resp.Body)
}

// newIdempotencyKey is a fresh key per publish. It is deliberately NOT derived
// from the draft: publishing does not change the draft revision, so a derived
// key would make "unpublish, then publish the same draft again" replay the
// first version instead of activating one.
func newIdempotencyKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "yottactl-" + hex.EncodeToString(b), nil
}

// replaceWorkflowsDraftCommands swaps the registry-generated generic update
// and publish for the bespoke ones above.
func replaceWorkflowsDraftCommands(env *Env, workflows *cobra.Command) {
	for _, sub := range workflows.Commands() {
		switch sub.Name() {
		case "update", "publish":
			workflows.RemoveCommand(sub)
		}
	}
	workflows.AddCommand(newWorkflowsUpdateCmd(env), newWorkflowsPublishCmd(env))
}
