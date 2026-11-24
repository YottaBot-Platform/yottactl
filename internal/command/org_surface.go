package command

// `yottactl org` commands generated from the customer surfaces, the routes
// chosen as safe to open to a program: the Identity API's (plan 333 step 5)
// and Boo · Admin's (`org boo`, step 7, reads only). directory_surface.json
// and boo_admin_surface.json are byte copies of the servers' lists (`make
// sync-surface`), so yottactl offers exactly what the server admits a token on
// and Platform documents: one row, one command, `yottactl org <cli>`. The
// app-secret rows' commands are written by hand (org.go), as they keep a
// secret off the screen.

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/YottaBot-Platform/yottactl/internal/config"
	"github.com/YottaBot-Platform/yottactl/internal/output"
	"github.com/spf13/cobra"
)

//go:embed directory_surface.json
var directorySurfaceJSON []byte

//go:embed boo_admin_surface.json
var booSurfaceJSON []byte

type surfaceRow struct {
	Method     string `json:"method"`
	Path       string `json:"path"`
	Permission string `json:"permission"`
	Doc        string `json:"doc"`
	CLI        string `json:"cli"`
}

func directorySurface() []surfaceRow {
	return surfaceRows("directory_surface.json", directorySurfaceJSON)
}

// booSurface is Boo · Admin's rows, every one `boo <verb>`.
func booSurface() []surfaceRow { return surfaceRows("boo_admin_surface.json", booSurfaceJSON) }

