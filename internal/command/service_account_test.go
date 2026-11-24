package command

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/YottaBot-Platform/yottactl/internal/config"
)

const testServiceAccount = "sa-33400000-0000-4000-8000-000000000011"

// machineLogin is login.yotta.bot for a service account: it checks every
// assertion as login-auth does (the key on file by its ID, iss and sub,
// the token endpoint as aud, at most five minutes, a jti once), trades a
// GitHub token for a token under a trust policy, and serves Identity's and
// Boo · Admin's routes to a token for each.
type machineLogin struct {
	t      *testing.T
	srv    *httptest.Server
	mu     sync.Mutex
	keys   map[string]ed25519.PublicKey // by key ID
	jtis   map[string]bool
	tokens map[string]string // token -> audience
	minted map[string]int    // audience -> tokens minted
	github int
	refuse bool
	endAll bool // every token it issues is ended at once
}

func newMachineLogin(t *testing.T) *machineLogin {
	m := &machineLogin{t: t, keys: map[string]ed25519.PublicKey{}, jtis: map[string]bool{}, tokens: map[string]string{}, minted: map[string]int{}}
	m.srv = httptest.NewTLSServer(http.HandlerFunc(m.serve))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *machineLogin) serve(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	reply := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	issue := func(audience string) {
		tok := fmt.Sprintf("machine-%d", len(m.tokens)+1)
		m.tokens[tok], m.minted[audience] = audience, m.minted[audience]+1
		reply(200, map[string]any{"access_token": tok, "token_type": "Bearer", "expires_in": 900})
	}
	audiences := map[string]bool{m.srv.URL + "/v1/directory": true, m.srv.URL + "/v1/login/boo-admin": true}
	switch r.URL.Path {
	case "/oauth/token":
		_ = r.ParseForm()
		if m.refuse || r.PostFormValue("grant_type") != "client_credentials" || r.PostFormValue("client_assertion_type") != clientAssertionType || !audiences[r.PostFormValue("audience")] {
			reply(401, map[string]string{"error": "invalid_client"})
			return
		}
		parts := strings.Split(r.PostFormValue("client_assertion"), ".")
		var h struct{ Alg, Kid, Typ string }
		var c struct {
			Iss, Sub, Aud, Jti string
			Iat, Exp           int64
		}
		hb, _ := base64.RawURLEncoding.DecodeString(parts[0])
		cb, _ := base64.RawURLEncoding.DecodeString(parts[1])
		sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
		_ = json.Unmarshal(hb, &h)
		_ = json.Unmarshal(cb, &c)
		pub, ok := m.keys[h.Kid]
		switch {
		case len(parts) != 3 || h.Alg != "EdDSA" || !ok || !ed25519.Verify(pub, []byte(parts[0]+"."+parts[1]), sig),
			c.Iss != testServiceAccount || c.Sub != c.Iss, c.Aud != m.srv.URL+"/oauth/token",
			c.Exp-c.Iat > 300 || c.Exp <= c.Iat, c.Jti == "" || m.jtis[c.Jti]:
			reply(401, map[string]string{"error": "invalid_client"})
			return
		}
		m.jtis[c.Jti] = true
		issue(r.PostFormValue("audience"))
	case "/oauth/federated-token":
		_ = r.ParseForm()
		if r.PostFormValue("grant_type") != tokenExchangeGrant || r.PostFormValue("subject_token_type") != idTokenType ||
			r.PostFormValue("subject_token") != "github-job-token" || r.PostFormValue("client_id") != testServiceAccount {
			reply(401, map[string]string{"error": "invalid_grant"})
			return
		}
		issue(r.PostFormValue("audience"))
	case "/github-token":
		// GitHub Actions' token service: the job's request token, and the
		// audience login.yotta.bot asks for.
		if r.Header.Get("Authorization") != "Bearer gh-request" || r.URL.Query().Get("audience") != m.srv.URL || r.URL.Query().Get("api-version") != "2.0" {
			reply(403, map[string]string{})
			return
		}
		m.github++
		reply(200, map[string]string{"value": "github-job-token"})
	case "/v1/directory/people", "/v1/login/boo-admin/balance":
		want := m.srv.URL + "/v1/directory"
		if strings.HasPrefix(r.URL.Path, "/v1/login/boo-admin") {
			want = m.srv.URL + "/v1/login/boo-admin"
		}
		if m.endAll || m.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")] != want {
			reply(401, map[string]string{"error": "not signed in"})
			return
		}
		if r.URL.Path == "/v1/directory/people" {
			reply(200, map[string]any{"people": []map[string]string{{"id": "p1", "email": "pat@acme.test"}}})
			return
		}
		reply(200, map[string]any{"balance": map[string]int{"available_micros": 5}})
	default:
		reply(404, map[string]string{"error": "not found"})
	}
}

