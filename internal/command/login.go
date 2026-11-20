package command

// `yottactl login` and `yottactl logout` (#258 Step 12c; the
// owner's answers, 2026-10-15).
//
// login signs yottactl in at login.yotta.bot with the OAuth device grant (RFC
// 8628): it prints an 8-letter code and opens login.yotta.bot/device, where
// the person signs in (with two-factor where their organization requires it)
// and approves the code. The answer is an 8-hour CLI session that can list the
// person's assignments and mint Access keys' 5-minute ID token, and, for an
// organization administrator, manage the apps' client secrets (`yottactl org`,
// #279 C); nothing else.
// login writes one context per account and permission set, named as the
// Access keys modal names it, with the environment's own origin as its server.
//
// A command against such a context trades for the environment's own token the
// way Access keys does (the ID token, then the environment's exchange) and
// keeps it until a minute before it expires. The CLI session goes only to
// login.yotta.bot and the environment's token only to its own environment;
// every address either one is sent to is checked first.
//
// logout revokes the CLI session and deletes every token login wrote.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/YottaBot-Platform/yottactl/internal/config"
	"github.com/spf13/cobra"
)

const (
	defaultLoginIssuer = "https://login.yotta.bot"
	// loginClientID is login-auth's one public client (oauth.CLIClientID).
	loginClientID   = "yottactl"
	deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"
	// The environment's exchange, as Access keys calls it
	// (access-keys.cjs ACCESS_KEYS_EXCHANGE_PATH).
	exchangePath = "/api/human-auth/v1/sso/yotta-login/token"
	// refreshBefore: a context's token is replaced this long before it expires.
	refreshBefore = time.Minute
	// RFC 8628 §3.5: slow_down adds five seconds; a connection error backs off.
	slowDownStep    = 5 * time.Second
	maxPollInterval = 60 * time.Second
	maxLoginBody    = 1 << 20
	maxDeviceName   = 64
)

// Seams for tests: the HTTP client, the browser, the clock.
var (
	loginHTTP = &http.Client{
		Timeout: 30 * time.Second,
		// A redirect is never followed: every address here is checked before a
		// secret is sent to it, and a redirect would skip that check.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	openBrowser = openInBrowser
	loginSleep  = time.Sleep
	loginNow    = time.Now
)

// errLoginEnded is what a command against a login context says once the CLI
// session is gone.
var errLoginEnded = errors.New("yottactl's sign-in at the Access Portal has ended — run `yottactl login`")

func newLoginCmd() *cobra.Command {
	var issuer, account, set string
	var noBrowser bool
	cmd := &cobra.Command{
		Use:     "login",
		Short:   "Sign in at the Access Portal and write a context for each permission set you hold",
		GroupID: groupID("Core"),
		Long: `Sign in at the Access Portal with a code you approve in your browser.

yottactl prints an 8-letter code and opens the Access Portal's device page.
Sign in there, check the computer's name, and approve the code. yottactl then holds an
8-hour sign-in that can only list your permission sets and fetch keys for them.

It writes one context per account and permission set you hold, named as the
Access keys window names it. A command against one of them fetches that
environment's own token (an hour at most) and fetches a new one when it runs
out, until the 8 hours end. --account and --permission-set write just one.

Examples:
  yottactl login
  yottactl login --account acme-prod --permission-set Administrator
  yottactl config use-context acme-prod-administrator`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogin(cmd, issuer, account, set, noBrowser)
		},
	}
	cmd.Flags().StringVar(&issuer, "issuer", defaultLoginIssuer, "the Access Portal address (another only for a self-hosted sign-in)")
	cmd.Flags().StringVar(&account, "account", "", "write the context for this account only (its name or id)")
	cmd.Flags().StringVar(&set, "permission-set", "", "write the context for this permission set only (its name or id)")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "print the address instead of opening a browser")
	return cmd
}

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "logout",
		Short:   "Sign yottactl out of the Access Portal and delete the tokens it fetched",
		GroupID: groupID("Core"),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogout(cmd)
		},
	}
}

// ---- login ----------------------------------------------------------------

type loginDiscovery struct {
	Issuer                      string `json:"issuer"`
	TokenEndpoint               string `json:"token_endpoint"`
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
}

type deviceCodeAnswer struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

