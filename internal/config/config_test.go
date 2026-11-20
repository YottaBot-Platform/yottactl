package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("YOTTA_CONFIG", filepath.Join(dir, "config.yaml"))

	// Missing file → empty config, no error.
	c, err := Load()
	if err != nil {
		t.Fatalf("Load on missing file: %v", err)
	}
	if len(c.Contexts) != 0 || c.Current() != nil {
		t.Fatal("fresh config should be empty")
	}

	// Upsert two contexts; second becomes current.
	c.SetContext(Context{Name: "acme-prod", Server: "https://acme.yotta.bot", Token: "tok-a"})
	c.SetContext(Context{Name: "acme-staging", Server: "https://acme-staging.yotta.bot", Token: "tok-b"})
	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Reload and verify.
	c2, err := Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if c2.CurrentContext != "acme-staging" {
		t.Errorf("current = %q, want acme-staging", c2.CurrentContext)
	}
	if cur := c2.Current(); cur == nil || cur.Token != "tok-b" {
		t.Errorf("current context token wrong: %+v", cur)
	}

	// use-context to the other one.
	if err := c2.UseContext("acme-prod"); err != nil {
		t.Fatalf("UseContext: %v", err)
	}
	if c2.Current().Server != "https://acme.yotta.bot" {
		t.Error("UseContext didn't switch")
	}

	// Unknown context errors.
	if err := c2.UseContext("nope"); err == nil {
		t.Error("UseContext on unknown name should error")
	}

	// File perms are 0600 (holds tokens).
	info, err := os.Stat(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config perms = %o, want 600", info.Mode().Perm())
	}
}

// SetContext on an existing name updates in place (no dup row).
func TestSetContext_UpsertsInPlace(t *testing.T) {
	c := &Config{}
	c.SetContext(Context{Name: "x", Token: "old"})
	c.SetContext(Context{Name: "x", Token: "new"})
	if len(c.Contexts) != 1 {
		t.Fatalf("got %d contexts, want 1 (upsert)", len(c.Contexts))
	}
	if c.Contexts[0].Token != "new" {
		t.Errorf("token = %q, want new", c.Contexts[0].Token)
	}
}

// `yottactl login`'s CLI session and the contexts it writes survive a round
// trip, and saving tightens a file someone left readable (#258 Step 12c).
func TestLoginStateRoundTripsAndStaysPrivate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("YOTTA_CONFIG", p)
	if err := os.WriteFile(p, []byte("current-context: \"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 15, 20, 0, 0, 0, time.UTC)
	c := &Config{Login: &LoginSession{Issuer: "https://login.yotta.bot", Token: "cli", ExpiresAt: at, Email: "pat@acme.test"}}
	c.SetContext(Context{Name: "acme-prod-admin", Server: "https://acme.yotta.bot", AccountID: "100000000101",
		Login: &LoginContext{PermissionSetID: "set-1", TokenExpiresAt: at.Add(-7 * time.Hour)}})
	c.SetContext(Context{Name: "manual", Server: "https://x.test", Token: "t"})
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("after Save the file is %v, %v; want 0600", info.Mode().Perm(), err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Login == nil || got.Login.Token != "cli" || !got.Login.ExpiresAt.Equal(at) || got.Login.Email != "pat@acme.test" {
		t.Fatalf("the CLI session: %+v", got.Login)
	}
	lc := got.Find("acme-prod-admin")
	if lc == nil || lc.Login == nil || lc.Login.PermissionSetID != "set-1" || !lc.Login.TokenExpiresAt.Equal(at.Add(-7*time.Hour)) {
		t.Fatalf("the login context: %+v", lc)
	}
	if m := got.Find("manual"); m == nil || m.Login != nil {
		t.Fatalf("a manual context grew a login block: %+v", m)
	}

	got.Remove("manual")
	if got.Find("manual") != nil || got.CurrentContext != "" {
		t.Fatalf("Remove left %+v, current %q", got.Contexts, got.CurrentContext)
	}
	got.Remove("nobody")
	if len(got.Contexts) != 1 {
		t.Fatalf("removing an unknown name changed the list: %+v", got.Contexts)
	}
}
