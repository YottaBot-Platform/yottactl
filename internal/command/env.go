// Package command builds the yottactl cobra tree from the spec
// registry + the bespoke top-level commands (auth, config,
// tools-catalog). Plan #60 Track B.
package command

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/YottaBot-Platform/yottactl/internal/client"
	"github.com/YottaBot-Platform/yottactl/internal/config"
	"github.com/YottaBot-Platform/yottactl/internal/output"
)

// Env carries the resolved global flags + config, shared by every
// command's RunE. Resolution order for endpoint + token, highest
// first: explicit flag → YOTTA_ENDPOINT/YOTTA_TOKEN env → the
// current (or --context-selected) config context.
type Env struct {
	Headers     []string // repeated --header Name=value
	Query       []string // repeated --query key=value
	Output      string   // -o table|json|yaml
	Endpoint    string   // --endpoint override
	Token       string   // --token override
	ContextName string   // --context override
}

// Client resolves a *client.Client from flags → env → config.
func (e *Env) Client() (*client.Client, error) {
	endpoint, token := e.Endpoint, e.Token
	if endpoint == "" {
		endpoint = os.Getenv("YOTTA_ENDPOINT")
	}
	if token == "" {
		token = os.Getenv("YOTTA_TOKEN")
	}
	if endpoint == "" || token == "" || e.ContextName != "" {
		cfg, err := config.Load()
		if err != nil {
			return nil, err
		}
		name := cfg.CurrentContext
		if e.ContextName != "" {
			name = e.ContextName
		}
		ctx := cfg.Find(name)
		if name != "" && ctx == nil {
			return nil, fmt.Errorf("no context named %q", name)
		}
		if ctx != nil {
			if endpoint == "" {
				endpoint = ctx.Server
			}
			if token == "" && ctx.Login != nil {
				// A context `yottactl login` wrote (#258 Step 12c): its token
				// is that environment's own and goes nowhere else.
				if origin, ok := httpsOrigin(endpoint); !ok || origin != ctx.Server {
					return nil, fmt.Errorf("context %q holds a token for %s only; pass --token to call %s", ctx.Name, ctx.Server, endpoint)
				}
				if token, err = loginContextToken(cfg, ctx); err != nil {
					return nil, err
				}
			} else if token == "" {
				token = ctx.Token
			}
		}
	}
	if endpoint == "" {
		// Surfaced here (not in client.Do) so doCall maps it to the
		// auth/not-configured exit code (2), the actionable signal:
		// the user needs to `yottactl auth token ...` or pass
		// --endpoint.
		return nil, fmt.Errorf("no endpoint configured — run `yottactl auth token <api-token> --endpoint <url>` or pass --endpoint")
	}
	query := url.Values{}
	for _, item := range e.Query {
		key, value, ok := strings.Cut(item, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("invalid --query %q: want key=value", item)
		}
		query.Add(key, value)
	}
	api := client.New(endpoint, token)
	api.SetQuery(query)
	headers := map[string]string{}
	for _, item := range e.Headers {
		name, value, ok := strings.Cut(item, "=")
		if !ok || name == "" {
			return nil, fmt.Errorf("invalid --header %q: want Name=value", item)
		}
		headers[http.CanonicalHeaderKey(name)] = value
	}
	api.SetHeaders(headers)
	return api, nil
}

// Render writes a response body in the selected output format.
func (e *Env) Render(body []byte) error {
	return output.Render(e.Output, body, os.Stdout)
}

// controlHeader preserves a caller's conditional-write or retry control before
// a bespoke command supplies defaults. Conflicting explicit controls are an
// error: silently choosing one could publish an unreviewed draft or repeat a run.
func (e *Env) controlHeader(name, explicit string) (string, error) {
	value := explicit
	for _, item := range e.Headers {
		key, header, ok := strings.Cut(item, "=")
		if !ok || !strings.EqualFold(key, name) {
			continue // Client validates the general header syntax.
		}
		if strings.TrimSpace(header) == "" {
			return "", &ExitError{Code: 4, Msg: name + " must not be empty"}
		}
		if value != "" && value != header {
			return "", &ExitError{Code: 4, Msg: "conflicting " + name + " values; supply one consistent value"}
		}
		value = header
	}
	return value, nil
}

// ExitError carries a CLI exit code. main() translates it via
// os.Exit. Codes (plan #60 Track B B1): 0 success, 1 generic,
// 2 auth, 3 not found, 4 validation, 5 conflict, 99 not implemented.
type ExitError struct {
	Code int
	Msg  string
}

func (e *ExitError) Error() string { return e.Msg }

// exitForStatus maps an HTTP status to a CLI exit code.
func exitForStatus(status int) int {
	switch {
	case status >= 200 && status < 300:
		return 0
	case status == 401 || status == 403:
		return 2
	case status == 404:
		return 3
	case status == 400 || status == 422:
		return 4
	case status == 409 || status == 412:
		return 5
	default:
		return 1
	}
}

// finish renders the body and returns an ExitError when the status
// is non-2xx so the process exit code reflects the outcome (the
// background agent reads it).
// Finish prints a response in the chosen output format and turns a failed
// status into its exit code, as every yottactl command does.
func Finish(output string, status int, body []byte) error {
	return (&Env{Output: output}).finish(status, body)
}

func (e *Env) finish(status int, body []byte) error {
	if err := e.Render(body); err != nil {
		return &ExitError{Code: 1, Msg: err.Error()}
	}
	if code := exitForStatus(status); code != 0 {
		return &ExitError{Code: code, Msg: fmt.Sprintf("request failed: HTTP %d", status)}
	}
	return nil
}

// ReadBody is readBody, exported for other command trees built on this one.
func ReadBody(file string) (any, error) { return readBody(file) }

// RandomUUID is a random version-4 UUID, an idempotency key.
func RandomUUID() (string, error) { return randomUUID() }
