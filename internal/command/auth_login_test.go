package command

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YottaBot-Platform/yottactl/internal/config"
)

func TestAuthLogin_StoresToken(t *testing.T) {
	t.Setenv("YOTTA_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/login" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"token":"jwt-abc","account_id":100000000000,"expires_in":3600}`))
	}))
	defer srv.Close()

	cmd := newAuthCmd(&Env{})
	cmd.SetArgs([]string{"login",
		"--email", "you@example.com", "--password", "pw",
		"--context", "acme-staging", "--endpoint", "http://gw:8081",
		"--auth-endpoint", srv.URL})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if !strings.Contains(gotBody, `"email":"you@example.com"`) || !strings.Contains(gotBody, `"password":"pw"`) {
		t.Errorf("login body = %s", gotBody)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	cur := cfg.Current()
	if cur == nil {
		t.Fatal("no current context after login")
	}
	if cur.Name != "acme-staging" || cur.Token != "jwt-abc" || cur.Server != "http://gw:8081" || cur.AccountID != "100000000000" {
		t.Errorf("stored context = %+v", cur)
	}
}

func TestAuthLogin_PasswordStdin(t *testing.T) {
	t.Setenv("YOTTA_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`{"token":"t","account_id":1}`))
	}))
	defer srv.Close()

	cmd := newAuthCmd(&Env{})
	cmd.SetIn(strings.NewReader("secretpw\n"))
	cmd.SetArgs([]string{"login", "--email", "you@example.com", "--password-stdin", "--auth-endpoint", srv.URL})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	// trailing newline trimmed
	if !strings.Contains(gotBody, `"password":"secretpw"`) {
		t.Errorf("stdin password not forwarded/trimmed: %s", gotBody)
	}
}

func TestAuthLogin_BadCredsError(t *testing.T) {
	t.Setenv("YOTTA_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	cmd := newAuthCmd(&Env{})
	cmd.SetArgs([]string{"login", "--email", "x@y.z", "--password", "bad", "--auth-endpoint", srv.URL})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error on 401 login")
	}
}

func TestAuthLogin_RequiresEmail(t *testing.T) {
	cmd := newAuthCmd(&Env{})
	cmd.SetArgs([]string{"login", "--password", "x", "--auth-endpoint", "http://x"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when email missing")
	}
}

func TestAuthLogin_RequiresPassword(t *testing.T) {
	cmd := newAuthCmd(&Env{})
	cmd.SetArgs([]string{"login", "--email", "you@example.com", "--auth-endpoint", "http://x"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when password missing")
	}
}