// useMachine points yottactl at m, with a fresh config and none of a
// pipeline's variables.
func useMachine(t *testing.T, m *machineLogin) {
	t.Helper()
	t.Setenv("YOTTA_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	for _, k := range []string{"YOTTA_ENDPOINT", "YOTTA_TOKEN", "YOTTA_SERVICE_ACCOUNT", "YOTTA_PRIVATE_KEY", "YOTTA_PRIVATE_KEY_FILE", "YOTTA_KEY_ID",
		"ACTIONS_ID_TOKEN_REQUEST_URL", "ACTIONS_ID_TOKEN_REQUEST_TOKEN"} {
		t.Setenv(k, "")
	}
	t.Setenv("YOTTA_ISSUER", m.srv.URL)
	old, oldNow := loginHTTP, loginNow
	c := m.srv.Client()
	c.CheckRedirect = loginHTTP.CheckRedirect
	loginHTTP, loginNow = c, time.Now
	machineTokens = map[string]machineToken{}
	t.Cleanup(func() { loginHTTP, loginNow, machineTokens = old, oldNow, map[string]machineToken{} })
}

// keygen makes a key with `org service-accounts keygen` and puts its public
// half on file at m, under the key ID keygen printed.
func keygen(t *testing.T, m *machineLogin) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "terraform.pem")
	out, errOut, err := runOrg("service-accounts", "keygen", "--out", file)
	if err != nil {
		t.Fatalf("keygen: %v %s", err, errOut)
	}
	var made struct {
		PublicKey string `json:"public_key"`
		KeyID     string `json:"key_id"`
	}
	if err := json.Unmarshal([]byte(out), &made); err != nil {
		t.Fatalf("keygen's output: %v %s", err, out)
	}
	pub, _ := base64.RawURLEncoding.DecodeString(made.PublicKey)
	if len(pub) != ed25519.PublicKeySize || made.KeyID != keyThumbprint(pub) {
		t.Fatalf("keygen's key: %+v", made)
	}
	m.keys[made.KeyID] = pub
	return file
}

// The key is made here, private only to its file, and its ID is its own
// thumbprint, which the server recomputes from what it was given.
func TestKeygen(t *testing.T) {
	m := newMachineLogin(t)
	useMachine(t, m)
	file := keygen(t, m)
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the key file: %v %v", info, err)
	}
	raw, _ := os.ReadFile(file)
	if _, err := parsePrivateKey(raw); err != nil {
		t.Errorf("the key file doesn't read back: %v", err)
	}
	if _, _, err := runOrg("service-accounts", "keygen", "--out", file); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Errorf("keygen over a file that exists: %v", err)
	}
	// RFC 7638's own Ed25519 example (appendix A of RFC 8037).
	pub, _ := base64.RawURLEncoding.DecodeString("11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo")
	if got := keyThumbprint(pub); got != "kPrK_qmxVWaYVA9wwBF6Iuo3vVzz7TxHCTwXBygrS4k" {
		t.Errorf("thumbprint: %s", got)
	}
}

