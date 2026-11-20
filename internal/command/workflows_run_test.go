package command

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

// sampleTargets: two namespaces in cluster-a, one in cluster-b. cluster-a is
// matchable by resource id (cx-cluster-a) or name snapshot (cluster-a).
func sampleTargets() []deploymentTarget {
	return []deploymentTarget{
		{ID: "t-1", ClusterResourceID: "cx-cluster-a", ClusterNameSnapshot: "cluster-a", NamespaceResourceID: "cx-ns-run", NamespaceSnapshot: "yotta-run"},
		{ID: "t-2", ClusterResourceID: "cx-cluster-a", ClusterNameSnapshot: "cluster-a", NamespaceResourceID: "cx-ns-batch", NamespaceSnapshot: "batch"},
		{ID: "t-3", ClusterResourceID: "cx-cluster-b", ClusterNameSnapshot: "cluster-b", NamespaceResourceID: "cx-ns-run", NamespaceSnapshot: "yotta-run"},
	}
}

// exitCode extracts the CLI exit code from an error, or -1 if it isn't an
// *ExitError.
func exitCode(err error) int {
	var e *ExitError
	if errors.As(err, &e) {
		return e.Code
	}
	return -1
}

// fakeRunSources answers the three reads an execution request may need, and
// records which ones were made — the common paths must not cost a read they
// do not need.
type fakeRunSources struct {
	targets     []deploymentTarget
	deployments []workflowDeployment
	effective   string
	err         error
	calls       []string
}

func (f *fakeRunSources) sources() runSources {
	return runSources{
		targets: func() ([]deploymentTarget, error) {
			f.calls = append(f.calls, "targets")
			return f.targets, f.err
		},
		deployments: func() ([]workflowDeployment, error) {
			f.calls = append(f.calls, "deployments")
			return f.deployments, f.err
		},
		effectiveTarget: func() (string, error) {
			f.calls = append(f.calls, "effective")
			return f.effective, f.err
		},
	}
}

func TestBuildExecutionRequest(t *testing.T) {
	activeDefault := []workflowDeployment{{ID: "d-1", Name: "primary", Status: "active", IsDefault: true}}
	for name, c := range map[string]struct {
		flags       runFlags
		deployments []workflowDeployment
		want        map[string]any
		calls       string
	}{
		"no flags runs the active default deployment": {
			deployments: activeDefault,
			want:        map[string]any{"deployment_id": "d-1"}, calls: "deployments",
		},
		"no flags and an inactive default runs ad hoc where the workflow resolves": {
			deployments: []workflowDeployment{{ID: "d-1", Status: "inactive", IsDefault: true}},
			want:        map[string]any{"deployment_target_id": "t-eff"}, calls: "deployments effective",
		},
		"no flags and no deployment runs ad hoc where the workflow resolves": {
			want: map[string]any{"deployment_target_id": "t-eff"}, calls: "deployments effective",
		},
		"a version alone runs ad hoc: a deployment cannot take another version": {
			flags: runFlags{version: "v-2"}, deployments: activeDefault,
			want: map[string]any{"deployment_target_id": "t-eff", "workflow_version_id": "v-2"}, calls: "effective",
		},
		"--deployment names the deployment and reads nothing": {
			flags: runFlags{deployment: "d-9"},
			want:  map[string]any{"deployment_id": "d-9"}, calls: "",
		},
		"--deployment-target runs ad hoc there and reads nothing": {
			flags: runFlags{deploymentTarget: "t-7", version: "v-2"},
			want:  map[string]any{"deployment_target_id": "t-7", "workflow_version_id": "v-2"}, calls: "",
		},
		"--cluster with --namespace resolves the exact target": {
			flags: runFlags{cluster: "cluster-a", namespace: "batch"},
			want:  map[string]any{"deployment_target_id": "t-2"}, calls: "targets",
		},
		"--cluster alone resolves its only target": {
			flags: runFlags{cluster: "cx-cluster-b"},
			want:  map[string]any{"deployment_target_id": "t-3"}, calls: "targets",
		},
	} {
		t.Run(name, func(t *testing.T) {
			src := &fakeRunSources{targets: sampleTargets(), deployments: c.deployments, effective: "t-eff"}
			got, err := buildExecutionRequest(src.sources(), c.flags)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotJSON, wantJSON := mustJSON(t, got), mustJSON(t, c.want); gotJSON != wantJSON {
				t.Errorf("body = %s, want %s", gotJSON, wantJSON)
			}
			if calls := strings.Join(src.calls, " "); calls != c.calls {
				t.Errorf("reads = %q, want %q", calls, c.calls)
			}
		})
	}
}

