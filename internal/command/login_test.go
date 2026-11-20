package command

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/YottaBot-Platform/yottactl/internal/config"
)

// A fake login.yotta.bot (login-auth's device grant, start page, Access keys
// and sign-out) and a fake environment (its exchange and one /v1 route), both
// over TLS, holding to the shapes #258 Step 12a and 8c serve.
type fakeLogin struct {
	t        *testing.T
	login    *httptest.Server
	env      *httptest.Server
	mu       sync.Mutex
	polls    []string // scripted answers to the token endpoint, in order
	pollN    int
	devices  []url.Values
	keys     []accessKeysCall
	exchange []exchangeCall
	apiAuth  []string
	signouts []string
	stray    []string // requests to any other address, on either server
	revoked  bool
	sets     []map[string]any // the acme-prod account's permission sets

	// Overrides, to see what yottactl refuses.
	discovery        map[string]any
	verificationURI  string
	keysTokenAddress string
	// directory serves login-directory's /v1/directory/ routes (org_test.go).
	directory http.HandlerFunc
}

type accessKeysCall struct {
	AccountID       int64  `json:"account_id"`
	PermissionSetID string `json:"permission_set_id"`
}

type exchangeCall struct {
	form          url.Values
	authorization string
	cookie        string
}

const cliSecret = "cli-secret"

func newFakeLogin(t *testing.T, polls ...string) *fakeLogin {
	t.Helper()
	f := &fakeLogin{t: t, polls: polls, sets: []map[string]any{
		{"id": "set-admin", "name": "Administrator"}, {"id": "set-ro", "name": "ReadOnly"},
	}}
	f.login = httptest.NewTLSServer(http.HandlerFunc(f.serveLogin))
	f.env = httptest.NewTLSServer(http.HandlerFunc(f.serveEnv))
	t.Cleanup(f.login.Close)
	t.Cleanup(f.env.Close)
	return f
}

func (f *fakeLogin) bearer(r *http.Request) bool {
	return r.Header.Get("Authorization") == "Bearer "+cliSecret && !f.revoked
}

