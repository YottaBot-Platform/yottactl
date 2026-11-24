package command

// An organization's service account (plan 334 step 6): yottactl
// signing in as a machine of the organization, not as a person.
//
// Three ways, the first that applies:
//
//  1. YOTTA_SERVICE_ACCOUNT names it, with YOTTA_PRIVATE_KEY (the key's PEM)
//     or YOTTA_PRIVATE_KEY_FILE (its path): a pipeline's secret store.
//  2. YOTTA_SERVICE_ACCOUNT with no key, inside GitHub Actions with
//     `permissions: id-token: write`: GitHub's own OIDC token for the job is
//     traded for one of the service account's, under one of its trust
//     policies. No key exists at all.
//  3. `yottactl login --service-account <id> --key <file>` saved where the key
//     is (never the key itself).
//
// A command asks login.yotta.bot for a 15-minute token for the surface it
// calls, Identity's or Boo · Admin's, signing a five-minute, single-use
// assertion with the key (RFC 7523 private_key_jwt). The third way keeps the
// token in the config until a minute before it ends, so the next command uses
// it rather than signing in again (as the AWS CLI does); a pipeline's last the
// run. A kept token refused (its sign-in ended in Identity) is dropped and the
// key signs in once more: ending a sign-in ends that token, not the key. The key never leaves this computer; the token only goes to
// that issuer. What the token may do is what the service account's roles
// allow, and never a change to service accounts, keys or trust policies.

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/YottaBot-Platform/yottactl/internal/config"
)

const (
	clientAssertionType = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"
	tokenExchangeGrant  = "urn:ietf:params:oauth:grant-type:token-exchange"
	idTokenType         = "urn:ietf:params:oauth:token-type:id-token"
	// booAdminPrefix is Boo · Admin's routes' prefix, and its tokens' audience.
	booAdminPrefix = "/v1/login/boo-admin"
	// assertionLife is how long a signed assertion lasts: login.yotta.bot
	// refuses one over five minutes.
	assertionLife = 4 * time.Minute
)

// machineCredential is a service account's way in, from the environment or
// the config.
type machineCredential struct {
	issuer   string
	clientID string
	keyID    string
	key      ed25519.PrivateKey // nil for GitHub Actions
	github   bool
	// saved is the third way: its tokens are kept in the config.
	saved bool
	// reused are the audiences whose token this run didn't ask for itself.
	reused map[string]bool
}

// machineTokens are the run's tokens, by audience.
var machineTokens = map[string]machineToken{}

type machineToken struct {
	token   string
	expires time.Time
}

// serviceAccountClientID is a service account as login.yotta.bot names it:
// sa-<id>, given as that or as the id alone.
func serviceAccountClientID(arg string) (string, error) {
	id := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(arg)), "sa-")
	if !looksLikeUUID(id) {
		return "", &ExitError{Code: 2, Msg: fmt.Sprintf("%q is not a service account's ID or client ID (sa-<id>)", arg)}
	}
	return "sa-" + id, nil
}

// parsePrivateKey reads an Ed25519 private key in PKCS #8 PEM, as Identity
// and `yottactl org service-accounts keygen` write it.
func parsePrivateKey(raw []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, errors.New("not a PEM private key, as `yottactl org service-accounts keygen` writes it")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("not a PKCS #8 private key: %w", err)
	}
	ed, ok := k.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("not an Ed25519 key")
	}
	return ed, nil
}

