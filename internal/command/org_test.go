package command

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	orgApp    = "0a1b2c3d-0000-4000-8000-00000000000a"
	orgKeyApp = "0a1b2c3d-0000-4000-8000-00000000000b"
	orgOld    = "5ec0e700-0000-4000-8000-000000000001"
	orgNew    = "5ec0e700-0000-4000-8000-000000000002"
	// orgValue stands in for a secret's value: it must reach only --out or stdout.
	orgValue = "VALUE-only-for-the-file-or-stdout-0123456789"
)

// orgDirectory is login-directory's app-secret routes, as #279 C serves them
// to a CLI session's Bearer.
type orgDirectory struct {
	calls  []string // method, path, Authorization, Content-Type, body, and any Cookie or Origin
	answer map[string]func(w http.ResponseWriter)
}

func (d *orgDirectory) serve(w http.ResponseWriter, r *http.Request) {
	body := new(bytes.Buffer)
	_, _ = body.ReadFrom(r.Body)
	d.calls = append(d.calls, strings.Join([]string{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type"),
		body.String(), r.Header.Get("Cookie"), r.Header.Get("Origin")}, "|"))
	if a, ok := d.answer[r.Method+" "+r.URL.Path]; ok {
		a(w)
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

func newOrgDirectory(t *testing.T, f *fakeLogin) *orgDirectory {
	t.Helper()
	secrets := "/v1/directory/applications/" + orgApp + "/secrets"
	d := &orgDirectory{answer: map[string]func(http.ResponseWriter){
		"GET /v1/directory/applications": func(w http.ResponseWriter) {
			_ = json.NewEncoder(w).Encode(map[string]any{"portal_url": "https://acme.yotta.bot", "applications": []map[string]any{
				{"id": orgApp, "kind": "oidc", "name": "Argo CD", "enabled": true, "config": map[string]any{"redirect_uri": "https://argocd.test/cb", "token_endpoint_auth_method": "client_secret"}},
				{"id": orgKeyApp, "kind": "oidc", "name": "Support", "enabled": false, "config": map[string]any{"jwks_uri": "https://support.test/keys"}},
				{"id": "0a1b2c3d-0000-4000-8000-00000000000c", "kind": "saml", "name": "Wiki", "enabled": true},
			}})
		},
		"GET " + secrets: func(w http.ResponseWriter) {
			_ = json.NewEncoder(w).Encode(map[string]any{"secrets": []map[string]any{{"id": orgOld, "created_at": "2026-10-23T08:00:00Z"}}})
		},
		"POST " + secrets: func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": orgNew, "secret": orgValue, "created_at": "2026-10-23T09:00:00Z"})
		},
		"DELETE " + secrets + "/" + orgOld: func(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) },
	}}
	f.directory = d.serve
	return d
}

func runOrg(args ...string) (string, string, error) {
	var out, errOut bytes.Buffer
	cmd := newOrgCmd(&Env{Output: "json"})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestOrgListsAppsAndManagesSecrets(t *testing.T) {
	f := newFakeLogin(t, "ok")
	useFakes(t, f)
	if _, errOut, err := runLoginCmd(f, "--no-browser"); err != nil {
		t.Fatalf("login: %v\n%s", err, errOut)
	}
	d := newOrgDirectory(t, f)

	out, _, err := runOrg("apps", "list")
	if err != nil {
		t.Fatal(err)
	}
	var apps []map[string]any
	if err := json.Unmarshal([]byte(out), &apps); err != nil || len(apps) != 3 {
		t.Fatalf("apps: %v %s", err, out)
	}
	for i, want := range [][2]string{{"app-" + orgApp, "client_secret"}, {"app-" + orgKeyApp, "private_key_jwt"}, {"", ""}} {
		if apps[i]["client_id"] != want[0] || apps[i]["token_auth"] != want[1] {
			t.Errorf("app %d: %v, want %v", i, apps[i], want)
		}
	}

	// The client ID and the ID name the same app.
	out, _, err = runOrg("apps", "secrets", "list", "app-"+orgApp)
	if err != nil || !strings.Contains(out, orgOld) {
		t.Fatalf("secrets: %v %s", err, out)
	}

	// --out: the value alone in a new 0600 file; nothing on stdout or stderr.
	file := filepath.Join(t.TempDir(), "argocd.secret")
	out, errOut, err := runOrg("apps", "secrets", "create", orgApp, "--out", file)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(file); err != nil || string(got) != orgValue {
		t.Fatalf("the file: %q %v", got, err)
	}
	if st, _ := os.Stat(file); st.Mode().Perm() != 0o600 {
		t.Errorf("the file's mode: %v", st.Mode())
	}
	if out != "" || strings.Contains(errOut, orgValue) || !strings.Contains(errOut, orgNew) || !strings.Contains(errOut, "written to "+file) {
		t.Errorf("stdout %q, stderr %q", out, errOut)
	}

	// A file that exists is refused before a secret is made.
	posts := len(d.calls)
	if _, _, err := runOrg("apps", "secrets", "create", orgApp, "--out", file); exitCode(err) != 4 || len(d.calls) != posts {
		t.Fatalf("an existing --out: %v, %d calls", err, len(d.calls)-posts)
	}

	// No --out: the value alone on stdout, for $(...).
	out, errOut, err = runOrg("apps", "secrets", "create", orgApp)
	if err != nil || out != orgValue+"\n" || strings.Contains(errOut, orgValue) || !strings.Contains(errOut, "printed on stdout") {
		t.Fatalf("stdout %q, stderr %q, %v", out, errOut, err)
	}

	if out, _, err := runOrg("apps", "secrets", "delete", "app-"+orgApp, orgOld); err != nil || out != "Deleted secret "+orgOld+".\n" {
		t.Fatalf("delete: %v %q", err, out)
	}

	// Each call went to the saved issuer with the CLI session alone, and a
	// write carried JSON.
	secrets := "/v1/directory/applications/" + orgApp + "/secrets"
	want := []string{
		"GET|/v1/directory/applications|Bearer cli-secret||||",
		"GET|" + secrets + "|Bearer cli-secret||||",
		"POST|" + secrets + "|Bearer cli-secret|application/json|{}||",
		"POST|" + secrets + "|Bearer cli-secret|application/json|{}||",
		"DELETE|" + secrets + "/" + orgOld + "|Bearer cli-secret||||",
	}
	if strings.Join(d.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(d.calls, "\n"), strings.Join(want, "\n"))
	}
	if len(f.stray) != 0 || len(f.apiAuth) != 0 {
		t.Errorf("stray %v, environment %v", f.stray, f.apiAuth)
	}
}

