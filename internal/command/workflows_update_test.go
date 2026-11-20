package command

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ── Plan #244 WP2b-3: the draft-aware workflows commands ─────────────
//
// A fake workflows API records every request, so each case can assert both
// what was sent and — the point of most of them — what was NOT: no legacy
// definition PATCH, no metadata write before the draft is safe, no request at
// all for a body refused locally.

const (
	v2Definition   = `{"schema_version":2,"nodes":[],"connections":{}}`
	draftETag      = `"draft-3-0123456789ab"`
	nextDraftETag  = `"draft-4-ba9876543210"`
	adoptedID      = "11111111-1111-4111-8111-111111111111"
	pendingID      = "22222222-2222-4222-8222-222222222222"
	failedID       = "33333333-3333-4333-8333-333333333333"
	canonicalDraft = "/v1/automation-platform/workflows/" + adoptedID + "/draft"
)

type seenRequest struct {
	method, path string
	header       http.Header
	body         map[string]any
}

type fakeWorkflowsAPI struct {
	mu       sync.Mutex
	seen     []seenRequest
	putCode  int // PUT draft status; 0 = 200, or 412 when If-Match is stale
	patchErr int // PATCH status when non-zero
}

func (f *fakeWorkflowsAPI) requests() []seenRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]seenRequest(nil), f.seen...)
}

func (f *fakeWorkflowsAPI) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = nil
}

func (f *fakeWorkflowsAPI) calls() []string {
	var out []string
	for _, r := range f.requests() {
		out = append(out, r.method+" "+r.path)
	}
	return out
}

func (f *fakeWorkflowsAPI) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.seen = append(f.seen, seenRequest{method: r.Method, path: r.URL.Path, header: r.Header.Clone(), body: body})
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/draft"):
			switch {
			case strings.Contains(r.URL.Path, pendingID):
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"Not Found","message":"not_found","adoption_state":"pending"}`))
			case strings.Contains(r.URL.Path, failedID):
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"Not Found","message":"not_found","adoption_state":"failed","adoption_reason":"roundtrip_mismatch"}`))
			case strings.Contains(r.URL.Path, adoptedID):
				w.Header().Set("ETag", draftETag)
				_, _ = w.Write([]byte(`{"workflow_id":"` + adoptedID + `","revision":3,"definition":` + v2Definition + `}`))
			default:
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"Not Found","message":"not_found"}`))
			}
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/draft"):
			code := f.putCode
			if r.Header.Get("If-Match") != draftETag {
				code = http.StatusPreconditionFailed
			}
			if code == 0 || code == http.StatusOK {
				w.Header().Set("ETag", nextDraftETag)
				_, _ = w.Write([]byte(`{"workflow_id":"` + adoptedID + `","revision":4,"canonical_digest":"sha256:ba98"}`))
				return
			}
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"error":"Precondition Failed","message":"draft_conflict"}`))
		case r.Method == http.MethodPatch:
			if f.patchErr != 0 {
				w.WriteHeader(f.patchErr)
				_, _ = w.Write([]byte(`{"error":"Conflict","message":"conflict"}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"` + adoptedID + `","name":"renamed"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/publish"):
			if r.Header.Get("If-Match") != draftETag && r.Header.Get("If-Match") != "draft-3" {
				w.WriteHeader(http.StatusPreconditionFailed)
				_, _ = w.Write([]byte(`{"error":"Precondition Failed","message":"draft_conflict"}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"v-1","version_number":1,"state":"published"}`))
		case r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"` + adoptedID + `","adoption_state":"ready"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// runCLI runs yottactl against srv and returns its error and stdout.
func runCLI(t *testing.T, srv *httptest.Server, args ...string) (error, string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	root := NewRoot()
	root.SetArgs(append([]string{"--endpoint", srv.URL, "--token", "test", "--output", "json"}, args...))
	runErr := root.Execute()
	os.Stdout = stdout
	_ = w.Close()
	out, _ := io.ReadAll(r)
	return runErr, string(out)
}

func bodyFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWorkflowsUpdate_DefinitionGoesToTheDraftThenMetadata(t *testing.T) {
	api := &fakeWorkflowsAPI{}
	srv := api.serve(t)
	err, out := runCLI(t, srv, "workflows", "update", adoptedID, "-f",
		bodyFile(t, `{"definition":`+v2Definition+`,"name":"renamed","tags":"t"}`))
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	want := []string{"GET " + canonicalDraft, "PUT " + canonicalDraft, "PATCH /v1/agent-platform/workflows/" + adoptedID}
	if got := api.calls(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("requests = %v, want %v", got, want)
	}
	reqs := api.requests()
	if reqs[1].header.Get("If-Match") != draftETag {
		t.Errorf("PUT If-Match = %q, want the ETag the GET returned", reqs[1].header.Get("If-Match"))
	}
	if _, ok := reqs[1].body["definition"]; !ok || len(reqs[1].body) != 1 {
		t.Errorf("PUT body = %v, want only the definition", reqs[1].body)
	}
	if _, ok := reqs[2].body["definition"]; ok || reqs[2].body["name"] != "renamed" || reqs[2].body["tags"] != "t" {
		t.Errorf("PATCH body = %v, want the metadata and no definition", reqs[2].body)
	}
	var result struct {
		Draft    map[string]any `json:"draft"`
		Workflow map[string]any `json:"workflow"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output %q: %v", out, err)
	}
	if result.Draft["etag"] != nextDraftETag || result.Workflow["name"] != "renamed" {
		t.Errorf("output = %s; it must carry the new draft ETag for publish, and the workflow", out)
	}
}

