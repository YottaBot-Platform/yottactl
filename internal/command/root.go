package command

import (
	"sort"
	"strings"

	"github.com/YottaBot-Platform/yottactl/internal/spec"
	"github.com/spf13/cobra"
)

// NewRoot assembles the full yottactl command tree: global flags +
// the Core commands (auth, config, tools-catalog) + a command per
// registry noun, grouped for help. Plan #60 Track B.
func NewRoot() *cobra.Command {
	env := &Env{}

	root := &cobra.Command{
		Use:   "yottactl",
		Short: "yottactl, YottaBot's command-line tool",
		Long: "yottactl is YottaBot's command-line tool: your organization's users,\n" +
			"groups, roles and apps under `yottactl org`, and your environment's\n" +
			"agents, workflows, context and more.\n\n" +
			"Start with `yottactl login`. Docs: https://yotta.bot/platform",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			switch env.Output {
			case "table", "json", "yaml":
			default:
				return &ExitError{Code: 4, Msg: "output format must be table, json, or yaml"}
			}
			for _, entry := range append(append([]string{}, env.Query...), env.Headers...) {
				name, _, ok := strings.Cut(entry, "=")
				if !ok || name == "" {
					return &ExitError{Code: 4, Msg: "query and header arguments must be name=value"}
				}
			}
			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		// yottactl --version (plan 333 step 5).
		Version: version(),
	}
	root.SetVersionTemplate("yottactl {{.Version}}\n")

	pf := root.PersistentFlags()
	pf.StringArrayVar(&env.Headers, "header", nil, "HTTP header Name=value (repeatable; authentication headers cannot be overridden)")
	pf.StringArrayVar(&env.Query, "query", nil, "query parameter key=value (repeatable)")
	pf.StringVarP(&env.Output, "output", "o", "table", "output format: table|json|yaml")
	pf.StringVar(&env.Endpoint, "endpoint", "", "server URL (overrides config + YOTTA_ENDPOINT)")
	pf.StringVar(&env.Token, "token", "", "bearer token (overrides config + YOTTA_TOKEN)")
	pf.StringVar(&env.ContextName, "context", "", "config context to use (overrides current-context)")

	// Core commands.
	root.AddCommand(newLoginCmd(), newLogoutCmd(), newOrgCmd(env), newAuthCmd(env), newConfigCmd(env), newCatalogCmd(), newClusterCmd(env))

	// Noun commands from the registry. (deployment-targets was a bespoke
	// CLI-only read until plan #112 promoted its full list/create/delete +
	// set-default surface into the registry.)
	for _, c := range BuildNounCommands(env) {
		switch c.Use {
		case "agents":
			// Bespoke non-CRUD verbs mounted on the agents noun; kept out of
			// the registry/catalog by design (see each command's doc):
			// `can` (plan #60 Phase 8 step 36) and `run` (plan #111 §7 —
			// a direct agent run that honors the require-workflow guardrail).
			c.AddCommand(newAgentsCanCmd(env), newAgentsRunCmd(env))
		case "workflows":
			// Swap the registry-generated generic `run` for the target-aware
			// bespoke one (plan #111 §7), and update/publish for the
			// draft-aware ones (plan #244 WP2b-3). All three stay in the
			// catalog (registry-driven); only the cobra commands change.
			replaceWorkflowsRun(env, c)
			replaceWorkflowsDraftCommands(env, c)
		case "licenses":
			// The registry declares the request-body contract for OpenAPI, while
			// operators get a safer file/Secret-shaped command that never asks
			// them to hand-author a JSON wrapper around a compact JWS.
			replaceLicensesInstall(env, c)
		}
		root.AddCommand(c)
	}

	// Help groups, in display order: only those a listed command is in, so
	// no heading stands empty. A hidden command (inHelp) leaves its group.
	used := map[string]bool{}
	for _, c := range root.Commands() {
		if c.Hidden {
			c.GroupID = ""
		} else if c.GroupID != "" {
			used[c.GroupID] = true
		}
	}
	for _, g := range helpGroups() {
		if used[groupID(g)] {
			root.AddGroup(&cobra.Group{ID: groupID(g), Title: g + ":"})
		}
	}
	return root
}

// groupID derives a stable cobra group ID from a human group name.
func groupID(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, " ", "-")) + "-grp"
}

// helpGroups returns the distinct registry groups (+ Core) in a
// stable display order: Core first, then the rest alphabetical.
func helpGroups() []string {
	// No Internal Admin group: yottactl has no staff commands (plan 333
	// step 3).
	seen := map[string]bool{"Core": true}
	for _, n := range spec.Registry {
		seen[n.Group] = true
	}
	var rest []string
	for g := range seen {
		if g != "Core" {
			rest = append(rest, g)
		}
	}
	sort.Strings(rest)
	return append([]string{"Core"}, rest...)
}