// From a pipeline's variables: a token for each surface, each made once per
// run, and each sent only to its own surface.
func TestServiceAccountFromTheEnvironment(t *testing.T) {
	m := newMachineLogin(t)
	useMachine(t, m)
	file := keygen(t, m)
	raw, _ := os.ReadFile(file)
	t.Setenv("YOTTA_SERVICE_ACCOUNT", strings.TrimPrefix(testServiceAccount, "sa-"))
	t.Setenv("YOTTA_PRIVATE_KEY", string(raw))
	for i := 0; i < 2; i++ {
		if out, errOut, err := runOrg("users", "list"); err != nil || !strings.Contains(out, "pat@acme.test") {
			t.Fatalf("users list: %v %s %s", err, out, errOut)
		}
	}
	if out, _, err := runOrg("boo", "balance"); err != nil || !strings.Contains(out, "available_micros") {
		t.Fatalf("boo balance: %v %s", err, out)
	}
	if m.minted[m.srv.URL+"/v1/directory"] != 1 || m.minted[m.srv.URL+"/v1/login/boo-admin"] != 1 {
		t.Errorf("tokens made: %v, want one for each surface", m.minted)
	}
	// A pipeline's tokens last the run: nothing is written to disk.
	if raw, err := os.ReadFile(os.Getenv("YOTTA_CONFIG")); err == nil && bytes.Contains(raw, []byte("machine-")) {
		t.Errorf("a pipeline's token was kept on disk:\n%s", raw)
	}
	out, _, err := runOrg("token", "--surface", "boo-admin")
	if err != nil || m.tokens[strings.TrimSpace(out)] != m.srv.URL+"/v1/login/boo-admin" {
		t.Errorf("org token --surface boo-admin: %v %q", err, out)
	}
	// A key login.yotta.bot doesn't hold is refused, and says what to check.
	other := filepath.Join(t.TempDir(), "other.pem")
	if _, _, err := runOrg("service-accounts", "keygen", "--out", other); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(other)
	t.Setenv("YOTTA_PRIVATE_KEY", string(raw))
	machineTokens = map[string]machineToken{}
	if _, _, err := runOrg("users", "list"); err == nil || !strings.Contains(err.Error(), "its key is one of its keys in Identity") {
		t.Errorf("an unknown key: %v", err)
	}
	t.Setenv("YOTTA_PRIVATE_KEY", "")
	if _, _, err := runOrg("users", "list"); err == nil || !strings.Contains(err.Error(), "YOTTA_PRIVATE_KEY") {
		t.Errorf("a service account with no key and no GitHub: %v", err)
	}
}

// In GitHub Actions, no key: the job's own token, under a trust policy.
func TestServiceAccountInGitHubActions(t *testing.T) {
	m := newMachineLogin(t)
	useMachine(t, m)
	t.Setenv("YOTTA_SERVICE_ACCOUNT", testServiceAccount)
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", m.srv.URL+"/github-token?api-version=2.0")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "gh-request")
	if out, errOut, err := runOrg("users", "list"); err != nil || !strings.Contains(out, "pat@acme.test") {
		t.Fatalf("users list in GitHub Actions: %v %s %s", err, out, errOut)
	}
	if m.github != 1 {
		t.Errorf("asked GitHub %d times", m.github)
	}
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "stale")
	machineTokens = map[string]machineToken{}
	if _, _, err := runOrg("users", "list"); err == nil || !strings.Contains(err.Error(), "id-token: write") {
		t.Errorf("GitHub refusing the job: %v", err)
	}
}