func (f *fakeLogin) serveLogin(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	base := f.login.URL
	switch r.Method + " " + r.URL.Path {
	case "GET /.well-known/openid-configuration":
		d := map[string]any{"issuer": base, "token_endpoint": base + "/oauth/token", "device_authorization_endpoint": base + "/v1/login/device/code"}
		if f.discovery != nil {
			d = f.discovery
		}
		_ = json.NewEncoder(w).Encode(d)
	case "POST /v1/login/device/code":
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = r.ParseForm()
		f.devices = append(f.devices, r.PostForm)
		uri := base + "/device"
		if f.verificationURI != "" {
			uri = f.verificationURI
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"device_code": "dc-1", "user_code": "BCDF-GHJK", "verification_uri": uri, "expires_in": 600, "interval": 5})
	case "POST /oauth/token":
		_ = r.ParseForm()
		want := url.Values{"grant_type": {deviceGrantType}, "device_code": {"dc-1"}, "client_id": {"yottactl"}}
		if !reflect.DeepEqual(r.PostForm, want) || r.Header.Get("Authorization") != "" {
			f.t.Errorf("a poll sent %v (Authorization %q)", r.PostForm, r.Header.Get("Authorization"))
		}
		answer := "authorization_pending"
		if f.pollN < len(f.polls) {
			answer = f.polls[f.pollN]
		}
		f.pollN++
		switch answer {
		case "close":
			conn, _, _ := w.(http.Hijacker).Hijack()
			_ = conn.Close()
		case "ok":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": cliSecret, "token_type": "Bearer", "expires_in": 28800})
		default:
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": answer})
		}
	case "GET /v1/login/start":
		if !f.bearer(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"person": map[string]any{"id": "p-1", "email": "pat@acme.test", "name": "Pat"},
			"accounts": []map[string]any{
				{"id": 100000000101, "name": "acme-prod", "environment": f.env.URL, "status": "active", "registered": true, "permission_sets": f.sets},
				{"id": 100000000102, "name": "acme-staging", "environment": "https://staging.example.test", "status": "active", "registered": false,
					"permission_sets": []map[string]any{{"id": "set-dev", "name": "Developer"}}},
				{"id": 100000000103, "name": "acme-old", "environment": f.env.URL, "status": "suspended", "registered": true,
					"permission_sets": []map[string]any{{"id": "set-old", "name": "Developer"}}},
			},
		})
	case "POST /v1/login/access-keys":
		if !f.bearer(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var in accessKeysCall
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if r.Header.Get("Content-Type") != "application/json" || dec.Decode(&in) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.keys = append(f.keys, in)
		endpoint := f.env.URL + exchangePath
		if f.keysTokenAddress != "" {
			endpoint = f.keysTokenAddress
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id_token": "idt-" + strconv.Itoa(len(f.keys)), "token_endpoint": endpoint,
			"environment": f.env.URL, "account_id": "100000000101", "role": "admin", "expires_in": 300})
	case "POST /v1/login/signout":
		f.signouts = append(f.signouts, r.Header.Get("Authorization")+" "+r.Header.Get("Content-Type"))
		if r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		}
		if f.bearer(r) {
			f.revoked = true
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		if f.directory != nil && strings.HasPrefix(r.URL.Path, "/v1/directory/") {
			f.directory(w, r)
			return
		}
		f.stray = append(f.stray, "login "+r.Method+" "+r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeLogin) serveEnv(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method + " " + r.URL.Path {
	case "POST " + exchangePath:
		_ = r.ParseForm()
		f.exchange = append(f.exchange, exchangeCall{r.PostForm, r.Header.Get("Authorization"), r.Header.Get("Cookie")})
		if r.PostForm.Get("subject_token") != "idt-"+strconv.Itoa(len(f.keys)) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "env-" + strconv.Itoa(len(f.exchange)), "token_type": "Bearer", "expires_in": 3600})
	case "GET /v1/agents":
		f.apiAuth = append(f.apiAuth, r.Header.Get("Authorization"))
		_, _ = w.Write([]byte("[]"))
	default:
		f.stray = append(f.stray, "env "+r.Method+" "+r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

// fakeClock stands in for time and the browser; sleeping moves it forward.
type fakeClock struct {
	now    time.Time
	slept  []time.Duration
	opened []string
}

func useFakes(t *testing.T, f *fakeLogin) *fakeClock {
	t.Helper()
	t.Setenv("YOTTA_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	for _, k := range []string{"YOTTA_ENDPOINT", "YOTTA_TOKEN"} {
		t.Setenv(k, "")
	}
	clock := &fakeClock{now: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)}
	oldHTTP, oldOpen, oldSleep, oldNow, oldTransport := loginHTTP, openBrowser, loginSleep, loginNow, http.DefaultTransport
	c := f.login.Client() // both servers present httptest's one certificate
	c.CheckRedirect = loginHTTP.CheckRedirect
	loginHTTP = c
	http.DefaultTransport = c.Transport // the API client's, for the environment's /v1
	openBrowser = func(u string) error { clock.opened = append(clock.opened, u); return nil }
	loginSleep = func(d time.Duration) { clock.slept = append(clock.slept, d); clock.now = clock.now.Add(d) }
	loginNow = func() time.Time { return clock.now }
	t.Cleanup(func() {
		loginHTTP, openBrowser, loginSleep, loginNow, http.DefaultTransport = oldHTTP, oldOpen, oldSleep, oldNow, oldTransport
	})
	return clock
}

func runLoginCmd(f *fakeLogin, args ...string) (string, string, error) {
	var out, errOut bytes.Buffer
	cmd := newLoginCmd()
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(append([]string{"--issuer", f.login.URL}, args...))
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

func contextNames(cfg *config.Config) []string {
	var names []string
	for _, c := range cfg.Contexts {
		names = append(names, c.Name)
	}
	return names
}

func mustLoad(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestLoginWritesAContextPerAssignment(t *testing.T) {
	// A dropped connection backs off, slow_down adds five seconds (RFC 8628 §3.5).
	f := newFakeLogin(t, "close", "authorization_pending", "slow_down", "ok")
	clock := useFakes(t, f)

	out, errOut, err := runLoginCmd(f)
	if err != nil {
		t.Fatalf("login: %v\n%s", err, errOut)
	}
	if want := []time.Duration{5 * time.Second, 10 * time.Second, 10 * time.Second, 15 * time.Second}; !reflect.DeepEqual(clock.slept, want) {
		t.Errorf("polled after %v, want %v", clock.slept, want)
	}
	if len(f.devices) != 1 || f.devices[0].Get("client_id") != "yottactl" || len(f.devices[0]) != 2 {
		t.Errorf("the device request: %v", f.devices)
	}
	if host, _ := os.Hostname(); host != "" && f.devices[0].Get("device_name") == "" {
		t.Error("no device_name, though this computer has a name")
	}
	if !strings.Contains(errOut, "BCDF-GHJK") || !strings.Contains(errOut, f.login.URL+"/device") {
		t.Errorf("the code and the page were not printed:\n%s", errOut)
	}
	if !reflect.DeepEqual(clock.opened, []string{f.login.URL + "/device"}) {
		t.Errorf("opened %v", clock.opened)
	}

	cfg := mustLoad(t)
	if s := cfg.Login; s == nil || s.Token != cliSecret || s.Issuer != f.login.URL || s.Email != "pat@acme.test" ||
		!s.ExpiresAt.Equal(clock.now.Add(8*time.Hour)) {
		t.Fatalf("the CLI session: %+v", cfg.Login)
	}
	// Unregistered and suspended accounts get no context, as the start page
	// offers them no keys.
	if got := contextNames(cfg); !reflect.DeepEqual(got, []string{"acme-prod-administrator", "acme-prod-readonly"}) {
		t.Fatalf("contexts %v", got)
	}
	for i, set := range []string{"set-admin", "set-ro"} {
		c := cfg.Contexts[i]
		if c.Server != f.env.URL || c.AccountID != "100000000101" || c.Token != "" || c.Login == nil || c.Login.PermissionSetID != set {
			t.Errorf("context %d: %+v", i, c)
		}
	}
	if cfg.CurrentContext != "acme-prod-administrator" {
		t.Errorf("current context %q", cfg.CurrentContext)
	}
	p, _ := config.Path()
	if info, err := os.Stat(p); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the config file is %v, %v", info.Mode().Perm(), err)
	}
	if !strings.Contains(out, "Signed in as pat@acme.test") || !strings.Contains(out, "* acme-prod-administrator") {
		t.Errorf("output:\n%s", out)
	}
}

func TestALoginContextFetchesItsTokenAndKeepsIt(t *testing.T) {
	f := newFakeLogin(t, "ok")
	clock := useFakes(t, f)
	if _, errOut, err := runLoginCmd(f, "--no-browser"); err != nil {
		t.Fatalf("login: %v\n%s", err, errOut)
	}
	if len(clock.opened) != 0 {
		t.Errorf("--no-browser opened %v", clock.opened)
	}
	call := func(env *Env) error {
		c, err := env.Client()
		if err != nil {
			return err
		}
		status, _, err := c.Do("GET", "/v1/agents", nil)
		if err == nil && status != 200 {
			t.Errorf("GET /v1/agents: %d", status)
		}
		return err
	}

	// The first command trades: Access keys, then the environment's exchange.
	if err := call(&Env{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.keys, []accessKeysCall{{100000000101, "set-admin"}}) {
		t.Fatalf("Access keys calls %+v", f.keys)
	}
	want := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token_type": {"urn:ietf:params:oauth:token-type:id_token"}, "subject_token": {"idt-1"}}
	if len(f.exchange) != 1 || !reflect.DeepEqual(f.exchange[0].form, want) {
		t.Fatalf("exchange %+v", f.exchange)
	}
	if f.exchange[0].authorization != "" || f.exchange[0].cookie != "" {
		t.Errorf("the exchange was sent credentials: %+v", f.exchange[0])
	}
	if !reflect.DeepEqual(f.apiAuth, []string{"Bearer env-1"}) {
		t.Fatalf("the environment saw %v", f.apiAuth)
	}
	if c := mustLoad(t).Find("acme-prod-administrator"); c.Token != "env-1" || !c.Login.TokenExpiresAt.Equal(clock.now.Add(time.Hour)) {
		t.Fatalf("saved %+v %+v", c, c.Login)
	}

	// Kept while more than a minute is left; replaced after.
	clock.now = clock.now.Add(58 * time.Minute)
	if err := call(&Env{}); err != nil || len(f.keys) != 1 {
		t.Fatalf("a token with two minutes left was replaced (%d trades): %v", len(f.keys), err)
	}
	clock.now = clock.now.Add(90 * time.Second)
	if err := call(&Env{}); err != nil || len(f.keys) != 2 || f.apiAuth[2] != "Bearer env-2" {
		t.Fatalf("a token with 30 seconds left was kept (%d trades, %v): %v", len(f.keys), f.apiAuth, err)
	}

	// --context picks another set without changing the saved current one.
	if err := call(&Env{ContextName: "acme-prod-readonly"}); err != nil || f.keys[2].PermissionSetID != "set-ro" {
		t.Fatalf("--context: %+v %v", f.keys, err)
	}
	if cur := mustLoad(t).CurrentContext; cur != "acme-prod-administrator" {
		t.Errorf("--context changed the current context to %q", cur)
	}

	// The token never goes to another address.
	if _, err := (&Env{Endpoint: "https://elsewhere.example.test"}).Client(); err == nil || len(f.keys) != 3 {
		t.Fatalf("a login context's token was offered to another endpoint (%d trades): %v", len(f.keys), err)
	}
	// An explicit token is used as given, with no trade.
	if c, err := (&Env{Token: "mine"}).Client(); err != nil || c == nil || len(f.keys) != 3 {
		t.Fatalf("--token: %v", err)
	}
}

func TestALoginContextStopsWhenTheSignInEnds(t *testing.T) {
	f := newFakeLogin(t, "ok")
	clock := useFakes(t, f)
	if _, errOut, err := runLoginCmd(f, "--no-browser"); err != nil {
		t.Fatalf("login: %v\n%s", err, errOut)
	}
	// Revoked at login.yotta.bot: Access keys answers 401.
	f.revoked = true
	if _, err := (&Env{}).Client(); !errors.Is(err, errLoginEnded) {
		t.Fatalf("after revocation: %v", err)
	}
	// Past its eight hours: nothing is sent.
	f.revoked = false
	clock.now = clock.now.Add(8 * time.Hour)
	before := len(f.keys)
	if _, err := (&Env{}).Client(); !errors.Is(err, errLoginEnded) || len(f.keys) != before {
		t.Fatalf("after eight hours (%d calls): %v", len(f.keys)-before, err)
	}
}

func TestLoginRefusesAddressesItDoesNotTrust(t *testing.T) {
	t.Run("an issuer that is not https", func(t *testing.T) {
		f := newFakeLogin(t)
		useFakes(t, f)
		cmd := newLoginCmd()
		cmd.SetArgs([]string{"--issuer", "http://login.example.test"})
		cmd.SetErr(&bytes.Buffer{})
		var ee *ExitError
		if err := cmd.Execute(); !errors.As(err, &ee) || ee.Code != 2 {
			t.Fatalf("%v", err)
		}
	})
	t.Run("a token endpoint on another origin", func(t *testing.T) {
		f := newFakeLogin(t, "ok")
		useFakes(t, f)
		f.discovery = map[string]any{"issuer": f.login.URL, "token_endpoint": f.env.URL + "/oauth/token",
			"device_authorization_endpoint": f.login.URL + "/v1/login/device/code"}
		if _, _, err := runLoginCmd(f); err == nil || len(f.devices) != 0 {
			t.Fatalf("login went on (%d device requests): %v", len(f.devices), err)
		}
	})
	t.Run("a discovery document for another issuer", func(t *testing.T) {
		f := newFakeLogin(t, "ok")
		useFakes(t, f)
		f.discovery = map[string]any{"issuer": "https://login.example.test", "token_endpoint": f.login.URL + "/oauth/token",
			"device_authorization_endpoint": f.login.URL + "/v1/login/device/code"}
		if _, _, err := runLoginCmd(f); err == nil || len(f.devices) != 0 {
			t.Fatalf("login went on: %v", err)
		}
	})
	t.Run("an approval page on another origin", func(t *testing.T) {
		f := newFakeLogin(t, "ok")
		clock := useFakes(t, f)
		f.verificationURI = "https://login.example.test/device"
		if _, errOut, err := runLoginCmd(f); err == nil || len(clock.opened) != 0 || f.pollN != 0 || strings.Contains(errOut, "example.test") {
			t.Fatalf("opened %v, polled %d, printed %q: %v", clock.opened, f.pollN, errOut, err)
		}
	})
	t.Run("an exchange on another origin", func(t *testing.T) {
		f := newFakeLogin(t, "ok")
		useFakes(t, f)
		if _, errOut, err := runLoginCmd(f, "--no-browser"); err != nil {
			t.Fatalf("login: %v\n%s", err, errOut)
		}
		f.keysTokenAddress = f.login.URL + exchangePath
		if _, err := (&Env{}).Client(); err == nil || len(f.exchange) != 0 || len(f.stray) != 0 {
			t.Fatalf("the ID token went to %d exchanges and %v: %v", len(f.exchange), f.stray, err)
		}
		f.keysTokenAddress = f.env.URL + "/api/human-auth/v1/sso/other/token"
		if _, err := (&Env{}).Client(); err == nil || len(f.exchange) != 0 || len(f.stray) != 0 {
			t.Fatalf("the ID token went to %v: %v", f.stray, err)
		}
	})
}

func TestLoginStopsWhenTheCodeIsNotApproved(t *testing.T) {
	for _, tc := range []struct {
		polls     []string
		say       string
		wantPolls int
	}{
		{[]string{"access_denied"}, "denied", 1},
		{[]string{"authorization_pending", "expired_token"}, "expired", 2},
		{nil, "expired", 119}, // pending every 5s until the 600s run out
		{[]string{"invalid_client"}, "invalid_client", 1},
	} {
		f := newFakeLogin(t, tc.polls...)
		useFakes(t, f)
		_, _, err := runLoginCmd(f, "--no-browser")
		var ee *ExitError
		if !errors.As(err, &ee) || ee.Code != 2 || !strings.Contains(err.Error(), tc.say) || f.pollN != tc.wantPolls {
			t.Errorf("%v: %v after %d polls", tc.polls, err, f.pollN)
		}
		if cfg := mustLoad(t); cfg.Login != nil || len(cfg.Contexts) != 0 {
			t.Errorf("%v: wrote %+v", tc.polls, cfg)
		}
	}
}

func TestLoginWithAccountAndSetWritesOne(t *testing.T) {
	f := newFakeLogin(t, "ok", "ok", "ok", "ok")
	useFakes(t, f)
	if _, errOut, err := runLoginCmd(f, "--no-browser"); err != nil {
		t.Fatalf("login: %v\n%s", err, errOut)
	}

	// One context, made current; the others stay.
	if _, errOut, err := runLoginCmd(f, "--no-browser", "--account", "100000000101", "--permission-set", "readonly"); err != nil {
		t.Fatalf("login --account: %v\n%s", err, errOut)
	}
	cfg := mustLoad(t)
	if got := contextNames(cfg); len(got) != 2 || cfg.CurrentContext != "acme-prod-readonly" {
		t.Fatalf("contexts %v, current %q", got, cfg.CurrentContext)
	}

	// A set nobody holds any more goes on the next full login.
	f.sets = f.sets[:1]
	if _, errOut, err := runLoginCmd(f, "--no-browser"); err != nil {
		t.Fatalf("login: %v\n%s", err, errOut)
	}
	cfg = mustLoad(t)
	if got := contextNames(cfg); !reflect.DeepEqual(got, []string{"acme-prod-administrator"}) || cfg.CurrentContext != "acme-prod-administrator" {
		t.Fatalf("contexts %v, current %q", got, cfg.CurrentContext)
	}

	// An account with no keys to offer matches nothing.
	var ee *ExitError
	if _, _, err := runLoginCmd(f, "--no-browser", "--account", "acme-staging"); !errors.As(err, &ee) || ee.Code != 3 {
		t.Fatalf("--account acme-staging: %v", err)
	}
}

func TestLoginLeavesAContextItDidNotWrite(t *testing.T) {
	f := newFakeLogin(t, "ok")
	useFakes(t, f)
	cfg := &config.Config{}
	cfg.SetContext(config.Context{Name: "acme-prod-administrator", Server: "https://mine.example.test", Token: "mine"})
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	_, errOut, err := runLoginCmd(f, "--no-browser")
	if err != nil {
		t.Fatalf("login: %v\n%s", err, errOut)
	}
	cfg = mustLoad(t)
	if c := cfg.Find("acme-prod-administrator"); c.Token != "mine" || c.Server != "https://mine.example.test" || c.Login != nil {
		t.Fatalf("overwrote %+v", c)
	}
	if !strings.Contains(errOut, "skipped acme-prod-administrator") || cfg.CurrentContext != "acme-prod-readonly" {
		t.Fatalf("current %q, said:\n%s", cfg.CurrentContext, errOut)
	}
}

func TestLogoutRevokesAndForgets(t *testing.T) {
	f := newFakeLogin(t, "ok")
	useFakes(t, f)
	if _, errOut, err := runLoginCmd(f, "--no-browser"); err != nil {
		t.Fatalf("login: %v\n%s", err, errOut)
	}
	if _, err := (&Env{}).Client(); err != nil {
		t.Fatal(err)
	}
	cfg := mustLoad(t)
	upsertContext(cfg, config.Context{Name: "manual", Server: "https://x.example.test", Token: "t"})
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	logout := func() (string, error) {
		var out bytes.Buffer
		cmd := newLogoutCmd()
		cmd.SetOut(&out)
		cmd.SetArgs(nil)
		err := cmd.Execute()
		return out.String(), err
	}
	out, err := logout()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.signouts, []string{"Bearer cli-secret application/json"}) || !f.revoked {
		t.Fatalf("sign-out calls %v", f.signouts)
	}
	cfg = mustLoad(t)
	if cfg.Login != nil {
		t.Errorf("kept the CLI session: %+v", cfg.Login)
	}
	for _, c := range cfg.Contexts {
		if c.Login != nil && (c.Token != "" || !c.Login.TokenExpiresAt.IsZero()) {
			t.Errorf("kept %s's token", c.Name)
		}
	}
	if m := cfg.Find("manual"); m == nil || m.Token != "t" {
		t.Errorf("logout touched a context it did not write: %+v", m)
	}
	if !strings.Contains(out, "deleted 1 environment token") {
		t.Errorf("said %q", out)
	}
	if _, err := (&Env{}).Client(); !errors.Is(err, errLoginEnded) {
		t.Errorf("after logout: %v", err)
	}
	if out, err := logout(); err != nil || !strings.Contains(out, "not signed in") || len(f.signouts) != 1 {
		t.Errorf("a second logout: %q %v", out, err)
	}
}

func TestLogoutForgetsEvenWhenLoginIsUnreachable(t *testing.T) {
	f := newFakeLogin(t, "ok")
	useFakes(t, f)
	if _, errOut, err := runLoginCmd(f, "--no-browser"); err != nil {
		t.Fatalf("login: %v\n%s", err, errOut)
	}
	f.login.Close()
	cmd := newLogoutCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs(nil)
	var ee *ExitError
	if err := cmd.Execute(); !errors.As(err, &ee) || ee.Code != 1 || !strings.Contains(err.Error(), "lasts until") {
		t.Fatalf("logout: %v", err)
	}
	if mustLoad(t).Login != nil {
		t.Error("kept the CLI session")
	}
}
