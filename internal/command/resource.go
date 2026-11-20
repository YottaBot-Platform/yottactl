package command

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/YottaBot-Platform/yottactl/internal/client"
	"github.com/YottaBot-Platform/yottactl/internal/spec"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// BuildNounCommands turns the spec.Registry into cobra commands.
// Nouns with a Parent are mounted under a shared parent command
// (created once, e.g. `yottactl context resources ...`); top-level
// nouns mount directly on root. Returns the commands to add to root.
func BuildNounCommands(env *Env) []*cobra.Command {
	// First pass: build a command for every noun and index the top-level ones
	// by name. A child whose Parent is itself a top-level noun (e.g.
	// model-credentials under models, agent-credentials under agents) must mount
	// on the REAL noun command — creating a second synthetic parent with the
	// same Use shadows the real noun and makes the child unreachable (cobra
	// resolves to the real noun and prints its help). Only pure grouping parents
	// with no standalone noun (context, machines, humans) get a synthetic parent.
	// Keyed by (parent, name), NOT bare name: two nouns can share a
	// name across groups (e.g. agent-platform `workflows` at top level
	// and drone `workflows` under `drone`). Keying by name alone lets
	// the later registry entry clobber the earlier one, so the drone
	// noun shadows the top-level agent-platform noun and `yottactl
	// workflows` silently hits /v1/drone-platform/workflows. See nounKey.
	cmds := make(map[string]*cobra.Command, len(spec.Registry))
	// Parents are referenced by bare name (unique among top-level nouns),
	// so topByName stays name-keyed.
	topByName := map[string]*cobra.Command{}
	for _, n := range spec.Registry {
		nounCmd := &cobra.Command{
			Use:    n.Name,
			Short:  n.Short,
			Hidden: !inHelp(n),
		}
		// Only root-level commands carry a GroupID (cobra requires the
		// group to be defined on the command's parent — root defines
		// them). Sub-nouns mounted under a parent command (e.g.
		// `context resources`) inherit no group.
		if n.Parent == "" {
			nounCmd.GroupID = groupID(n.Group)
		}
		for _, v := range n.Verbs {
			nounCmd.AddCommand(buildVerb(env, n, v))
		}
		for _, a := range n.Actions {
			nounCmd.AddCommand(buildAction(env, n, a))
		}
		cmds[nounKey(n)] = nounCmd
		if n.Parent == "" {
			topByName[n.Name] = nounCmd
		}
	}

	// Second pass: assemble the root-level list. Top-level nouns in registry
	// order; each parented noun mounts on its real parent noun if one exists,
	// else on a synthetic grouping parent created once.
	var top []*cobra.Command
	synth := map[string]*cobra.Command{}
	for _, n := range spec.Registry {
		if n.Parent == "" {
			top = append(top, cmds[nounKey(n)])
			continue
		}
		if p, ok := topByName[n.Parent]; ok {
			p.AddCommand(cmds[nounKey(n)])
			continue
		}
		p, ok := synth[n.Parent]
		if !ok {
			p = &cobra.Command{
				Use:     n.Parent,
				Short:   fmt.Sprintf("%s resources", strings.Title(n.Parent)),
				GroupID: groupID(n.Group),
				// Shown once one of its nouns is (inHelp), below.
				Hidden: true,
			}
			synth[n.Parent] = p
			top = append(top, p)
		}
		if inHelp(n) {
			p.Hidden = false
		}
		p.AddCommand(cmds[nounKey(n)])
	}
	return top
}

// nounKey uniquely identifies a registry noun by (parent, name). Bare
// name is NOT unique: agent-platform and drone both expose a `workflows`
// noun (top-level vs under `drone`). The NUL separator can't appear in a
// noun name, so the key is collision-free.
func nounKey(n spec.Noun) string {
	return n.Parent + "\x00" + n.Name
}

// inHelp says whether a noun is listed in yottactl's help. One with no
// backend (Unshipped) or one that only answers [] (Stub) is left out, as
// customers read the help (plan 333 step 5, 1.0.1), but still runs, so a
// script that calls it gets the server's own error.
func inHelp(n spec.Noun) bool {
	return !n.Stub && !n.Unshipped
}

func buildVerb(env *Env, n spec.Noun, v spec.Verb) *cobra.Command {
	switch v {
	case spec.List:
		return &cobra.Command{
			Use:   "list",
			Short: "List " + n.Name,
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return doCall(env, "GET", n.VerbPath(spec.List), nil)
			},
		}
	case spec.Get:
		return &cobra.Command{
			Use:   "get <" + idArg(n) + ">",
			Short: "Get one " + singular(n.Name),
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return doCall(env, "GET", n.VerbPath(spec.Get)+"/"+url.PathEscape(args[0]), nil)
			},
		}
	case spec.Create:
		var file string
		c := &cobra.Command{
			Use:   "create -f <file>",
			Short: "Create a " + singular(n.Name),
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				body, err := readBody(file)
				if err != nil {
					return &ExitError{Code: 4, Msg: err.Error()}
				}
				return doCall(env, "POST", n.VerbPath(spec.Create), body)
			},
		}
		c.Flags().StringVarP(&file, "file", "f", "", "JSON/YAML file with the resource body")
		return c
	case spec.Update:
		var file string
		c := &cobra.Command{
			Use:   "update <" + idArg(n) + "> -f <file>",
			Short: "Update a " + singular(n.Name),
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				body, err := readBody(file)
				if err != nil {
					return &ExitError{Code: 4, Msg: err.Error()}
				}
				return doCall(env, "PATCH", n.VerbPath(spec.Update)+"/"+url.PathEscape(args[0]), body)
			},
		}
		c.Flags().StringVarP(&file, "file", "f", "", "JSON/YAML file with the patch body")
		return c
	case spec.Delete:
		return &cobra.Command{
			Use:   "delete <" + idArg(n) + ">",
			Short: "Delete a " + singular(n.Name),
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return doCall(env, "DELETE", n.VerbPath(spec.Delete)+"/"+url.PathEscape(args[0]), nil)
			},
		}
	default:
		return &cobra.Command{Use: string(v), Hidden: true}
	}
}

