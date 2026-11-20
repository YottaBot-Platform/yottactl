package command

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YottaBot-Platform/yottactl/internal/config"
)

func auditConfig(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv("YOTTA_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	t.Setenv("YOTTA_ENDPOINT", "")
	t.Setenv("YOTTA_TOKEN", "")
	c := &config.Config{CurrentContext: "a", Contexts: []config.Context{
		{Name: "a", Server: "https://a.example", Token: "a-token", AccountID: "1"},
		{Name: "b", Server: "https://b.example", Token: "b-token", AccountID: "2", Login: &config.LoginContext{PermissionSetID: "set"}},
	}}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAuditContextEdits(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		args                   []string
		server, token, account string
		login                  bool
	}{
		{"preserve", []string{"config", "set-context", "b"}, "https://b.example", "b-token", "2", true},
		{"same server", []string{"config", "set-context", "b", "--server", "https://b.example"}, "https://b.example", "b-token", "2", true},
		{"changed server", []string{"config", "set-context", "b", "--server", "https://new.example"}, "https://new.example", "", "2", false},
		{"changed account", []string{"config", "set-context", "b", "--account-id", "3"}, "https://b.example", "", "3", false},
		{"token noncurrent", []string{"auth", "token", "replacement", "--context", "b"}, "https://b.example", "replacement", "2", false},
		{"logout selected", []string{"auth", "logout", "--context", "b"}, "https://b.example", "", "2", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auditConfig(t)
			cmd := NewRoot()
			cmd.SetArgs(tc.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			c, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			b := c.Find("b")
			if b.Server != tc.server || b.Token != tc.token || b.AccountID != tc.account || (b.Login != nil) != tc.login {
				t.Fatalf("unexpected context: %#v", b)
			}
			if c.Find("a").Token != "a-token" {
				t.Fatal("changed unrelated context")
			}
		})
	}
}

func TestAuditUnknownContextNeverFallsThrough(t *testing.T) {
	auditConfig(t)
	for _, e := range []*Env{{ContextName: "missing", Endpoint: "https://override.example"}, {ContextName: "missing", Endpoint: "https://override.example", Token: "explicit"}} {
		if _, err := e.Client(); err == nil || !strings.Contains(err.Error(), "no context") {
			t.Fatalf("unknown context: %v", err)
		}
	}
}

func TestAuditRequestEncoding(t *testing.T) {
	auditConfig(t)
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r; w.Write([]byte(`{}`)) }))
	defer srv.Close()
	for _, tc := range []struct {
		args []string
		path string
	}{
		{[]string{"users", "get", "a?b#c/d"}, "/v1/identity/humans/users/a%3Fb%23c%2Fd"},
		{[]string{"groups", "remove-user", "a?b", "c/d"}, "/v1/identity/humans/groups/a%3Fb/users/c%2Fd"},
		{[]string{"github", "get-file", "owner/repo/file #?.go"}, "/v1/agent-platform/github/file/owner/repo/file%20%23%3F.go"},
		{[]string{"documents", "documents", "search"}, "/v1/documents/search"},
	} {
		got = nil
		cmd := NewRoot()
		cmd.SetArgs(append(tc.args, "--endpoint", srv.URL, "--token", "test", "--query", "q=a&b=c #d", "--query", "tag=one", "--query", "tag=two"))
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if got == nil || got.URL.EscapedPath() != tc.path {
			t.Fatalf("path: %v, want %s", got, tc.path)
		}
		if got.URL.Query().Get("q") != "a&b=c #d" || len(got.URL.Query()["tag"]) != 2 {
			t.Fatalf("query: %s", got.URL.RawQuery)
		}
	}
}

func TestAuditUnexpectedArgumentsDoNotCallAPI(t *testing.T) {
	auditConfig(t)
	for _, args := range [][]string{{"users", "list", "ignored"}, {"users", "create", "ignored"}} {
		cmd := NewRoot()
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatal("accepted extra positional argument")
		}
	}
}