// keyThumbprint is a public key's RFC 7638 thumbprint, its key ID at
// login.yotta.bot unless another was given.
func keyThumbprint(pub ed25519.PublicKey) string {
	canonical := `{"crv":"Ed25519","kty":"OKP","x":"` + base64.RawURLEncoding.EncodeToString(pub) + `"}`
	sum := sha256.Sum256([]byte(canonical))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// activeMachine is the service account this run signs in as, or nil for a
// person's `yottactl login` session.
func activeMachine(cfg *config.Config) (*machineCredential, error) {
	issuerOf := func(saved string) (string, error) {
		raw := firstNonEmpty(os.Getenv("YOTTA_ISSUER"), saved, cfg.LoginIssuer, defaultLoginIssuer)
		issuer, ok := httpsOrigin(raw)
		if !ok {
			return "", &ExitError{Code: 2, Msg: fmt.Sprintf("the issuer %q is not an https origin", raw)}
		}
		return issuer, nil
	}
	if name := os.Getenv("YOTTA_SERVICE_ACCOUNT"); name != "" {
		clientID, err := serviceAccountClientID(name)
		if err != nil {
			return nil, err
		}
		issuer, err := issuerOf("")
		if err != nil {
			return nil, err
		}
		c := &machineCredential{issuer: issuer, clientID: clientID, keyID: os.Getenv("YOTTA_KEY_ID")}
		raw := []byte(os.Getenv("YOTTA_PRIVATE_KEY"))
		if len(raw) == 0 && os.Getenv("YOTTA_PRIVATE_KEY_FILE") != "" {
			if raw, err = os.ReadFile(os.Getenv("YOTTA_PRIVATE_KEY_FILE")); err != nil {
				return nil, &ExitError{Code: 2, Msg: "read YOTTA_PRIVATE_KEY_FILE: " + err.Error()}
			}
		}
		switch {
		case len(raw) > 0:
			if c.key, err = parsePrivateKey(raw); err != nil {
				return nil, &ExitError{Code: 2, Msg: "the service account's key: " + err.Error()}
			}
		case os.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL") != "" && os.Getenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN") != "":
			c.github = true
		default:
			return nil, &ExitError{Code: 2, Msg: "YOTTA_SERVICE_ACCOUNT needs YOTTA_PRIVATE_KEY or YOTTA_PRIVATE_KEY_FILE, or a GitHub Actions job with `permissions: id-token: write`"}
		}
		return c, nil
	}
	sa := cfg.ServiceAccount
	if sa == nil {
		return nil, nil
	}
	issuer, err := issuerOf(sa.Issuer)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(sa.KeyFile)
	if err != nil {
		return nil, &ExitError{Code: 2, Msg: fmt.Sprintf("read the service account's key %s: %v — run `yottactl login --service-account` again", sa.KeyFile, err)}
	}
	key, err := parsePrivateKey(raw)
	if err != nil {
		return nil, &ExitError{Code: 2, Msg: "the service account's key " + sa.KeyFile + ": " + err.Error()}
	}
	return &machineCredential{issuer: issuer, clientID: sa.ClientID, keyID: sa.KeyID, key: key, saved: true}, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// audienceFor is the surface a path is on: Boo · Admin's, else Identity's.
func (c *machineCredential) audienceFor(path string) string {
	if strings.HasPrefix(path, booAdminPrefix) {
		return c.issuer + booAdminPrefix
	}
	return c.issuer + "/v1/directory"
}

// token is a token for audience: this run's, while it lasts, else a new one.
func (c *machineCredential) token(audience string) (string, error) {
	key := c.clientID + " " + audience
	if t, ok := machineTokens[key]; ok && t.expires.After(loginNow().Add(refreshBefore)) {
		c.markReused(audience)
		return t.token, nil
	}
	if t, ok := c.kept(audience); ok {
		machineTokens[key] = t
		c.markReused(audience)
		return t.token, nil
	}
	form := url.Values{"audience": {audience}}
	path := "/oauth/token"
	if c.github {
		idt, err := githubIDToken(c.issuer)
		if err != nil {
			return "", err
		}
		path = "/oauth/federated-token"
		form.Set("grant_type", tokenExchangeGrant)
		form.Set("subject_token_type", idTokenType)
		form.Set("subject_token", idt)
		form.Set("client_id", c.clientID)
	} else {
		assertion, err := c.assertion()
		if err != nil {
			return "", err
		}
		form.Set("grant_type", "client_credentials")
		form.Set("client_assertion_type", clientAssertionType)
		form.Set("client_assertion", assertion)
	}
	status, body, err := loginCall(http.MethodPost, c.issuer+path, "", form, nil)
	if err != nil {
		return "", fmt.Errorf("reach %s: %w", c.issuer, err)
	}
	if status != http.StatusOK {
		return "", &ExitError{Code: 2, Msg: machineRefusal(c, status, body)}
	}
	var t struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &t); err != nil || t.AccessToken == "" || t.ExpiresIn <= 0 {
		return "", fmt.Errorf("read %s's token: %v", c.issuer, err)
	}
	issued := machineToken{token: t.AccessToken, expires: loginNow().Add(time.Duration(t.ExpiresIn) * time.Second)}
	machineTokens[key] = issued
	delete(c.reused, audience)
	c.keep(audience, issued)
	return t.AccessToken, nil
}

func (c *machineCredential) markReused(audience string) {
	if c.reused == nil {
		c.reused = map[string]bool{}
	}
	c.reused[audience] = true
}