func TestOrgSaysWhatToDo(t *testing.T) {
	f := newFakeLogin(t, "ok")
	clock := useFakes(t, f)
	if _, _, err := runOrg("apps", "list"); exitCode(err) != 2 || !strings.Contains(err.Error(), "yottactl login") {
		t.Fatalf("before login: %v", err)
	}
	if _, errOut, err := runLoginCmd(f, "--no-browser"); err != nil {
		t.Fatalf("login: %v\n%s", err, errOut)
	}
	d := newOrgDirectory(t, f)
	secrets := "/v1/directory/applications/" + orgApp + "/secrets"
	answer := func(code int, body map[string]string) func(http.ResponseWriter) {
		return func(w http.ResponseWriter) {
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(body)
		}
	}

	for name, tc := range map[string]struct {
		code int
		body map[string]string
		exit int
		says string
	}{
		"signed out":       {401, map[string]string{"error": "not signed in"}, 2, "yottactl login"},
		"no two-factor":    {403, map[string]string{"error": "sign in again", "next": "step_up"}, 2, "browser session that used your second factor"},
		"no factor at all": {403, map[string]string{"error": "set up", "next": "enroll_two_factor"}, 2, "set it up at"},
		"not an admin":     {403, map[string]string{"error": "only an organization administrator can do this"}, 2, "only an organization administrator"},
		"two already":      {409, map[string]string{"error": "an app holds two client secrets at most; delete one first"}, 5, "two client secrets at most"},
	} {
		d.answer["POST "+secrets] = answer(tc.code, tc.body)
		file := filepath.Join(t.TempDir(), "s")
		_, _, err := runOrg("apps", "secrets", "create", orgApp, "--out", file)
		if exitCode(err) != tc.exit || !strings.Contains(err.Error(), tc.says) {
			t.Errorf("%s: %v (exit %d)", name, err, exitCode(err))
		}
		if _, statErr := os.Stat(file); !os.IsNotExist(statErr) {
			t.Errorf("%s: left %s behind", name, file)
		}
	}

	calls := len(d.calls)
	for _, args := range [][]string{
		{"apps", "secrets", "list", "nope"},
		{"apps", "secrets", "create", "app-../../people"},
		{"apps", "secrets", "delete", orgApp, "../../people"},
	} {
		if _, _, err := runOrg(args...); exitCode(err) != 4 {
			t.Errorf("%v: %v", args, err)
		}
	}
	if len(d.calls) != calls {
		t.Errorf("a bad argument was sent: %v", d.calls[calls:])
	}

	// The 8 hours end: nothing more is sent.
	clock.now = clock.now.Add(9 * time.Hour)
	if _, _, err := runOrg("apps", "list"); exitCode(err) != 2 || len(d.calls) != calls {
		t.Fatalf("after the sign-in ended: %v", err)
	}
}