func TestAuditBodyDecoding(t *testing.T) {
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{`{"id":9007199254740993,"ratio":0.1234567890123456789}`, true},
		{"id: 9007199254740993\n", true},
		{"", false}, {"null", false}, {"[]", false},
		{"name: first\n---\nname: silently-lost\n", false},
		{"nested:\n  1: first\n  '1': second\n", false},
	} {
		t.Run(tc.body, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "body")
			os.WriteFile(file, []byte(tc.body), 0600)
			body, err := readBody(file)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v: %v", tc.valid, err)
			}
			if tc.valid {
				b, err := json.Marshal(body)
				if err != nil || !strings.Contains(string(b), "9007199254740993") {
					t.Fatalf("lost integer: %s %v", b, err)
				}
			}
		})
	}
}

func TestAuditRawBodyAndConditionalHeaders(t *testing.T) {
	auditConfig(t)
	raw := []byte{0, 1, '\n', '{', 'x', '}', 255}
	file := filepath.Join(t.TempDir(), "payload")
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var received []byte
	var auth, match, contentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		auth = r.Header.Get("Authorization")
		match = r.Header.Get("If-Match")
		contentType = r.Header.Get("Content-Type")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	cmd := NewRoot()
	cmd.SetArgs([]string{"kms", "keys", "openpgp-sign", "key", "-f", file, "--endpoint", srv.URL, "--token", "test", "--header", "if-match=obsolete", "--header", "If-Match=\"revision-1\"", "--header", "Authorization=stolen"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, received) || contentType != "application/octet-stream" {
		t.Fatalf("raw payload changed: %q %s", received, contentType)
	}
	if auth != "Bearer test" || match != "\"revision-1\"" {
		t.Fatalf("headers: auth=%q match=%q", auth, match)
	}
}

func TestAuditAllCatalogToolsReachTheirCommand(t *testing.T) {
	root := NewRoot()
	for _, tool := range BuildCatalog().Tools {
		path := strings.Split(tool.Name, ".")
		cmd, remaining, err := root.Find(path)
		if err != nil || len(remaining) != 0 || cmd.RunE == nil || cmd.Name() != path[len(path)-1] {
			t.Errorf("%s is unreachable: command=%s remaining=%v err=%v", tool.Name, cmd.CommandPath(), remaining, err)
		}
	}
}

func TestAuditInvalidOutputCannotMutate(t *testing.T) {
	auditConfig(t)
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()
	cmd := NewRoot()
	cmd.SetArgs([]string{"users", "delete", "id", "--endpoint", srv.URL, "--token", "test", "--output", "invalid"})
	if err := cmd.Execute(); err == nil || called {
		t.Fatalf("invalid output performed mutation: called=%v err=%v", called, err)
	}
}

func TestAuditAuthHonorsLeadingGlobalFlags(t *testing.T) {
	auditConfig(t)
	cmd := NewRoot()
	cmd.SetArgs([]string{"--context", "b", "--endpoint", "https://override.example", "auth", "token", "replacement"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Find("b").Server != "https://override.example" || cfg.Find("b").Token != "replacement" || cfg.Find("a").Token != "a-token" {
		t.Fatal("auth token ignored leading global flags")
	}
}

func TestAuditDraftReadExposesETag(t *testing.T) {
	auditConfig(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"draft-7"`)
		w.Write([]byte(`{"definition":{}}`))
	}))
	defer srv.Close()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old; r.Close(); w.Close() }()
	cmd := NewRoot()
	cmd.SetArgs([]string{"workflows", "draft", "id", "--endpoint", srv.URL, "--token", "test", "-o", "json"})
	err = cmd.Execute()
	w.Close()
	os.Stdout = old
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	if body["etag"] != `"draft-7"` {
		t.Fatalf("ETag lost: %s", data)
	}
}