// `yottactl login --service-account` keeps where the key is, never the key;
// a person's sign-in has to end first; logout forgets it.
func TestServiceAccountLogin(t *testing.T) {
	m := newMachineLogin(t)
	useMachine(t, m)
	file := keygen(t, m)
	t.Setenv("YOTTA_ISSUER", "")
	login := func(args ...string) error {
		var out, errOut bytes.Buffer
		cmd := newLoginCmd()
		cmd.SilenceUsage, cmd.SilenceErrors = true, true
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		cmd.SetArgs(args)
		return cmd.Execute()
	}
	// A person signed in: one way at a time.
	cfg, _ := config.Load()
	cfg.Login = &config.LoginSession{Issuer: m.srv.URL, Token: "person", ExpiresAt: time.Now().Add(time.Hour), Email: "pat@acme.test"}
	_ = cfg.Save()
	if err := login("--service-account", testServiceAccount, "--key", file, "--issuer", m.srv.URL); err == nil || !strings.Contains(err.Error(), "logout") {
		t.Fatalf("over a person's sign-in: %v", err)
	}
	cfg.Login = nil
	_ = cfg.Save()
	if err := login("--key", file); err == nil {
		t.Error("--key without --service-account")
	}
	if err := login("--service-account", "terraform", "--key", file, "--issuer", m.srv.URL); err == nil {
		t.Error("a name, not an ID")
	}
	if err := login("--service-account", testServiceAccount, "--key", file, "--issuer", m.srv.URL); err != nil {
		t.Fatalf("login: %v", err)
	}
	cfg, _ = config.Load()
	raw, _ := os.ReadFile(os.Getenv("YOTTA_CONFIG"))
	if cfg.ServiceAccount == nil || cfg.ServiceAccount.ClientID != testServiceAccount || cfg.ServiceAccount.KeyFile != file || cfg.Login != nil ||
		bytes.Contains(raw, []byte("PRIVATE KEY")) {
		t.Fatalf("saved: %+v\n%s", cfg.ServiceAccount, raw)
	}
	// Each command is a new run: it uses the token login was issued, kept
	// in the config (0600) until a minute before it ends, as the AWS CLI keeps
	// its own, and doesn't sign in again.
	directory := m.srv.URL + "/v1/directory"
	for i := 0; i < 3; i++ {
		machineTokens = map[string]machineToken{}
		if out, _, err := runOrg("users", "list"); err != nil || !strings.Contains(out, "pat@acme.test") {
			t.Fatalf("users list after login: %v %s", err, out)
		}
	}
	if m.minted[directory] != 1 {
		t.Errorf("signed in %d times for three commands, want once", m.minted[directory])
	}
	cfg, _ = config.Load()
	if st, err := os.Stat(os.Getenv("YOTTA_CONFIG")); err != nil || st.Mode().Perm() != 0o600 ||
		cfg.ServiceAccount.Tokens[directory].Token == "" || !cfg.ServiceAccount.Tokens[directory].ExpiresAt.After(time.Now()) {
		t.Fatalf("kept token: %+v", cfg.ServiceAccount.Tokens)
	}
	// One within a minute of its end is replaced.
	cfg.ServiceAccount.Tokens[directory] = config.SavedToken{Token: cfg.ServiceAccount.Tokens[directory].Token, ExpiresAt: time.Now().Add(30 * time.Second)}
	_ = cfg.Save()
	machineTokens = map[string]machineToken{}
	if _, _, err := runOrg("users", "list"); err != nil || m.minted[directory] != 2 {
		t.Fatalf("a token about to end: %v, minted %d", err, m.minted[directory])
	}
	// A kept token ended in Identity: its next request is refused, and the
	// key signs in again once (ending a sign-in ends that token, not the key).
	m.tokens = map[string]string{}
	machineTokens = map[string]machineToken{}
	if out, _, err := runOrg("users", "list"); err != nil || !strings.Contains(out, "pat@acme.test") || m.minted[directory] != 3 {
		t.Fatalf("after its token ended: %v %s, minted %d", err, out, m.minted[directory])
	}
	// Its key revoked: refused, and said as such.
	m.tokens, m.refuse = map[string]string{}, true
	machineTokens = map[string]machineToken{}
	if _, _, err := runOrg("users", "list"); err == nil || !strings.Contains(err.Error(), "not revoked") {
		t.Errorf("a revoked key: %v", err)
	}
	m.refuse = false
	// A token refused the moment it was issued (ended in Identity at once):
	// said as such, not as a person's, and not tried again.
	m.tokens = map[string]string{}
	cfg, _ = config.Load()
	cfg.ServiceAccount.Tokens = nil
	_ = cfg.Save()
	machineTokens = map[string]machineToken{}
	m.endAll = true
	if _, _, err := runOrg("users", "list"); err == nil || !strings.Contains(err.Error(), "ended in Identity") {
		t.Errorf("an ended token: %v", err)
	}
	m.endAll = false
	var out bytes.Buffer
	logout := newLogoutCmd()
	logout.SetOut(&out)
	logout.SetArgs(nil)
	if err := logout.Execute(); err != nil || !strings.Contains(out.String(), "Forgot "+testServiceAccount) {
		t.Fatalf("logout: %v %s", err, out.String())
	}
	if cfg, _ = config.Load(); cfg.ServiceAccount != nil {
		t.Error("logout kept the service account")
	}
	if _, err := os.Stat(file); err != nil {
		t.Error("logout removed the key file")
	}
}

// A key file that isn't an Ed25519 PKCS #8 key is refused before anything is sent.
func TestParsePrivateKey(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(priv)
	good := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if k, err := parsePrivateKey(good); err != nil || !k.Equal(priv) {
		t.Fatalf("a good key: %v", err)
	}
	for name, raw := range map[string][]byte{"empty": nil, "not PEM": []byte("hello"), "a public key": pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}),
		"not PKCS #8": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("junk")})} {
		if _, err := parsePrivateKey(raw); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for _, bad := range []string{"terraform", "sa-x", ""} {
		if _, err := serviceAccountClientID(bad); err == nil {
			t.Errorf("%q named a service account", bad)
		}
	}
	if id, err := serviceAccountClientID("SA-33400000-0000-4000-8000-000000000011"); err != nil || id != testServiceAccount {
		t.Errorf("an upper-case client ID: %q %v", id, err)
	}
}
