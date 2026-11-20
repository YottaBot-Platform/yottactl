package command

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/YottaBot-Platform/yottactl/internal/client"
	"github.com/YottaBot-Platform/yottactl/internal/config"
	"github.com/spf13/cobra"
)

// newAuthCmd builds `yottactl auth {login,token,logout}`. Plan #60
// Track B B1.
//
// login (browser OIDC against the auth service) is a later-phase
// build — for now it prints the actionable alternative so the
// command exists and degrades cleanly rather than 404'ing.
func newAuthCmd(env *Env) *cobra.Command {
	auth := &cobra.Command{
		Use:     "auth",
		Short:   "Authenticate yottactl against a Yotta tenant",
		GroupID: groupID("Core"),
	}

	var (
		loginEmail, loginPassword, loginAuthEndpoint, loginCtxName, loginEndpoint string
		loginPasswordStdin                                                        bool
	)
	login := &cobra.Command{
		Use:   "login",
		Short: "Log in with email + password and store the session token",
		Long: `Log in with email + password against the auth service's /v1/login and
store the returned session token in a yottactl context.

The auth service serves /v1/login on a different route than the v1-gateway
resource API, so pass --auth-endpoint when they differ (e.g. a local
port-forward to the auth service). It defaults to the context/endpoint
server for single-endpoint deployments.

Use ` + "`yottactl login`" + ` for browser sign-in through the Access Portal.
This password flow (or ` + "`yottactl auth token`" + `) supports direct tenant authentication.

Examples:
  yottactl auth login --email you@example.com --password-stdin --context acme-prod \
    --endpoint http://localhost:8081 --auth-endpoint http://localhost:18444
  echo "$PW" | yottactl auth login --email you@example.com --password-stdin`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if loginEmail == "" {
				return &ExitError{Code: 2, Msg: "email is required: yottactl auth login --email <e> [--password <p> | --password-stdin] [--auth-endpoint <url>]"}
			}
			pw := loginPassword
			if loginPasswordStdin {
				b, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return &ExitError{Code: 1, Msg: "read password from stdin: " + err.Error()}
				}
				pw = strings.TrimRight(string(b), "\r\n")
			}
			if pw == "" {
				return &ExitError{Code: 2, Msg: "password is required (--password or --password-stdin)"}
			}

			cfg, err := config.Load()
			if err != nil {
				return &ExitError{Code: 1, Msg: err.Error()}
			}
			// Resolve the target context + its existing server.
			name := loginCtxName
			if name == "" {
				name = env.ContextName
			}
			var existingServer string
			if name == "" {
				if cur := cfg.Current(); cur != nil {
					name, existingServer = cur.Name, cur.Server
				} else {
					name = "default"
				}
			} else {
				for _, c := range cfg.Contexts {
					if c.Name == name {
						existingServer = c.Server
						break
					}
				}
			}
			gatewayEndpoint := loginEndpoint
			if gatewayEndpoint == "" {
				gatewayEndpoint = env.Endpoint
			}
			if gatewayEndpoint == "" {
				gatewayEndpoint = os.Getenv("YOTTA_ENDPOINT")
			}
			if gatewayEndpoint == "" {
				gatewayEndpoint = existingServer
			}
			// Where /v1/login is served. Defaults to the gateway endpoint
			// (single-endpoint deployments); demo/port-forward setups pass
			// the auth service URL explicitly.
			authEndpoint := loginAuthEndpoint
			if authEndpoint == "" {
				authEndpoint = gatewayEndpoint
			}
			if authEndpoint == "" {
				return &ExitError{Code: 2, Msg: "no auth endpoint — pass --auth-endpoint <url> (where /v1/login is served)"}
			}

			status, body, err := client.New(authEndpoint, "").Do("POST", "/v1/login",
				map[string]string{"email": loginEmail, "password": pw})
			if err != nil {
				return &ExitError{Code: 1, Msg: "login request failed: " + err.Error()}
			}
			if status != 200 {
				return &ExitError{Code: 1, Msg: fmt.Sprintf("login failed (HTTP %d): %s", status, strings.TrimSpace(string(body)))}
			}
			var lr struct {
				Token     string      `json:"token"`
				AccountID json.Number `json:"account_id"`
			}
			if err := json.Unmarshal(body, &lr); err != nil {
				return &ExitError{Code: 1, Msg: "parse login response: " + err.Error()}
			}
			if lr.Token == "" {
				return &ExitError{Code: 1, Msg: "login response contained no token"}
			}

			c := config.Context{Name: name, Token: lr.Token, Server: gatewayEndpoint, AccountID: lr.AccountID.String()}
			cfg.SetContext(c) // also makes this the current context
			if err := cfg.Save(); err != nil {
				return &ExitError{Code: 1, Msg: err.Error()}
			}
			fmt.Printf("logged in as %s; stored token for context %q\n", loginEmail, name)
			return nil
		},
	}
	login.Flags().StringVar(&loginEmail, "email", "", "login email")
	login.Flags().StringVar(&loginPassword, "password", "", "login password (prefer --password-stdin to keep it out of shell history)")
	login.Flags().BoolVar(&loginPasswordStdin, "password-stdin", false, "read the password from stdin")
	login.Flags().StringVar(&loginAuthEndpoint, "auth-endpoint", "", "auth service URL serving /v1/login (default: the context/endpoint server)")
	login.Flags().StringVar(&loginCtxName, "context", "", "context name to store the session under (default: current or 'default')")
	login.Flags().StringVar(&loginEndpoint, "endpoint", "", "v1-gateway server URL for this context")

	var ctxName, endpoint, accountID string
	token := &cobra.Command{
		Use:   "token <api-token>",
		Short: "Store an API token as a (new or current) context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return &ExitError{Code: 1, Msg: err.Error()}
			}
			name := ctxName
			if name == "" {
				name = env.ContextName
			}
			if name == "" {
				if cur := cfg.Current(); cur != nil {
					name = cur.Name
				} else {
					name = "default"
				}
			}
			server := endpoint
			if server == "" {
				server = env.Endpoint
			}
			if server == "" {
				server = os.Getenv("YOTTA_ENDPOINT")
			}
			c := config.Context{Name: name, Token: args[0], AccountID: accountID}
			// preserve existing server if the context already exists
			if existing := cfg.Find(name); existing != nil && server == "" {
				c.Server = existing.Server
				if !cmd.Flags().Changed("account-id") {
					c.AccountID = existing.AccountID
				}
			}
			if server != "" {
				c.Server = server
			}
			cfg.SetContext(c)
			if err := cfg.Save(); err != nil {
				return &ExitError{Code: 1, Msg: err.Error()}
			}
			fmt.Printf("stored token for context %q\n", name)
			return nil
		},
	}
	token.Flags().StringVar(&ctxName, "context", "", "context name to store under (default: current or 'default')")
	token.Flags().StringVar(&endpoint, "endpoint", "", "server URL for this context")
	token.Flags().StringVar(&accountID, "account-id", "", "tenant account id (only for multi-tenant tokens)")

	logout := &cobra.Command{
		Use:   "logout",
		Short: "Clear the current context's token",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return &ExitError{Code: 1, Msg: err.Error()}
			}
			name := cfg.CurrentContext
			if env.ContextName != "" {
				name = env.ContextName
			}
			cur := cfg.Find(name)
			if cur == nil {
				return &ExitError{Code: 3, Msg: "no current context"}
			}
			cur.Token, cur.Login = "", nil
			if err := cfg.Save(); err != nil {
				return &ExitError{Code: 1, Msg: err.Error()}
			}
			fmt.Printf("cleared token for context %q\n", cur.Name)
			return nil
		},
	}

	auth.AddCommand(login, token, logout)
	return auth
}