// kept is the config's token for audience, while it lasts.
func (c *machineCredential) kept(audience string) (machineToken, bool) {
	if !c.saved {
		return machineToken{}, false
	}
	cfg, err := config.Load()
	if err != nil || cfg.ServiceAccount == nil || cfg.ServiceAccount.ClientID != c.clientID {
		return machineToken{}, false
	}
	t, ok := cfg.ServiceAccount.Tokens[audience]
	if !ok || t.Token == "" || !t.ExpiresAt.After(loginNow().Add(refreshBefore)) {
		return machineToken{}, false
	}
	return machineToken{token: t.Token, expires: t.ExpiresAt}, true
}

// keep writes audience's token to the config, for the next command. A token
// that can't be kept still serves this run.
func (c *machineCredential) keep(audience string, t machineToken) {
	if !c.saved {
		return
	}
	cfg, err := config.Load()
	if err != nil || cfg.ServiceAccount == nil || cfg.ServiceAccount.ClientID != c.clientID {
		return
	}
	if t.token == "" {
		delete(cfg.ServiceAccount.Tokens, audience)
	} else {
		if cfg.ServiceAccount.Tokens == nil {
			cfg.ServiceAccount.Tokens = map[string]config.SavedToken{}
		}
		cfg.ServiceAccount.Tokens[audience] = config.SavedToken{Token: t.token, ExpiresAt: t.expires}
	}
	_ = cfg.Save()
}

// retryable is whether a refusal of audience's token may be a kept token's
// sign-in ended in Identity: then it is dropped, and the key signs in again.
func (c *machineCredential) retryable(audience string) bool {
	if !c.reused[audience] || c.github {
		return false
	}
	delete(machineTokens, c.clientID+" "+audience)
	delete(c.reused, audience)
	c.keep(audience, machineToken{})
	return true
}

// machineRefusal says what to do about a refused token request.
func machineRefusal(c *machineCredential, status int, body []byte) string {
	var e struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &e)
	switch {
	case e.Error == "invalid_client":
		return c.clientID + " was refused at " + c.issuer + ": check that it is enabled and that its key is one of its keys in Identity, not revoked"
	case e.Error == "invalid_grant" && c.github:
		return c.clientID + " was refused at " + c.issuer + ": no trust policy matches this job's repository, environment, ref and event, or its token was used before"
	case e.Error == "invalid_grant" && e.Description != "":
		return c.clientID + ": " + e.Description
	case e.Error == "access_denied":
		return c.clientID + " was refused at " + c.issuer + ": " + firstNonEmpty(e.Description, "its organization's network policy doesn't allow this address")
	case e.Error == "unsupported_grant_type" || status == http.StatusNotFound:
		return c.issuer + " doesn't sign service accounts in yet"
	}
	return fmt.Sprintf("%s answered HTTP %d %s", c.issuer, status, firstNonEmpty(e.Error, ""))
}

// assertion is a five-minute, single-use RFC 7523 assertion signed with the
// key: login.yotta.bot checks it against the service account's public key.
func (c *machineCredential) assertion() (string, error) {
	kid := c.keyID
	if kid == "" {
		kid = keyThumbprint(c.key.Public().(ed25519.PublicKey))
	}
	jti := make([]byte, 16)
	if _, err := rand.Read(jti); err != nil {
		return "", err
	}
	now := loginNow()
	h, _ := json.Marshal(map[string]string{"alg": "EdDSA", "typ": "JWT", "kid": kid})
	claims, _ := json.Marshal(map[string]any{"iss": c.clientID, "sub": c.clientID, "aud": c.issuer + "/oauth/token",
		"iat": now.Unix(), "exp": now.Add(assertionLife).Unix(), "jti": hex.EncodeToString(jti)})
	input := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(claims)
	return input + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(c.key, []byte(input))), nil
}

// githubIDToken is the job's own OIDC token, for audience issuer, from
// GitHub Actions' token service.
func githubIDToken(issuer string) (string, error) {
	u, err := url.Parse(os.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL"))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", &ExitError{Code: 2, Msg: "ACTIONS_ID_TOKEN_REQUEST_URL is not an https address"}
	}
	q := u.Query()
	q.Set("audience", issuer)
	u.RawQuery = q.Encode()
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN"))
	req.Header.Set("Accept", "application/json")
	resp, err := loginHTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("ask GitHub Actions for the job's token: %w", err)
	}
	defer resp.Body.Close()
	var t struct {
		Value string `json:"value"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&t) != nil || t.Value == "" {
		return "", &ExitError{Code: 2, Msg: fmt.Sprintf("GitHub Actions gave no token for the job (HTTP %d): the job needs `permissions: id-token: write`", resp.StatusCode)}
	}
	return t.Value, nil
}