// Flags that cannot mean one request are refused before any read, as usage
// errors (exit 4).
func TestBuildExecutionRequestRefusesConflictingFlags(t *testing.T) {
	for name, f := range map[string]runFlags{
		"deployment and version":         {deployment: "d-1", version: "v-1"},
		"deployment and target":          {deployment: "d-1", deploymentTarget: "t-1"},
		"deployment and cluster":         {deployment: "d-1", cluster: "cluster-a"},
		"target and cluster":             {deploymentTarget: "t-1", cluster: "cluster-a"},
		"target and namespace":           {deploymentTarget: "t-1", namespace: "batch"},
		"namespace without a cluster":    {namespace: "batch"},
		"a cluster with several targets": {cluster: "cluster-a"},
		"a cluster with no target":       {cluster: "cluster-z"},
		"a namespace the cluster lacks":  {cluster: "cluster-a", namespace: "nope"},
	} {
		t.Run(name, func(t *testing.T) {
			src := &fakeRunSources{targets: sampleTargets()}
			_, err := buildExecutionRequest(src.sources(), f)
			if exitCode(err) != 4 {
				t.Fatalf("err = %v, want exit 4", err)
			}
		})
	}
}

// A failed read is the request's failure, never a guess.
func TestBuildExecutionRequestPropagatesReadFailures(t *testing.T) {
	sentinel := &ExitError{Code: 2, Msg: "boom"}
	for name, f := range map[string]runFlags{
		"deployments": {},
		"effective":   {version: "v-1"},
		"targets":     {cluster: "cluster-a", namespace: "batch"},
	} {
		t.Run(name, func(t *testing.T) {
			src := &fakeRunSources{err: sentinel}
			if _, err := buildExecutionRequest(src.sources(), f); err != sentinel {
				t.Fatalf("err = %v, want the read's own error", err)
			}
		})
	}
	src := &fakeRunSources{}
	if _, err := buildExecutionRequest(src.sources(), runFlags{}); exitCode(err) != 4 {
		t.Fatalf("no deployment and no resolvable target → %v, want exit 4", err)
	}
}

// The bespoke run command replaces the generic one: exactly one `run`
// subcommand, taking one positional arg and carrying its flags.
func TestWorkflowsRunCmd_MountedWithFlags(t *testing.T) {
	workflows := findWorkflowsCmd(t)
	var runs []*cobra.Command
	for _, sub := range workflows.Commands() {
		if sub.Name() == "run" {
			runs = append(runs, sub)
		}
	}
	if len(runs) != 1 {
		t.Fatalf("expected exactly one `workflows run` subcommand, got %d", len(runs))
	}
	run := runs[0]
	if err := run.Args(run, []string{}); err == nil {
		t.Error("`workflows run` should require a workflow id")
	}
	if err := run.Args(run, []string{"wf-1"}); err != nil {
		t.Errorf("`workflows run wf-1` should be accepted: %v", err)
	}
	for _, f := range []string{"deployment", "version", "deployment-target", "cluster", "namespace", "idempotency-key"} {
		if run.Flags().Lookup(f) == nil {
			t.Errorf("`workflows run` missing --%s flag", f)
		}
	}
}