func surfaceRows(name string, raw []byte) []surfaceRow {
	var f struct {
		Routes []surfaceRow `json:"routes"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		panic(fmt.Sprintf("yottactl: %s: %v", name, err))
	}
	return f.Routes
}

// handWritten are the rows org.go serves itself.
var handWritten = map[string]bool{"apps list": true, "apps secrets list": true, "apps secrets create": true, "apps secrets delete": true}

var pathParam = regexp.MustCompile(`\{([^}]+)\}`)

// argNames names a row's path parameters for its usage line: the first {id}
// is the resource itself (users → <user>), the rest by their own names.
func argNames(row surfaceRow) []string {
	noun := strings.Fields(row.CLI)[0]
	singular := map[string]string{"users": "user", "groups": "group", "roles": "role", "policies": "policy", "assignments": "assignment", "accounts": "account", "events": "event"}[noun]
	var out []string
	for _, m := range pathParam.FindAllStringSubmatch(row.Path, -1) {
		name := m[1]
		if name == "id" {
			name = singular
		}
		out = append(out, name)
	}
	return out
}

// addSurfaceCommands puts every generated row under org, one group per noun.
func addSurfaceCommands(env *Env, org *cobra.Command) {
	nouns := map[string]*cobra.Command{}
	add := func(rows []surfaceRow, short func(noun string) string) {
		for _, row := range rows {
			if handWritten[row.CLI] {
				continue
			}
			parts := strings.Fields(row.CLI)
			noun, verb := parts[0], parts[1]
			group, ok := nouns[noun]
			if !ok {
				group = &cobra.Command{Use: noun, Short: short(noun)}
				nouns[noun] = group
			}
			group.AddCommand(newSurfaceCmd(env, row, verb))
		}
	}
	add(directorySurface(), func(noun string) string {
		return "Your organization's " + noun + " (Identity API; yotta.bot/platform/identity)"
	})
	add(booSurface(), func(string) string {
		return "Your organization's Boo usage, plans and credits, to read (Boo · Admin API; yotta.bot/platform/boo-admin)"
	})
	names := make([]string, 0, len(nouns))
	for n := range nouns {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		org.AddCommand(nouns[n])
	}
}

func newSurfaceCmd(env *Env, row surfaceRow, verb string) *cobra.Command {
	args := argNames(row)
	use := verb
	for _, a := range args {
		use += " <" + a + ">"
	}
	var file string
	var sets []string
	cmd := &cobra.Command{
		Use:   use,
		Short: fmt.Sprintf("%s %s (needs %s)", row.Method, row.Path, row.Permission),
		Long: fmt.Sprintf("%s %s, with your `yottactl login` session.\n\nNeeds %s. Reference: https://yotta.bot/platform/%s",
			row.Method, row.Path, row.Permission, row.Doc),
		Args: cobra.ExactArgs(len(args)),
		RunE: func(cmd *cobra.Command, given []string) error {
			i := 0
			path := pathParam.ReplaceAllStringFunc(row.Path, func(string) string {
				v := url.PathEscape(given[i])
				i++
				return v
			})
			if q := queryString(env.Query); q != "" {
				path += "?" + q
			}
			var body any
			if row.Method != "GET" && row.Method != "DELETE" {
				b, err := surfaceBody(file, sets)
				if err != nil {
					return &ExitError{Code: 4, Msg: err.Error()}
				}
				body = b
			}
			response, err := orgCallBody(row.Method, path, body)
			if err != nil {
				return err
			}
			if len(strings.TrimSpace(string(response))) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "done")
				return nil
			}
			return output.Render(env.Output, listOf(response), cmd.OutOrStdout())
		},
	}
	if row.Method != "GET" && row.Method != "DELETE" {
		cmd.Flags().StringVarP(&file, "file", "f", "", "the request body, a JSON or YAML file")
		cmd.Flags().StringArrayVar(&sets, "set", nil, "a body field, key=value (repeatable; true, false, null and numbers are JSON)")
	}
	return cmd
}

// surfaceBody is the request body: -f's file, or --set's fields, or {}.
func surfaceBody(file string, sets []string) (any, error) {
	if file != "" && len(sets) > 0 {
		return nil, fmt.Errorf("use -f or --set, not both")
	}
	if file != "" {
		return readBody(file)
	}
	body := map[string]any{}
	for _, s := range sets {
		key, value, ok := strings.Cut(s, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("--set takes key=value: %q", s)
		}
		// Numbers keep their exact digits (a 12-digit account ID).
		dec := json.NewDecoder(strings.NewReader(value))
		dec.UseNumber()
		var typed any
		if dec.Decode(&typed) == nil && !dec.More() {
			switch typed.(type) {
			case bool, json.Number, nil:
				body[key] = typed
				continue
			}
		}
		body[key] = value
	}
	return body, nil
}

func queryString(pairs []string) string {
	q := url.Values{}
	for _, p := range pairs {
		if k, v, ok := strings.Cut(p, "="); ok {
			q.Add(k, v)
		}
	}
	return q.Encode()
}

// listOf is a response's rows when it is {"<things>": [...]}, so table output
// lists them; anything else as it came.
func listOf(response []byte) []byte {
	var obj map[string]json.RawMessage
	if json.Unmarshal(response, &obj) == nil && len(obj) == 1 {
		for _, v := range obj {
			var rows []json.RawMessage
			if json.Unmarshal(v, &rows) == nil {
				return v
			}
		}
	}
	return response
}

// newOrgTokenCmd prints the `yottactl login` session's token for curl against
// yotta.bot/platform's endpoints, as `gh auth token` prints gh's (plan 333
// step 5).
func newOrgTokenCmd() *cobra.Command {
	var surface string
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Print your `yottactl login` session's token, for calling the Identity API with curl",
		Long: `Print the token your ` + "`yottactl login`" + ` session holds, for
Authorization: Bearer on yotta.bot/platform's endpoints. It acts as you, with
your roles, until the session ends; treat it as a password.

Signed in as a service account, it prints a 15-minute token of its own, for
Identity (--surface identity) or Boo · Admin (--surface boo-admin).

Example:
  export YOTTABOT_TOKEN="$(yottactl org token)"
  curl https://<company>.yotta.bot/v1/directory/people -H "Authorization: Bearer $YOTTABOT_TOKEN"`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			// A service account's token is for one surface (plan 334 step 6).
			machine, err := activeMachine(cfg)
			if err != nil {
				return err
			}
			if machine != nil {
				path := "/v1/directory"
				switch surface {
				case "identity":
				case "boo-admin":
					path = booAdminPrefix
				default:
					return &ExitError{Code: 2, Msg: "--surface is identity or boo-admin"}
				}
				t, err := machine.token(machine.audienceFor(path))
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), t)
				return nil
			}
			s := cfg.Login
			if s == nil || s.Token == "" || !s.ExpiresAt.After(loginNow()) {
				return &ExitError{Code: 2, Msg: errLoginEnded.Error()}
			}
			fmt.Fprintln(cmd.OutOrStdout(), s.Token)
			return nil
		},
	}
	cmd.Flags().StringVar(&surface, "surface", "identity", "for a service account: the token's surface, identity or boo-admin (a person's token is for both)")
	return cmd
}