func TestWorkflowsUpdate_MetadataOnlyIsOnePatch(t *testing.T) {
	api := &fakeWorkflowsAPI{}
	srv := api.serve(t)
	for _, body := range []string{`{"name":"renamed"}`, `{"status":"unavailable"}`, `{"name":"n","definition":null}`} {
		api.reset()
		if err, _ := runCLI(t, srv, "workflows", "update", adoptedID, "-f", bodyFile(t, body)); err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		if got := api.calls(); len(got) != 1 || got[0] != "PATCH /v1/agent-platform/workflows/"+adoptedID {
			t.Errorf("%s → %v, want one PATCH", body, got)
		}
		if _, ok := api.requests()[0].body["definition"]; ok {
			t.Errorf("%s: a null definition must not be sent", body)
		}
	}
}

func TestWorkflowsUpdatePreservesReviewedRevision(t *testing.T) {
	api := &fakeWorkflowsAPI{}
	srv := api.serve(t)
	// The read returns revision 3, but the caller reviewed revision 2. The
	// request must fail, and the accompanying metadata must remain untouched.
	err, _ := runCLI(t, srv, "workflows", "update", adoptedID,
		"--header", "If-Match=draft-2", "-f",
		bodyFile(t, `{"definition":`+v2Definition+`,"name":"must not change"}`))
	reqs := api.requests()
	if exitCode(err) != 5 || len(reqs) != 2 || reqs[1].method != http.MethodPut || reqs[1].header.Get("If-Match") != "draft-2" {
		t.Fatalf("err=%v, requests=%v; want stale draft refused before metadata", err, reqs)
	}
}

func TestWorkflowsUpdate_DraftlessWorkflowIsRefusedWithoutFallback(t *testing.T) {
	api := &fakeWorkflowsAPI{}
	srv := api.serve(t)
	for id, want := range map[string]string{
		pendingID: "adoption_state=pending",
		failedID:  "adoption_state=failed, adoption_reason=roundtrip_mismatch",
	} {
		api.reset()
		err, _ := runCLI(t, srv, "workflows", "update", id, "-f",
			bodyFile(t, `{"definition":`+v2Definition+`,"name":"renamed"}`))
		if exitCode(err) != 5 || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "nothing was written") {
			t.Errorf("%s: err = %v (exit %d), want exit 5 naming %s", id, err, exitCode(err), want)
		}
		if got := api.calls(); len(got) != 1 || !strings.HasPrefix(got[0], "GET ") {
			t.Errorf("%s: requests = %v — after the draft read, NOTHING: no legacy PATCH fallback, no metadata write", id, got)
		}
	}
	// A workflow that does not exist is still the plain not-found.
	api.reset()
	err, _ := runCLI(t, srv, "workflows", "update", "44444444-4444-4444-8444-444444444444", "-f", bodyFile(t, `{"definition":`+v2Definition+`}`))
	if exitCode(err) != 3 || len(api.calls()) != 1 {
		t.Errorf("missing workflow: exit %d, requests %v", exitCode(err), api.calls())
	}
}

