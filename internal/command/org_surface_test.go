package command

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// yottactl's org commands are the servers' surfaces, row for row: the
// Identity API's (plan 333 step 5) and Boo · Admin's (step 7).

func TestOrgHasACommandForEveryRow(t *testing.T) {
	org := newOrgCmd(&Env{Output: "json"})
	for _, row := range append(directorySurface(), booSurface()...) {
		if row.CLI == "" {
			t.Errorf("%s %s has no cli name", row.Method, row.Path)
			continue
		}
		cmd, rest, err := org.Find(strings.Fields(row.CLI))
		if err != nil || len(rest) != 0 || cmd.Name() != strings.Fields(row.CLI)[len(strings.Fields(row.CLI))-1] {
			t.Errorf("yottactl org %s: no such command (%v)", row.CLI, err)
			continue
		}
		if handWritten[row.CLI] {
			continue
		}
		if got, want := strings.Count(cmd.Use, "<"), strings.Count(row.Path, "{"); got != want {
			t.Errorf("yottactl org %s takes %d arguments; %s has %d", row.CLI, got, row.Path, want)
		}
		if !strings.Contains(cmd.Long, row.Permission) || !strings.Contains(cmd.Long, "https://yotta.bot/platform/"+row.Doc) {
			t.Errorf("yottactl org %s doesn't name its permission and reference", row.CLI)
		}
	}
}

// surfaceDirectory answers any route, and records each call.
type surfaceDirectory struct{ calls []string }

func (d *surfaceDirectory) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	d.calls = append(d.calls, strings.Join([]string{r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"), r.Header.Get("Content-Type"), string(body)}, "|"))
	switch {
	case r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]any{"people": []map[string]any{{"id": "p1", "email": "pat@example.com"}}})
	case r.Method == http.MethodDelete:
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "r1"})
	}
}

func runOrgWith(env *Env, args ...string) (string, string, error) {
	var out, errOut bytes.Buffer
	cmd := newOrgCmd(env)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestOrgSurfaceCommandsCallTheirRoutes(t *testing.T) {
	f := newFakeLogin(t, "ok")
	useFakes(t, f)
	if _, errOut, err := runLoginCmd(f, "--no-browser"); err != nil {
		t.Fatalf("login: %v\n%s", err, errOut)
	}
	d := &surfaceDirectory{}
	f.directory = d.serve
	env := &Env{Output: "json"}
	bearer := "Bearer " + cliSecret

	// A list renders its rows.
	out, _, err := runOrgWith(env, "users", "list")
	var rows []map[string]any
	if err != nil || json.Unmarshal([]byte(out), &rows) != nil || len(rows) != 1 || rows[0]["id"] != "p1" {
		t.Fatalf("users list: %v %s", err, out)
	}
	cases := []struct {
		args []string
		call string
	}{
		{[]string{"users", "list"}, "GET|/v1/directory/people|" + bearer + "||"},
		{[]string{"users", "invite", "--set", "email=pat@example.com", "--set", "username=patlee"},
			"POST|/v1/directory/people|" + bearer + "|application/json|{\"email\":\"pat@example.com\",\"username\":\"patlee\"}"},
		{[]string{"roles", "add-policy", "r1", "p1"}, "PUT|/v1/directory/roles/r1/policies/p1|" + bearer + "|application/json|{}"},
		{[]string{"groups", "remove-member", "g1", "p1"}, "DELETE|/v1/directory/groups/g1/members/p1|" + bearer + "||"},
		{[]string{"policies", "set-permission", "p1", "--set", "permission=agents:RunAgent", "--set", "granted=true"},
			"POST|/v1/directory/policies/p1/permissions|" + bearer + "|application/json|{\"granted\":true,\"permission\":\"agents:RunAgent\"}"},
		{[]string{"assignments", "create", "--set", "permission_set_id=r1", "--set", "account_id=100000000123", "--set", "group_id=g1"},
			"POST|/v1/directory/assignments|" + bearer + "|application/json|{\"account_id\":100000000123,\"group_id\":\"g1\",\"permission_set_id\":\"r1\"}"},
	}
	for _, c := range cases {
		d.calls = nil
		if _, errOut, err := runOrgWith(env, c.args...); err != nil {
			t.Fatalf("%v: %v %s", c.args, err, errOut)
		}
		if len(d.calls) != 1 || strings.TrimSpace(d.calls[0]) != c.call {
			t.Errorf("%v sent %q, want %q", c.args, d.calls, c.call)
		}
	}

	// Boo · Admin's reads, `org boo`: GETs alone, its path parameter an
	// argument, --query its filters.
	for _, c := range []struct {
		args  []string
		query []string
		call  string
	}{
		{[]string{"boo", "balance"}, nil, "GET|/v1/login/boo-admin/balance|" + bearer + "||"},
		{[]string{"boo", "usage"}, []string{"group_by=account", "account_id=100000000123"}, "GET|/v1/login/boo-admin/usage?account_id=100000000123&group_by=account|" + bearer + "||"},
		{[]string{"boo", "allowance", "p1"}, nil, "GET|/v1/login/boo-admin/allowances/p1|" + bearer + "||"},
		{[]string{"boo", "cloud-machines"}, nil, "GET|/v1/login/boo-admin/machines|" + bearer + "||"},
	} {
		d.calls = nil
		if _, errOut, err := runOrgWith(&Env{Output: "json", Query: c.query}, c.args...); err != nil {
			t.Fatalf("%v: %v %s", c.args, err, errOut)
		}
		if len(d.calls) != 1 || strings.TrimSpace(d.calls[0]) != c.call {
			t.Errorf("%v sent %q, want %q", c.args, d.calls, c.call)
		}
	}
	for _, row := range booSurface() {
		if row.Method != http.MethodGet {
			t.Errorf("yottactl org %s changes something; Boo · Admin's changes are the console's", row.CLI)
		}
	}

	// --query reaches the route's query string.
	d.calls = nil
	if _, _, err := runOrgWith(&Env{Output: "json", Query: []string{"limit=5"}}, "events", "list"); err != nil || !strings.HasPrefix(d.calls[0], "GET|/v1/directory/events?limit=5|") {
		t.Fatalf("events list --query: %v %q", err, d.calls)
	}
	// A wrong count of arguments, or both -f and --set, is refused before any call.
	d.calls = nil
	if _, _, err := runOrgWith(env, "roles", "add-policy", "r1"); err == nil || len(d.calls) != 0 {
		t.Fatalf("one argument for two: %v %q", err, d.calls)
	}
	if _, _, err := runOrgWith(env, "groups", "create", "-f", "x.json", "--set", "name=x"); err == nil || len(d.calls) != 0 {
		t.Fatalf("-f and --set together: %v %q", err, d.calls)
	}

	// org token prints the session's token, alone.
	out, _, err = runOrgWith(env, "token")
	if err != nil || out != cliSecret+"\n" {
		t.Fatalf("org token: %v %q", err, out)
	}
}

func TestOrgTokenNeedsASession(t *testing.T) {
	f := newFakeLogin(t, "ok")
	useFakes(t, f)
	_, _, err := runOrgWith(&Env{Output: "json"}, "token")
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != 2 {
		t.Fatalf("org token without a login: %v", err)
	}
}