// buildAction builds the cobra subcommand for a custom (non-CRUD) action: a
// positional <IDParam> (substituted into the path template) and an optional -f
// body. Mirrors the run/observe REST routes the backend already serves.
func buildAction(env *Env, n spec.Noun, a spec.Action) *cobra.Command {
	use := a.Verb
	nArgs := 0
	if a.IDParam != "" {
		use += " <" + a.IDParam + ">"
		nArgs = 1
		if a.IDParam2 != "" {
			use += " <" + a.IDParam2 + ">"
			nArgs = 2
		}
	}
	if a.IDParam3 != "" {
		use += " <" + a.IDParam3 + ">"
		nArgs++
	}
	if a.Body {
		use += " -f <file>"
	}
	var file string
	c := &cobra.Command{
		Use:   use,
		Short: a.Short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := a.Path
			if a.IDParam != "" {
				path = strings.Replace(path, "{"+a.IDParam+"}", escapeActionID(a, args[0]), 1)
			}
			if a.IDParam2 != "" {
				path = strings.Replace(path, "{"+a.IDParam2+"}", url.PathEscape(args[1]), 1)
			}
			if a.IDParam3 != "" {
				path = strings.Replace(path, "{"+a.IDParam3+"}", url.PathEscape(args[2]), 1)
			}
			var body any
			if a.Body {
				b, err := readActionBody(file, a.BodyContentType)
				if err != nil {
					return &ExitError{Code: 4, Msg: err.Error()}
				}
				body = b
			}
			return doCall(env, a.Method, path, body)
		},
	}
	if nArgs > 0 {
		c.Args = cobra.ExactArgs(nArgs)
	}
	if a.Body {
		c.Flags().StringVarP(&file, "file", "f", "", "JSON/YAML file with the request body")
	}
	return c
}

// doCall resolves the client, issues the request, and finishes
// (render + exit code).
func doCall(env *Env, method, path string, body any) error {
	c, err := env.Client()
	if err != nil {
		return &ExitError{Code: 2, Msg: err.Error()}
	}
	resp, err := c.DoResponse(method, path, body, nil)
	if err != nil {
		return &ExitError{Code: 1, Msg: err.Error()}
	}
	if etag := resp.Header.Get("ETag"); etag != "" {
		resp.Body = withETag(resp.Body, etag)
	}
	return env.finish(resp.Status, resp.Body)
}

// readBody loads a JSON or YAML file and returns it as a generic
// value (YAML is converted to JSON-compatible structures by the
// marshaller in client.Do).
func readBody(file string) (any, error) {
	if file == "" {
		return nil, fmt.Errorf("a body file is required: -f <file.yaml|json>")
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var v any
	if json.Valid(b) {
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber()
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
	} else {
		dec := yaml.NewDecoder(bytes.NewReader(b))
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", file, err)
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			return nil, fmt.Errorf("%s must contain exactly one document", file)
		}
	}
	if _, ok := v.(map[string]any); !ok {
		return nil, fmt.Errorf("%s must contain a JSON object", file)
	}
	// JSON marshalling also rejects non-string nested YAML mapping keys and
	// non-finite numbers, instead of silently rewriting or losing user data.
	if _, err := json.Marshal(v); err != nil {
		return nil, fmt.Errorf("%s is not JSON-compatible: %w", file, err)
	}
	return v, nil
}

// The GitHub file action is the backend's one wildcard path. Escape each
// segment there; all other action identifiers are single path segments.
func escapeActionID(a spec.Action, value string) string {
	if a.Path == "/v1/agent-platform/github/file/{spec}" {
		parts := strings.Split(value, "/")
		for i := range parts {
			parts[i] = url.PathEscape(parts[i])
		}
		return strings.Join(parts, "/")
	}
	return url.PathEscape(value)
}

func idArg(n spec.Noun) string {
	if n.IDParam != "" {
		return n.IDParam
	}
	return "id"
}

func singular(name string) string {
	return strings.TrimSuffix(name, "s")
}

func readActionBody(file, contentType string) (any, error) {
	if contentType == "" {
		return readBody(file)
	}
	if file == "" {
		return nil, fmt.Errorf("a body file is required: -f <file>")
	}
	b, err := os.ReadFile(file)
	return client.RawBody{Data: b, ContentType: contentType}, err
}