func TestWorkflowsUpdate_StaleDraftWritesNothingElse(t *testing.T) {
	api := &fakeWorkflowsAPI{putCode: http.StatusPreconditionFailed}
	srv := api.serve(t)
	err, _ := runCLI(t, srv, "workflows", "update", adoptedID, "-f",
		bodyFile(t, `{"definition":`+v2Definition+`,"name":"renamed","status":null}`))
	if exitCode(err) != 5 || !strings.Contains(err.Error(), "nothing was written") {
		t.Fatalf("stale draft: err = %v (exit %d)", err, exitCode(err))
	}
	for _, c := range api.calls() {
		if strings.HasPrefix(c, "PATCH ") {
			t.Errorf("a stale draft fell through to %s", c)
		}
	}
}

func TestWorkflowsUpdate_RefusedLocallyBeforeAnyRequest(t *testing.T) {
	api := &fakeWorkflowsAPI{}
	srv := api.serve(t)
	for name, c := range map[string]struct{ body, want string }{
		"a v1 definition":              {`{"definition":{"steps":[{"name":"a","type":"agent_call"}]}}`, "canonical v2"},
		"an empty object definition":   {`{"definition":{}}`, "canonical v2"},
		"definition plus status":       {`{"definition":` + v2Definition + `,"status":"available"}`, "publish"},
		"definition plus reason":       {`{"definition":` + v2Definition + `,"change_reason":"fix"}`, "change_reason"},
		"a body that is not an object": {`["definition"]`, "JSON object"},
	} {
		t.Run(name, func(t *testing.T) {
			api.reset()
			err, _ := runCLI(t, srv, "workflows", "update", adoptedID, "-f", bodyFile(t, c.body))
			if exitCode(err) != 4 || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v (exit %d), want exit 4 mentioning %q", err, exitCode(err), c.want)
			}
			if got := api.calls(); len(got) != 0 {
				t.Errorf("a locally refused body sent %v", got)
			}
		})
	}
}

func TestWorkflowsUpdate_MetadataFailureAfterTheDraftSaysSo(t *testing.T) {
	api := &fakeWorkflowsAPI{patchErr: http.StatusConflict}
	srv := api.serve(t)
	err, _ := runCLI(t, srv, "workflows", "update", adoptedID, "-f",
		bodyFile(t, `{"definition":`+v2Definition+`,"name":"renamed"}`))
	if exitCode(err) != 5 || !strings.Contains(err.Error(), "the draft was written ("+nextDraftETag+")") {
		t.Errorf("err = %v (exit %d); the caller must learn the draft landed and the metadata did not", err, exitCode(err))
	}
}

