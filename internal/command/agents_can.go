package command

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

// newAgentsCanCmd is a bespoke, non-CRUD convenience verb mounted on the
// `agents` noun: `yottactl agents can <agent-id> <verb> <resource-id>`.
//
// It hits the plan #60 Phase 8 step 36 introspection endpoint
// (GET /v1/agent-platform/agents/{id}/resource-authz) and prints the
// live decision — direct grant ∪ selector-match policy, deny-wins,
// fail-closed. It's deliberately kept OUT of the declarative spec.Registry
// (and therefore the tools-catalog): the registry models CRUD nouns +
// their agent-facing tools, whereas `can` is a human/Albus debugging aid
// that takes positional args and reads a query-string endpoint. Adding it
// here keeps the registry/catalog clean and CRUD-only.
func newAgentsCanCmd(env *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "can <agent-id> <verb> <resource-id>",
		Short: "Check whether an agent may perform a verb on a Context resource",
		Long: "Runs the live resource-authz decision (plan #60 Phase 8 step 36):\n" +
			"direct grant ∪ selector-match policy, deny-wins, fail-closed. For MVP-01\n" +
			"an agent principal resolves to its direct grants only. Prints {allowed, reason}.",
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, verb, resource := args[0], args[1], args[2]
			path := fmt.Sprintf("/v1/agent-platform/agents/%s/resource-authz?resource_id=%s&verb=%s",
				url.PathEscape(id), url.QueryEscape(resource), url.QueryEscape(verb))
			return doCall(env, "GET", path, nil)
		},
	}
}
