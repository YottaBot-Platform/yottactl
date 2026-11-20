package command

import (
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"
)

// newAgentsRunCmd is a bespoke, non-CRUD verb on the `agents` noun:
// `yottactl agents run <agent-id>` — a DIRECT agent run (POST /v1/agent-runs).
//
// It deliberately HONORS the plan #111 §1 "no naked agent runs" guardrail: it
// sends only {agent_id} and NEVER a synthesized workflow_step_run_id, so the
// server-side policy (require_workflow_for_agent_runs) is the single arbiter.
// When the account requires a workflow the endpoint returns 403; this command
// surfaces the server message and points the user at running the agent inside
// a workflow instead of faking ownership.
//
// Like `agents can`, it stays OUT of the declarative spec.Registry (and the
// tools-catalog) on purpose: a naked agent run must not be part of the
// vocabulary Albus composes from — Albus runs agents through workflows.
func newAgentsRunCmd(env *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "run <agent-id>",
		Short: "Directly run an agent (honors the require-workflow guardrail; may be denied)",
		Long: "Enqueue a direct agent run (POST /v1/agent-runs).\n\n" +
			"Direct runs are governed by the account's require_workflow_for_agent_runs\n" +
			"policy. When the policy is on the server denies the run (HTTP 403) — this\n" +
			"command does NOT bypass it. Place the agent in a workflow and use\n" +
			"`yottactl workflows run <workflow-id>` instead, or an admin can turn the\n" +
			"guardrail off in Agent Platform → Settings → Runtime policy.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := env.Client()
			if err != nil {
				return &ExitError{Code: 2, Msg: err.Error()}
			}
			status, respBody, err := c.Do("POST", "/v1/agent-runs", map[string]any{"agent_id": args[0]})
			if err != nil {
				return &ExitError{Code: 1, Msg: err.Error()}
			}
			if rerr := env.Render(respBody); rerr != nil {
				return &ExitError{Code: 1, Msg: rerr.Error()}
			}
			if status == http.StatusForbidden {
				fmt.Fprintln(os.Stderr, "\nThis account requires agents to run inside a workflow (require_workflow_for_agent_runs).")
				fmt.Fprintln(os.Stderr, "Add the agent as a step in a workflow and run it with `yottactl workflows run <workflow-id>`,")
				fmt.Fprintln(os.Stderr, "or an admin can turn the guardrail off in Agent Platform → Settings → Runtime policy.")
				return &ExitError{Code: exitForStatus(status), Msg: "direct agent run denied by company policy"}
			}
			if code := exitForStatus(status); code != 0 {
				return &ExitError{Code: code, Msg: fmt.Sprintf("request failed: HTTP %d", status)}
			}
			return nil
		},
	}
}
