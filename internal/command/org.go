package command

// `yottactl org`: an organization administrator's commands at login.yotta.bot,
// through the `yottactl login` session (#279 C).
//
// login-directory accepts that session as a Bearer on the routes of its
// surface.json alone (plan 333 step 4): these hand-written app-secret commands,
// and the generated users, groups, roles, policies, assignments, accounts and
// events commands (org_surface.go). A new secret goes to a file or to stdout and nowhere else,
// so it can go straight into a secret store without being shown on a screen,
// as `aws iam create-access-key` does for an access key. A change needs a
// sign-in approved from a browser session that used two-factor.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/YottaBot-Platform/yottactl/internal/config"
	"github.com/YottaBot-Platform/yottactl/internal/output"
	"github.com/spf13/cobra"
)

func newOrgCmd(env *Env) *cobra.Command {
	org := &cobra.Command{
		Use:     "org",
		Short:   "Manage your organization in the Access Portal (administrators, after `yottactl login`)",
		GroupID: groupID("Core"),
	}
	apps := &cobra.Command{Use: "apps", Short: "Your organization's apps and their client secrets"}
	apps.AddCommand(newOrgAppsListCmd(env), newOrgAppSecretsCmd(env))
	org.AddCommand(apps, newOrgTokenCmd(), newOrgServiceAccountsCmd())
	// The rest of the Identity API's customer surface (plan 333 step 5).
	addSurfaceCommands(env, org)
	return org
}

func newOrgAppsListCmd(env *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the organization's apps",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := orgCall(http.MethodGet, "/v1/directory/applications")
			if err != nil {
				return err
			}
			var in struct {
				Applications []struct {
					ID      string          `json:"id"`
					Kind    string          `json:"kind"`
					Name    string          `json:"name"`
					Enabled bool            `json:"enabled"`
					Config  json.RawMessage `json:"config"`
				} `json:"applications"`
			}
			if err := json.Unmarshal(body, &in); err != nil {
				return fmt.Errorf("read the apps: %w", err)
			}
			rows := []map[string]any{}
			for _, a := range in.Applications {
				row := map[string]any{"id": a.ID, "name": a.Name, "kind": a.Kind, "enabled": a.Enabled, "client_id": "", "token_auth": ""}
				if a.Kind == "oidc" {
					var c struct {
						TokenAuthMethod string `json:"token_endpoint_auth_method"`
					}
					_ = json.Unmarshal(a.Config, &c)
					if c.TokenAuthMethod == "" {
						// Rows made before #279 have none: private_key_jwt.
						c.TokenAuthMethod = "private_key_jwt"
					}
					row["client_id"], row["token_auth"] = "app-"+a.ID, c.TokenAuthMethod
				}
				rows = append(rows, row)
			}
			b, _ := json.Marshal(rows)
			return output.Render(env.Output, b, cmd.OutOrStdout())
		},
	}
}

func newOrgAppSecretsCmd(env *Env) *cobra.Command {
	secrets := &cobra.Command{Use: "secrets", Short: "An app's client secrets: two at most, each shown only when made"}

	list := &cobra.Command{
		Use: "list <app>", Short: "List an app's client secrets (when each was made; never a value)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := orgAppID(args[0])
			if err != nil {
				return err
			}
			body, err := orgCall(http.MethodGet, "/v1/directory/applications/"+id+"/secrets")
			if err != nil {
				return err
			}
			var in struct {
				Secrets json.RawMessage `json:"secrets"`
			}
			if err := json.Unmarshal(body, &in); err != nil || in.Secrets == nil {
				return fmt.Errorf("read the secrets: %v", err)
			}
			return output.Render(env.Output, in.Secrets, cmd.OutOrStdout())
		},
	}

	var out string
	create := &cobra.Command{
		Use:   "create <app>",
		Short: "Make a client secret: its value goes to --out or stdout, only this once",
		Long: `Make a client secret for an app that signs in with one.

The value is sent only this once. With --out it is written alone, with no
newline, to a new file only you can read; a file that exists is refused before
anything is made. Without --out it is printed alone on stdout, for $(...) or a
pipe. The secret's ID and when it was made go to stderr.

Examples:
  yottactl org apps secrets create app-<id> --out ./argocd.secret
  jq -n --rawfile v ./argocd.secret '{argocd: $v}' | aws secretsmanager put-secret-value --secret-id <name> --secret-string file:///dev/stdin`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := orgAppID(args[0])
			if err != nil {
				return err
			}
			var file *os.File
			if out != "" {
				// Before the secret is made, so a bad path never spends one of
				// the app's two.
				if file, err = os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600); err != nil {
					return &ExitError{Code: 4, Msg: fmt.Sprintf("--out: %v", err)}
				}
			}
			body, err := orgCallBody(http.MethodPost, "/v1/directory/applications/"+id+"/secrets", struct{}{})
			if err != nil {
				if file != nil {
					_ = file.Close()
					_ = os.Remove(out)
				}
				return err
			}
			var made struct {
				ID        string `json:"id"`
				Secret    string `json:"secret"`
				CreatedAt string `json:"created_at"`
			}
			if err := json.Unmarshal(body, &made); err != nil || made.Secret == "" {
				if file != nil {
					_ = file.Close()
					_ = os.Remove(out)
				}
				return fmt.Errorf("the Access Portal's answer had no secret in it")
			}
			where := "printed on stdout"
			if file != nil {
				_, werr := file.WriteString(made.Secret)
				if cerr := file.Close(); werr == nil {
					werr = cerr
				}
				if werr != nil {
					_ = os.Remove(out)
					return fmt.Errorf("write %s: %v; secret %s was made and can't be shown again, so delete it", out, werr, made.ID)
				}
				where = "written to " + out
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), made.Secret)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Made secret %s for app-%s at %s; %s. It isn't shown again.\n", made.ID, id, made.CreatedAt, where)
			return nil
		},
	}
	create.Flags().StringVar(&out, "out", "", "write the value to this new file (mode 0600) instead of stdout")

	del := &cobra.Command{
		Use: "delete <app> <secret-id>", Short: "Delete one of an app's client secrets", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := orgAppID(args[0])
			if err != nil {
				return err
			}
			if !looksLikeUUID(args[1]) {
				return &ExitError{Code: 4, Msg: fmt.Sprintf("%q is not a secret's ID", args[1])}
			}
			if _, err := orgCall(http.MethodDelete, "/v1/directory/applications/"+id+"/secrets/"+args[1]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted secret %s.\n", args[1])
			return nil
		},
	}

	secrets.AddCommand(list, create, del)
	return secrets
}