// runAPI is the workflows service as the run command sees it.
type runAPI struct {
	mu          sync.Mutex
	deployments string
	seen        []seenRequest
}

func (f *runAPI) serve(t *testing.T) *httptest.Server {
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
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/deployments"):
			_, _ = w.Write([]byte(f.deployments))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/effective-target"):
			_, _ = w.Write([]byte(`{"Target":{"id":"t-eff","name":"default"},"Source":"account_default"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/executions"):
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"run-1","mode":"ad_hoc","status":"pending"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"Not Found"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The command starts a canonical execution: POST .../executions with an
// Idempotency-Key, never the legacy /run route.
func TestWorkflowsRunPostsACanonicalExecution(t *testing.T) {
	api := &runAPI{deployments: `{"items":[],"next_cursor":null}`}
	srv := api.serve(t)
	err, out := runCLI(t, srv, "workflows", "run", "wf-1")
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	var post *seenRequest
	for i := range api.seen {
		if strings.HasSuffix(api.seen[i].path, "/run") {
			t.Fatalf("called the legacy route: %s %s", api.seen[i].method, api.seen[i].path)
		}
		if api.seen[i].method == http.MethodPost {
			post = &api.seen[i]
		}
	}
	if post == nil || post.path != "/v1/automation-platform/workflows/wf-1/executions" {
		t.Fatalf("requests = %+v, want a POST to the executions route", api.seen)
	}
	if !strings.HasPrefix(post.header.Get("Idempotency-Key"), "yottactl-") {
		t.Errorf("Idempotency-Key = %q, want a fresh yottactl key", post.header.Get("Idempotency-Key"))
	}
	if post.body["deployment_target_id"] != "t-eff" {
		t.Errorf("body = %v, want the resolved target", post.body)
	}
	if !strings.Contains(out, `"id": "run-1"`) && !strings.Contains(out, `"id":"run-1"`) {
		t.Errorf("output does not show the run: %s", out)
	}

	// A caller-supplied key is sent as given, so a retry replays the first run.
	api.seen = nil
	if err, out := runCLI(t, srv, "workflows", "run", "wf-1", "--deployment", "d-1", "--idempotency-key", "retry-1"); err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	last := api.seen[len(api.seen)-1]
	if last.header.Get("Idempotency-Key") != "retry-1" || last.body["deployment_id"] != "d-1" || len(api.seen) != 1 {
		t.Fatalf("requests = %+v", api.seen)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestWorkflowsRunControlHeaders(t *testing.T) {
	api := &runAPI{}
	srv := api.serve(t)
	for i := 0; i < 2; i++ {
		err, _ := runCLI(t, srv, "workflows", "run", "wf-1", "--deployment", "d-1",
			"--header", "idempotency-key=retry-header")
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(api.seen) != 2 {
		t.Fatalf("requests = %v", api.seen)
	}
	for _, req := range api.seen {
		if req.header.Get("Idempotency-Key") != "retry-header" {
			t.Fatalf("retry key = %q", req.header.Get("Idempotency-Key"))
		}
	}
	for _, args := range [][]string{
		{"--idempotency-key", "one", "--header", "Idempotency-Key=two"},
		{"--header", "Idempotency-Key=one", "--header", "idempotency-key=two"},
		{"--header", "Idempotency-Key="},
	} {
		api.seen = nil
		err, _ := runCLI(t, srv, append([]string{"workflows", "run", "wf-1"}, args...)...)
		if exitCode(err) != 4 || len(api.seen) != 0 {
			t.Errorf("%v: err=%v, requests=%v; want validation before HTTP", args, err, api.seen)
		}
	}
}

func findWorkflowsCmd(t *testing.T) *cobra.Command {
	t.Helper()
	wf := findSubcommand(NewRoot(), "workflows")
	if wf == nil {
		t.Fatal("workflows command missing from root")
	}
	return wf
}