func TestWorkflowsPublish(t *testing.T) {
	api := &fakeWorkflowsAPI{}
	srv := api.serve(t)
	publish := "POST /v1/automation-platform/workflows/" + adoptedID + "/publish"

	t.Run("publishes the draft it reads, with a fresh key each time", func(t *testing.T) {
		api.reset()
		for i := 0; i < 2; i++ {
			if err, _ := runCLI(t, srv, "workflows", "publish", adoptedID, "--reason", "ship it"); err != nil {
				t.Fatalf("publish: %v", err)
			}
		}
		reqs := api.requests()
		if got := api.calls(); len(got) != 4 || got[0] != "GET "+canonicalDraft || got[1] != publish {
			t.Fatalf("requests = %v", got)
		}
		if reqs[1].header.Get("If-Match") != draftETag || reqs[1].body["reason"] != "ship it" {
			t.Errorf("publish If-Match %q body %v", reqs[1].header.Get("If-Match"), reqs[1].body)
		}
		k1, k2 := reqs[1].header.Get("Idempotency-Key"), reqs[3].header.Get("Idempotency-Key")
		if k1 == "" || k1 == k2 {
			t.Errorf("Idempotency-Key %q then %q; each publish needs its own", k1, k2)
		}
	})
	t.Run("--if-match publishes exactly that revision, without a read", func(t *testing.T) {
		api.reset()
		if err, _ := runCLI(t, srv, "workflows", "publish", adoptedID, "--if-match", "draft-3", "--idempotency-key", "k-1"); err != nil {
			t.Fatalf("publish: %v", err)
		}
		reqs := api.requests()
		if len(reqs) != 1 || reqs[0].header.Get("If-Match") != "draft-3" || reqs[0].header.Get("Idempotency-Key") != "k-1" {
			t.Errorf("requests = %v", api.calls())
		}
	})
	t.Run("a draft that moved on is refused", func(t *testing.T) {
		api.reset()
		err, _ := runCLI(t, srv, "workflows", "publish", adoptedID, "--if-match", `"draft-2-000000000000"`)
		if exitCode(err) != 5 || !strings.Contains(err.Error(), "nothing was published") {
			t.Errorf("err = %v (exit %d)", err, exitCode(err))
		}
	})
	t.Run("a workflow with no draft publishes nothing", func(t *testing.T) {
		api.reset()
		err, _ := runCLI(t, srv, "workflows", "publish", pendingID)
		if exitCode(err) != 5 || len(api.calls()) != 1 {
			t.Errorf("err = %v (exit %d), requests %v", err, exitCode(err), api.calls())
		}
	})

	// The catalog advertises publish with a body, because a catalog tool
	// passes a body but not flags; Albus reaches publish through -f.
	bodyFile := func(t *testing.T, content string) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "publish.json")
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	t.Run("-f carries the reason and the revision to publish", func(t *testing.T) {
		api.reset()
		f := bodyFile(t, `{"reason":"from the body","if_match":"draft-3"}`)
		if err, _ := runCLI(t, srv, "workflows", "publish", adoptedID, "-f", f); err != nil {
			t.Fatalf("publish: %v", err)
		}
		reqs := api.requests()
		if len(reqs) != 1 || reqs[0].header.Get("If-Match") != "draft-3" || reqs[0].body["reason"] != "from the body" {
			t.Errorf("requests %v; publish If-Match %q body %v", api.calls(), reqs[0].header.Get("If-Match"), reqs[0].body)
		}
	})
	t.Run("a flag given as well beats the body", func(t *testing.T) {
		api.reset()
		f := bodyFile(t, `{"reason":"from the body"}`)
		if err, _ := runCLI(t, srv, "workflows", "publish", adoptedID, "-f", f, "--reason", "from the flag"); err != nil {
			t.Fatalf("publish: %v", err)
		}
		reqs := api.requests()
		if len(reqs) != 2 || reqs[1].body["reason"] != "from the flag" {
			t.Errorf("requests %v; body %v", api.calls(), reqs[len(reqs)-1].body)
		}
	})
	t.Run("an empty body publishes the draft as read, with no reason", func(t *testing.T) {
		api.reset()
		if err, _ := runCLI(t, srv, "workflows", "publish", adoptedID, "-f", bodyFile(t, `{}`)); err != nil {
			t.Fatalf("publish: %v", err)
		}
		reqs := api.requests()
		if got := api.calls(); len(got) != 2 || got[0] != "GET "+canonicalDraft || got[1] != publish {
			t.Fatalf("requests = %v", got)
		}
		if reqs[1].header.Get("If-Match") != draftETag || reqs[1].body["reason"] != nil {
			t.Errorf("publish If-Match %q body %v", reqs[1].header.Get("If-Match"), reqs[1].body)
		}
	})
	t.Run("a misspelled body field is refused before any request", func(t *testing.T) {
		api.reset()
		err, _ := runCLI(t, srv, "workflows", "publish", adoptedID, "-f", bodyFile(t, `{"reasom":"typo"}`))
		if exitCode(err) != 4 || len(api.calls()) != 0 || !strings.Contains(err.Error(), "reasom") {
			t.Errorf("err = %v (exit %d), requests %v", err, exitCode(err), api.calls())
		}
	})
}