// orgAppID is an app's ID from its ID or its client ID (app-<id>).
func orgAppID(arg string) (string, error) {
	id := strings.TrimPrefix(arg, "app-")
	if !looksLikeUUID(id) {
		return "", &ExitError{Code: 4, Msg: fmt.Sprintf("%q is not an app's ID or client ID", arg)}
	}
	return strings.ToLower(id), nil
}

func orgCall(method, path string) ([]byte, error) { return orgCallBody(method, path, nil) }

// orgCallBody calls login-directory at the issuer `yottactl login` saved,
// with the CLI session, and turns a refusal into what to do about it.
func orgCallBody(method, path string, body any) ([]byte, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	// A service account (plan 334 step 6) signs in for itself, with a token
	// for the surface this path is on; else the person's `yottactl login`.
	machine, err := activeMachine(cfg)
	if err != nil {
		return nil, err
	}
	var issuer, bearer string
	if machine != nil {
		issuer = machine.issuer
		if bearer, err = machine.token(machine.audienceFor(path)); err != nil {
			return nil, err
		}
	} else {
		s := cfg.Login
		if s == nil || s.Token == "" || !s.ExpiresAt.After(loginNow()) {
			return nil, &ExitError{Code: 2, Msg: errLoginEnded.Error()}
		}
		var ok bool
		if issuer, ok = httpsOrigin(s.Issuer); !ok {
			return nil, &ExitError{Code: 2, Msg: errLoginEnded.Error()}
		}
		bearer = s.Token
	}
	// A change always sends JSON, {} when it has nothing to say: the
	// directory refuses one that isn't (415).
	if (method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch) && body == nil {
		body = struct{}{}
	}
	status, b, err := loginCall(method, issuer+path, bearer, nil, body)
	if err != nil {
		return nil, fmt.Errorf("reach %s: %w", issuer, err)
	}
	// A kept token refused: its sign-in was ended in Identity. Sign in once
	// more with the key and try again.
	if status == http.StatusUnauthorized && machine != nil && machine.retryable(machine.audienceFor(path)) {
		if bearer, err = machine.token(machine.audienceFor(path)); err != nil {
			return nil, err
		}
		if status, b, err = loginCall(method, issuer+path, bearer, nil, body); err != nil {
			return nil, fmt.Errorf("reach %s: %w", issuer, err)
		}
	}
	if status >= 200 && status < 300 {
		return b, nil
	}
	var e struct {
		Error string `json:"error"`
		Next  string `json:"next"`
	}
	_ = json.Unmarshal(b, &e)
	msg := fmt.Sprintf("%s answered HTTP %d", issuer, status)
	if e.Error != "" {
		msg += ": " + e.Error
	}
	switch {
	case status == http.StatusUnauthorized && machine != nil:
		msg = machine.clientID + "'s token was refused at " + issuer + ": it may have been ended in Identity, or its account disabled"
	case status == http.StatusForbidden && machine != nil && e.Error == "yottactl can't do this; use the console":
		msg = "a service account can't do this: it's the console's, in Identity"
	case status == http.StatusUnauthorized:
		msg = errLoginEnded.Error()
	case e.Next == "step_up":
		msg = "a change needs a sign-in that used two-factor: run `yottactl login` and approve it from a browser session that used your second factor"
	case e.Next == "enroll_two_factor":
		msg = "a change needs two-factor: set it up at " + issuer + ", then run `yottactl login`"
	}
	return nil, &ExitError{Code: exitForStatus(status), Msg: msg}
}