type loginStart struct {
	Person struct {
		Email string `json:"email"`
	} `json:"person"`
	Accounts []struct {
		ID             int64  `json:"id"`
		Name           string `json:"name"`
		Environment    string `json:"environment"`
		Status         string `json:"status"`
		Registered     bool   `json:"registered"`
		PermissionSets []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"permission_sets"`
	} `json:"accounts"`
}

var userCodeShape = regexp.MustCompile(`^[A-Z]{4}-[A-Z]{4}$`)

func runLogin(cmd *cobra.Command, issuerFlag, accountFlag, setFlag string, noBrowser bool) error {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
	issuer, ok := httpsOrigin(issuerFlag)
	if !ok {
		return &ExitError{Code: 2, Msg: "--issuer must be an https origin, like " + defaultLoginIssuer}
	}
	cfg, err := config.Load()
	if err != nil {
		return &ExitError{Code: 1, Msg: err.Error()}
	}

	d, err := discoverLogin(issuer)
	if err != nil {
		return &ExitError{Code: 1, Msg: err.Error()}
	}
	dc, err := requestDeviceCode(d, issuer)
	if err != nil {
		return &ExitError{Code: 1, Msg: err.Error()}
	}
	fmt.Fprintf(errOut, "To sign in, open %s and enter this code:\n\n    %s\n\nThe code works for %d minutes. Approve it only if you started it here.\n",
		dc.VerificationURI, dc.UserCode, (dc.ExpiresIn+59)/60)
	if !noBrowser {
		if err := openBrowser(dc.VerificationURI); err != nil {
			fmt.Fprintln(errOut, "(Couldn't open a browser; open the address yourself.)")
		}
	}
	fmt.Fprintln(errOut, "\nWaiting for you to approve it…")

	session, err := pollDeviceCode(d, dc)
	if err != nil {
		return &ExitError{Code: 2, Msg: err.Error()}
	}

	status, body, err := loginCall(http.MethodGet, issuer+"/v1/login/start", session.Token, nil, nil)
	if err != nil {
		return &ExitError{Code: 1, Msg: "list your permission sets: " + err.Error()}
	}
	if status != http.StatusOK {
		return &ExitError{Code: exitForStatus(status), Msg: fmt.Sprintf("list your permission sets: HTTP %d %s", status, loginErrorCode(body))}
	}
	var start loginStart
	if err := json.Unmarshal(body, &start); err != nil {
		return &ExitError{Code: 1, Msg: "read your permission sets: " + err.Error()}
	}

	previous := cfg.Login
	session.Issuer = issuer
	session.Email = start.Person.Email
	cfg.Login = &session

	type written struct{ name, server string }
	var wrote []written
	names := map[string]bool{}
	for _, a := range start.Accounts {
		if accountFlag != "" && accountFlag != a.Name && accountFlag != strconv.FormatInt(a.ID, 10) {
			continue
		}
		// What the start page offers keys for (start.jsx `trusting`).
		if !a.Registered || a.Status != "active" {
			continue
		}
		server, ok := httpsOrigin(a.Environment)
		if !ok {
			fmt.Fprintf(errOut, "skipped %s: its environment address %q is not https\n", a.Name, a.Environment)
			continue
		}
		for _, ps := range a.PermissionSets {
			if setFlag != "" && !strings.EqualFold(setFlag, ps.Name) && setFlag != ps.ID {
				continue
			}
			name := contextName(a.Name, ps.Name)
			if names[name] {
				fmt.Fprintf(errOut, "skipped %s / %s: another permission set is also named %q\n", a.Name, ps.Name, name)
				continue
			}
			if existing := cfg.Find(name); existing != nil && existing.Login == nil {
				fmt.Fprintf(errOut, "skipped %s: a context of that name exists that yottactl login did not write\n", name)
				continue
			}
			names[name] = true
			upsertContext(cfg, config.Context{Name: name, Server: server, AccountID: strconv.FormatInt(a.ID, 10),
				Login: &config.LoginContext{PermissionSetID: ps.ID}})
			wrote = append(wrote, written{name, server})
		}
	}
	// A full login replaces the contexts the last one wrote; so does one as
	// somebody else.
	if (accountFlag == "" && setFlag == "") || (previous != nil && previous.Email != session.Email) {
		for _, c := range append([]config.Context(nil), cfg.Contexts...) {
			if c.Login != nil && !names[c.Name] {
				cfg.Remove(c.Name)
			}
		}
	}
	if len(wrote) > 0 && !names[cfg.CurrentContext] {
		cfg.CurrentContext = wrote[0].name
	}
	if err := cfg.Save(); err != nil {
		return &ExitError{Code: 1, Msg: err.Error()}
	}

	fmt.Fprintf(out, "Signed in as %s until %s.\n", session.Email, session.ExpiresAt.Local().Format("15:04"))
	if len(wrote) == 0 {
		msg := "You hold no permission set in an environment that trusts the Access Portal"
		if accountFlag != "" || setFlag != "" {
			msg = "No permission set you hold matches --account and --permission-set"
		}
		return &ExitError{Code: 3, Msg: msg + "; no context was written."}
	}
	fmt.Fprintln(out, "Contexts:")
	for _, w := range wrote {
		marker := " "
		if w.name == cfg.CurrentContext {
			marker = "*"
		}
		fmt.Fprintf(out, "  %s %s  %s\n", marker, w.name, w.server)
	}
	if len(wrote) > 1 {
		fmt.Fprintln(out, "Switch with: yottactl config use-context <name>")
	}
	return nil
}

func discoverLogin(issuer string) (loginDiscovery, error) {
	var d loginDiscovery
	status, body, err := loginCall(http.MethodGet, issuer+"/.well-known/openid-configuration", "", nil, nil)
	if err != nil {
		return d, fmt.Errorf("reach %s: %w", issuer, err)
	}
	if status != http.StatusOK {
		return d, fmt.Errorf("%s answered its discovery document with HTTP %d", issuer, status)
	}
	if err := json.Unmarshal(body, &d); err != nil {
		return d, fmt.Errorf("read %s's discovery document: %w", issuer, err)
	}
	if d.Issuer != issuer {
		return d, fmt.Errorf("%s says it is %q", issuer, d.Issuer)
	}
	if !onOrigin(d.TokenEndpoint, issuer) || !onOrigin(d.DeviceAuthorizationEndpoint, issuer) {
		return d, fmt.Errorf("%s does not offer the device sign-in on its own address", issuer)
	}
	return d, nil
}

func requestDeviceCode(d loginDiscovery, issuer string) (deviceCodeAnswer, error) {
	var dc deviceCodeAnswer
	form := url.Values{"client_id": {loginClientID}, "device_name": {deviceName()}}
	status, body, err := loginCall(http.MethodPost, d.DeviceAuthorizationEndpoint, "", form, nil)
	if err != nil {
		return dc, fmt.Errorf("ask %s for a code: %w", issuer, err)
	}
	if status != http.StatusOK {
		return dc, fmt.Errorf("%s gave no code: HTTP %d %s", issuer, status, loginErrorCode(body))
	}
	if err := json.Unmarshal(body, &dc); err != nil {
		return dc, fmt.Errorf("read the code: %w", err)
	}
	// The address is printed and opened, so it must be the issuer's own page.
	if dc.DeviceCode == "" || !userCodeShape.MatchString(dc.UserCode) || !onOrigin(dc.VerificationURI, issuer) {
		return dc, fmt.Errorf("%s answered with a code yottactl does not trust", issuer)
	}
	if dc.ExpiresIn <= 0 || dc.ExpiresIn > 1800 {
		dc.ExpiresIn = 600
	}
	if dc.Interval <= 0 {
		dc.Interval = 5 // RFC 8628 §3.2's default
	}
	return dc, nil
}

// pollDeviceCode asks at the token endpoint until the person decides, the
// code expires, or login-auth refuses (RFC 8628 §3.4–3.5).
func pollDeviceCode(d loginDiscovery, dc deviceCodeAnswer) (config.LoginSession, error) {
	interval := time.Duration(dc.Interval) * time.Second
	deadline := loginNow().Add(time.Duration(dc.ExpiresIn) * time.Second)
	form := url.Values{"grant_type": {deviceGrantType}, "device_code": {dc.DeviceCode}, "client_id": {loginClientID}}
	expired := errors.New("the code expired before it was approved — run `yottactl login` again")
	for {
		loginSleep(interval)
		if !loginNow().Before(deadline) {
			return config.LoginSession{}, expired
		}
		status, body, err := loginCall(http.MethodPost, d.TokenEndpoint, "", form, nil)
		if err != nil {
			interval = min(interval*2, maxPollInterval)
			continue
		}
		if status == http.StatusOK {
			var tok struct {
				AccessToken string `json:"access_token"`
				ExpiresIn   int    `json:"expires_in"`
			}
			if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" || tok.ExpiresIn <= 0 {
				return config.LoginSession{}, errors.New("the Access Portal approved the code but sent no sign-in")
			}
			return config.LoginSession{Token: tok.AccessToken, ExpiresAt: loginNow().Add(time.Duration(tok.ExpiresIn) * time.Second)}, nil
		}
		switch code := loginErrorCode(body); code {
		case "authorization_pending":
		case "slow_down":
			interval = min(interval+slowDownStep, maxPollInterval)
		case "access_denied":
			return config.LoginSession{}, errors.New("the code was denied at the Access Portal")
		case "expired_token":
			return config.LoginSession{}, expired
		default:
			return config.LoginSession{}, fmt.Errorf("the Access Portal refused the sign-in: HTTP %d %s", status, code)
		}
	}
}

// ---- the per-command trade -------------------------------------------------

// loginContextToken is the environment token for a context `yottactl login`
// wrote: the one it holds, or a new one from Access keys and the
// environment's exchange, saved for the next command.
func loginContextToken(cfg *config.Config, ctx *config.Context) (string, error) {
	now := loginNow()
	if ctx.Token != "" && ctx.Login.TokenExpiresAt.After(now.Add(refreshBefore)) {
		return ctx.Token, nil
	}
	s := cfg.Login
	if s == nil || s.Token == "" || !s.ExpiresAt.After(now) {
		return "", errLoginEnded
	}
	issuer, ok := httpsOrigin(s.Issuer)
	server, ok2 := httpsOrigin(ctx.Server)
	accountID, err := strconv.ParseInt(ctx.AccountID, 10, 64)
	if !ok || !ok2 || err != nil || ctx.Login.PermissionSetID == "" {
		return "", fmt.Errorf("context %q is not one yottactl login can fetch keys for — run `yottactl login`", ctx.Name)
	}

	status, body, err := loginCall(http.MethodPost, issuer+"/v1/login/access-keys", s.Token, nil,
		map[string]any{"account_id": accountID, "permission_set_id": ctx.Login.PermissionSetID})
	if err != nil {
		return "", fmt.Errorf("fetch keys for %q: %w", ctx.Name, err)
	}
	if status == http.StatusUnauthorized {
		return "", errLoginEnded
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("the Access Portal refused keys for %q: HTTP %d %s", ctx.Name, status, loginErrorCode(body))
	}
	var keys struct {
		IDToken       string `json:"id_token"`
		TokenEndpoint string `json:"token_endpoint"`
		Environment   string `json:"environment"`
	}
	if err := json.Unmarshal(body, &keys); err != nil || keys.IDToken == "" {
		return "", fmt.Errorf("the Access Portal sent no keys for %q", ctx.Name)
	}
	// The ID token goes only to this context's own environment, at its one
	// exchange (access-keys.cjs exchangeTarget).
	if env, ok := httpsOrigin(keys.Environment); !ok || env != server || keys.TokenEndpoint != server+exchangePath {
		return "", fmt.Errorf("the Access Portal named an exchange for %q that is not %s", ctx.Name, server+exchangePath)
	}

	form := url.Values{
		"grant_type":         {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token_type": {"urn:ietf:params:oauth:token-type:id_token"},
		"subject_token":      {keys.IDToken},
	}
	status, body, err = loginCall(http.MethodPost, keys.TokenEndpoint, "", form, nil)
	if err != nil {
		return "", fmt.Errorf("reach %s: %w", server, err)
	}
	var got struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &got) != nil || got.AccessToken == "" {
		return "", fmt.Errorf("%s did not issue a token for %q: HTTP %d %s", server, ctx.Name, status, loginErrorCode(body))
	}
	if got.ExpiresIn <= 0 || got.ExpiresIn > 3600 {
		got.ExpiresIn = 3600
	}
	ctx.Token = got.AccessToken
	ctx.Login.TokenExpiresAt = now.Add(time.Duration(got.ExpiresIn) * time.Second)
	if err := cfg.Save(); err != nil {
		return "", err
	}
	return ctx.Token, nil
}

// ---- logout ----------------------------------------------------------------

func runLogout(cmd *cobra.Command) error {
	out := cmd.OutOrStdout()
	cfg, err := config.Load()
	if err != nil {
		return &ExitError{Code: 1, Msg: err.Error()}
	}
	cleared := 0
	for i := range cfg.Contexts {
		if c := &cfg.Contexts[i]; c.Login != nil {
			if c.Token != "" {
				cleared++
			}
			c.Token, c.Login.TokenExpiresAt = "", time.Time{}
		}
	}
	session := cfg.Login
	if session == nil && cleared == 0 {
		fmt.Fprintln(out, "yottactl is not signed in at the Access Portal.")
		return nil
	}

	// Revoke the CLI session first; whatever login.yotta.bot answers, this
	// computer forgets it.
	var revokeErr error
	if session != nil && session.Token != "" && session.ExpiresAt.After(loginNow()) {
		if issuer, ok := httpsOrigin(session.Issuer); ok {
			status, _, err := loginCall(http.MethodPost, issuer+"/v1/login/signout", session.Token, nil, struct{}{})
			switch {
			case err != nil:
				revokeErr = fmt.Errorf("couldn't reach %s (%v): the sign-in there lasts until %s", issuer, err, session.ExpiresAt.Local().Format("15:04"))
			case status != http.StatusNoContent && status != http.StatusOK && status != http.StatusUnauthorized:
				revokeErr = fmt.Errorf("%s answered HTTP %d: the sign-in there lasts until %s", issuer, status, session.ExpiresAt.Local().Format("15:04"))
			}
		}
	}
	cfg.Login = nil
	if err := cfg.Save(); err != nil {
		return &ExitError{Code: 1, Msg: err.Error()}
	}
	fmt.Fprintf(out, "Signed out of the Access Portal; deleted %d environment token(s) from this computer.\n", cleared)
	if revokeErr != nil {
		return &ExitError{Code: 1, Msg: revokeErr.Error()}
	}
	return nil
}

// ---- helpers ---------------------------------------------------------------

// loginCall sends one request: a form when form is set, else JSON when body
// is set. bearer is the CLI session, sent only to login.yotta.bot.
func loginCall(method, target, bearer string, form url.Values, body any) (int, []byte, error) {
	var rdr io.Reader
	contentType := ""
	switch {
	case form != nil:
		rdr, contentType = strings.NewReader(form.Encode()), "application/x-www-form-urlencoded"
	case body != nil:
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rdr, contentType = bytes.NewReader(b), "application/json"
	}
	req, err := http.NewRequest(method, target, rdr)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := loginHTTP.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxLoginBody))
	return resp.StatusCode, b, err
}

// loginErrorCode is an OAuth or login-auth error's code, or "".
func loginErrorCode(body []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &e)
	return e.Error
}

// httpsOrigin is raw as an https origin (scheme and host, nothing else).
func httpsOrigin(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") {
		return "", false
	}
	return "https://" + u.Host, true
}

// onOrigin reports whether raw is an https address on origin, with no query.
func onOrigin(raw, origin string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.User == nil && u.RawQuery == "" && u.Fragment == "" &&
		"https://"+u.Host == origin
}

var nonContextName = regexp.MustCompile(`[^a-z0-9]+`)

// contextName names a context for an account and set exactly as Access keys
// does (access-keys.cjs contextName; tests/context-names.json holds both).
func contextName(account, set string) string {
	name := strings.Trim(nonContextName.ReplaceAllString(strings.ToLower(account+" "+set), "-"), "-")
	if len(name) > 63 {
		name = name[:63]
	}
	if name == "" {
		return "yotta"
	}
	return name
}

// upsertContext adds or replaces a context without changing the current one.
func upsertContext(cfg *config.Config, c config.Context) {
	if existing := cfg.Find(c.Name); existing != nil {
		*existing = c
		return
	}
	cfg.Contexts = append(cfg.Contexts, c)
}

// deviceName is the computer's name as the approval page shows it.
func deviceName() string {
	host, err := os.Hostname()
	if err != nil {
		return ""
	}
	name := strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return -1
	}, strings.TrimSpace(host))
	if r := []rune(name); len(r) > maxDeviceName {
		name = string(r[:maxDeviceName])
	}
	return name
}

func openInBrowser(target string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", target).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
	default:
		return exec.Command("xdg-open", target).Start()
	}
}