func TestWorkflowsPublishControlHeaders(t *testing.T) {
	api := &fakeWorkflowsAPI{}
	srv := api.serve(t)
	// Header-only callers must neither read a newer draft nor receive a fresh
	// retry key. Repeating the command must send the same controls both times.
	for i := 0; i < 2; i++ {
		err, _ := runCLI(t, srv, "workflows", "publish", adoptedID,
			"--header", "if-match=draft-3", "--header", "idempotency-key=retry-header")
		if err != nil {
			t.Fatal(err)
		}
	}
	if reqs := api.requests(); len(reqs) != 2 {
		t.Fatalf("requests = %v, want only two publishes", api.calls())
	} else {
		for _, req := range reqs {
			if req.header.Get("If-Match") != "draft-3" || req.header.Get("Idempotency-Key") != "retry-header" {
				t.Fatalf("publish controls = %v", req.header)
			}
		}
	}
	for _, args := range [][]string{
		{"--if-match", "draft-3", "--header", "If-Match=draft-2"},
		{"--idempotency-key", "one", "--header", "Idempotency-Key=two"},
		{"--header", "If-Match=draft-3", "--header", "if-match=draft-2"},
		{"--header", "Idempotency-Key="},
		{"--header", "If-Match= "},
	} {
		api.reset()
		err, _ := runCLI(t, srv, append([]string{"workflows", "publish", adoptedID}, args...)...)
		if exitCode(err) != 4 || len(api.calls()) != 0 {
			t.Errorf("%v: err=%v, requests=%v; want validation before HTTP", args, err, api.calls())
		}
	}
	api.reset()
	err, _ := runCLI(t, srv, "workflows", "publish", adoptedID,
		"--if-match", "draft-3", "--header", "If-Match=draft-3",
		"--idempotency-key", "same", "--header", "Idempotency-Key=same")
	if err != nil || len(api.calls()) != 1 {
		t.Fatalf("matching controls: %v, requests=%v", err, api.calls())
	}
}

// create moves to the canonical, draft-first POST; the other verbs stay on
// the legacy base until their canonical routes exist.
func TestWorkflowsCRUDRoutes(t *testing.T) {
	api := &fakeWorkflowsAPI{}
	srv := api.serve(t)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"workflows", "create", "-f", bodyFile(t, `{"name":"w","definition":`+v2Definition+`}`)}, "POST /v1/automation-platform/workflows"},
		{[]string{"workflows", "list"}, "GET /v1/agent-platform/workflows"},
		{[]string{"workflows", "get", adoptedID}, "GET /v1/agent-platform/workflows/" + adoptedID},
		{[]string{"workflows", "delete", adoptedID}, "DELETE /v1/agent-platform/workflows/" + adoptedID},
		{[]string{"workflows", "draft", adoptedID}, "GET " + canonicalDraft},
		{[]string{"workflows", "unpublish", adoptedID}, "POST /v1/automation-platform/workflows/" + adoptedID + "/unpublish"},
		{[]string{"drone", "workflows", "create", "-f", bodyFile(t, `{"name":"d"}`)}, "POST /v1/drone-platform/workflows"},
	} {
		api.reset()
		if err, _ := runCLI(t, srv, c.args...); err != nil {
			t.Fatalf("%v: %v", c.args, err)
		}
		if got := api.calls(); len(got) != 1 || got[0] != c.want {
			t.Errorf("%v → %v, want %s", c.args, got, c.want)
		}
	}
}
