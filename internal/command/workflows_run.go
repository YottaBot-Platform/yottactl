package command

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/YottaBot-Platform/yottactl/internal/spec"
)

// deploymentTarget mirrors one row of GET /v1/agent-platform/deployment-targets.
// Only the fields the CLI resolves against are decoded; the rest are ignored.
type deploymentTarget struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	ClusterResourceID   string `json:"cluster_resource_id"`
	NamespaceResourceID string `json:"namespace_resource_id"`
	ClusterNameSnapshot string `json:"cluster_name_snapshot"`
	NamespaceSnapshot   string `json:"namespace_snapshot"`
	Status              string `json:"status"`
	IsDefault           bool   `json:"is_default"`
	RunnerKind          string `json:"runner_kind"`
}

// workflowDeployment is the part of a deployment the run command reads from
// GET /v1/automation-platform/workflows/{id}/deployments.
type workflowDeployment struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	IsDefault bool   `json:"is_default"`
}

// newWorkflowsRunCmd starts a canonical execution (plan #244):
// POST /v1/automation-platform/workflows/{id}/executions with an
// Idempotency-Key. It runs a PUBLISHED version — a draft never runs — under
// the runner the server chooses, after the runtime preflight.
//
// Where it runs, in order:
//
//   - --deployment <id>: that deployment's own version on its own target.
//   - --deployment-target <id>, or --cluster [--namespace] resolved to exactly
//     one registered target: ad hoc there, on --version or the active version.
//   - no placement flag: the workflow's active default deployment; with none
//     (or with --version, which a deployment cannot take), ad hoc on the target
//     the workflow resolves to (GET .../effective-target), as the legacy /run
//     route chose it.
//
// workflows.run stays in the tools catalog (it's registry-driven); only the
// cobra command is bespoke. Albus runs tools through this binary, so it starts
// executions the same way.
func newWorkflowsRunCmd(env *Env) *cobra.Command {
	var f runFlags
	var key string
	c := &cobra.Command{
		Use:   "run <id>",
		Short: "Start a run of a workflow's published version",
		Long: "Start an execution of a workflow (POST /v1/automation-platform/workflows/{id}/executions).\n" +
			"It runs a published version, never the draft: publish first.\n\n" +
			"Where it runs:\n" +
			"  --deployment <id>                       that deployment's version on its target\n" +
			"  --deployment-target <id>                ad hoc on a registered target\n" +
			"  --cluster <resource-or-name>            ad hoc on the cluster's only target\n" +
			"  --cluster <...> --namespace <name>      ad hoc on an exact cluster/namespace target\n" +
			"  (none)                                  the default deployment, else ad hoc where\n" +
			"                                          the workflow resolves\n\n" +
			"--version <id> pins the version of an ad-hoc run (default: the active version).\n" +
			"Each run sends a fresh Idempotency-Key; pass the same --idempotency-key to\n" +
			"make a retry return the first run. The global --header also accepts\n" +
			"Idempotency-Key; conflicting values are refused.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			var err error
			if key, err = env.controlHeader("Idempotency-Key", key); err != nil {
				return err
			}
			body, err := buildExecutionRequest(runSources{
				targets:         func() ([]deploymentTarget, error) { return fetchDeploymentTargets(env) },
				deployments:     func() ([]workflowDeployment, error) { return fetchWorkflowDeployments(env, id) },
				effectiveTarget: func() (string, error) { return fetchEffectiveTarget(env, id) },
			}, f)
			if err != nil {
				return err
			}
			if key == "" {
				if key, err = newIdempotencyKey(); err != nil {
					return &ExitError{Code: 1, Msg: err.Error()}
				}
			}
			api, err := env.Client()
			if err != nil {
				return &ExitError{Code: 2, Msg: err.Error()}
			}
			resp, err := api.DoResponse(http.MethodPost, spec.WorkflowsCanonicalPath+"/"+url.PathEscape(id)+"/executions", body,
				map[string]string{"Idempotency-Key": key})
			if err != nil {
				return &ExitError{Code: 1, Msg: err.Error()}
			}
			return env.finish(resp.Status, resp.Body)
		},
	}
	c.Flags().StringVar(&f.deployment, "deployment", "", "run this deployment (its own version on its own target); excludes the flags below")
	c.Flags().StringVar(&f.version, "version", "", "the published version an ad-hoc run pins (default: the active version)")
	c.Flags().StringVar(&f.deploymentTarget, "deployment-target", "", "run ad hoc on this registered deployment target (target UUID); excludes --cluster/--namespace")
	c.Flags().StringVar(&f.cluster, "cluster", "", "run ad hoc on this cluster's target (cluster resource id or cluster name)")
	c.Flags().StringVar(&f.namespace, "namespace", "", "narrow --cluster to this namespace (namespace resource id or name)")
	c.Flags().StringVar(&key, "idempotency-key", "", "reuse a key to make a retried run return the first one")
	return c
}

// runFlags is where and what a run asks for.
type runFlags struct {
	deployment, version, deploymentTarget, cluster, namespace string
}

// runSources are the reads an execution request may need. Each is called only
// when the flags need it.
type runSources struct {
	targets         func() ([]deploymentTarget, error)
	deployments     func() ([]workflowDeployment, error)
	effectiveTarget func() (string, error)
}

// buildExecutionRequest turns the flags into the executions request body:
// {deployment_id} for a deployed run, {deployment_target_id[,
// workflow_version_id]} for an ad-hoc one. Conflicting flags are refused before
// any read (exit 4).
func buildExecutionRequest(src runSources, f runFlags) (map[string]any, error) {
	if err := validateRunFlags(f); err != nil {
		return nil, err
	}
	if f.deployment != "" {
		return map[string]any{"deployment_id": f.deployment}, nil
	}
	adHoc := func(target string) map[string]any {
		body := map[string]any{"deployment_target_id": target}
		if f.version != "" {
			body["workflow_version_id"] = f.version
		}
		return body
	}
	switch {
	case f.deploymentTarget != "":
		return adHoc(f.deploymentTarget), nil
	case f.cluster != "":
		targets, err := src.targets()
		if err != nil {
			return nil, err
		}
		target, err := resolveClusterNamespaceTarget(targets, f.cluster, f.namespace)
		if err != nil {
			return nil, err
		}
		return adHoc(target), nil
	}
	if f.version == "" {
		deployments, err := src.deployments()
		if err != nil {
			return nil, err
		}
		for _, d := range deployments {
			if d.IsDefault && d.Status == "active" {
				return map[string]any{"deployment_id": d.ID}, nil
			}
		}
	}
	target, err := src.effectiveTarget()
	if err != nil {
		return nil, err
	}
	if target == "" {
		return nil, &ExitError{Code: 4, Msg: "the workflow has no default deployment and resolves to no deployment target; " +
			"name one with --deployment, --deployment-target or --cluster"}
	}
	return adHoc(target), nil
}

// validateRunFlags enforces the flag contract before any network call.
func validateRunFlags(f runFlags) error {
	if f.deployment != "" && (f.version != "" || f.deploymentTarget != "" || f.cluster != "" || f.namespace != "") {
		return &ExitError{Code: 4, Msg: "--deployment runs that deployment's own version on its own target; " +
			"it cannot be combined with --version, --deployment-target, --cluster or --namespace"}
	}
	if f.deploymentTarget != "" && (f.cluster != "" || f.namespace != "") {
		return &ExitError{Code: 4, Msg: "--deployment-target is mutually exclusive with --cluster/--namespace"}
	}
	if f.namespace != "" && f.cluster == "" {
		return &ExitError{Code: 4, Msg: "--namespace requires --cluster (a namespace name is only unambiguous within a cluster)"}
	}
	return nil
}

// resolveClusterNamespaceTarget matches the human --cluster (and optional
// --namespace) input against the registered deployment targets, to exactly one
// target id. A cluster matches by cluster_resource_id OR cluster_name_snapshot;
// a namespace by namespace_resource_id OR namespace_snapshot. A cluster alone
// must have exactly one target: an execution names a target, not a cluster.
func resolveClusterNamespaceTarget(targets []deploymentTarget, cluster, namespace string) (string, error) {
	var clusterMatches []deploymentTarget
	for _, t := range targets {
		if t.ClusterResourceID == cluster || t.ClusterNameSnapshot == cluster {
			clusterMatches = append(clusterMatches, t)
		}
	}
	if len(clusterMatches) == 0 {
		return "", &ExitError{Code: 4, Msg: fmt.Sprintf("no registered deployment target for cluster %q", cluster)}
	}
	if namespace == "" {
		if len(clusterMatches) == 1 {
			return clusterMatches[0].ID, nil
		}
		names := make([]string, 0, len(clusterMatches))
		for _, t := range clusterMatches {
			names = append(names, t.NamespaceSnapshot)
		}
		return "", &ExitError{Code: 4, Msg: fmt.Sprintf("cluster %q has %d deployment targets (%s); name one with --namespace",
			cluster, len(clusterMatches), strings.Join(names, ", "))}
	}
	for _, t := range clusterMatches {
		if t.NamespaceResourceID == namespace || t.NamespaceSnapshot == namespace {
			return t.ID, nil
		}
	}
	return "", &ExitError{Code: 4, Msg: fmt.Sprintf("no registered deployment target for cluster %q / namespace %q", cluster, namespace)}
}

// fetchWorkflowDeployments lists the workflow's deployments, to find its
// active default.
func fetchWorkflowDeployments(env *Env, workflowID string) ([]workflowDeployment, error) {
	var page struct {
		Items []workflowDeployment `json:"items"`
	}
	if err := getJSON(env, spec.WorkflowsCanonicalPath+"/"+url.PathEscape(workflowID)+"/deployments", "listing the workflow's deployments", &page); err != nil {
		return nil, err
	}
	return page.Items, nil
}

// fetchEffectiveTarget is the target the workflow resolves to without an
// override — its binding, its agents', or the account default.
func fetchEffectiveTarget(env *Env, workflowID string) (string, error) {
	var eff struct {
		Target struct {
			ID string `json:"id"`
		} `json:"Target"`
	}
	if err := getJSON(env, "/v1/agent-platform/workflows/"+url.PathEscape(workflowID)+"/effective-target", "resolving the workflow's deployment target", &eff); err != nil {
		return "", err
	}
	return eff.Target.ID, nil
}

// getJSON reads one resource for the run command, mapping failures to exit
// codes the way every other read does.
func getJSON(env *Env, path, what string, out any) error {
	c, err := env.Client()
	if err != nil {
		return &ExitError{Code: 2, Msg: err.Error()}
	}
	status, body, err := c.Do("GET", path, nil)
	if err != nil {
		return &ExitError{Code: 1, Msg: err.Error()}
	}
	if code := exitForStatus(status); code != 0 {
		return &ExitError{Code: code, Msg: fmt.Sprintf("%s failed: HTTP %d: %s", what, status, strings.TrimSpace(string(body)))}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return &ExitError{Code: 1, Msg: what + ": " + err.Error()}
	}
	return nil
}

// fetchDeploymentTargets lists the account's registered deployment targets so
// the CLI can resolve a human --cluster/--namespace into a concrete id.
func fetchDeploymentTargets(env *Env) ([]deploymentTarget, error) {
	c, err := env.Client()
	if err != nil {
		return nil, &ExitError{Code: 2, Msg: err.Error()}
	}
	status, body, err := c.Do("GET", "/v1/agent-platform/deployment-targets", nil)
	if err != nil {
		return nil, &ExitError{Code: 1, Msg: err.Error()}
	}
	if code := exitForStatus(status); code != 0 {
		return nil, &ExitError{Code: code, Msg: fmt.Sprintf("listing deployment targets failed: HTTP %d: %s", status, strings.TrimSpace(string(body)))}
	}
	var targets []deploymentTarget
	if err := json.Unmarshal(body, &targets); err != nil {
		return nil, &ExitError{Code: 1, Msg: "parsing deployment targets: " + err.Error()}
	}
	return targets, nil
}

// replaceWorkflowsRun swaps the registry-generated generic `run` subcommand on
// the agent-platform workflows noun for the target-aware bespoke one. Called
// from NewRoot after BuildNounCommands (mirrors the `agents can` mount point).
func replaceWorkflowsRun(env *Env, workflows *cobra.Command) {
	for _, sub := range workflows.Commands() {
		if sub.Name() == "run" {
			workflows.RemoveCommand(sub)
			break
		}
	}
	workflows.AddCommand(newWorkflowsRunCmd(env))
}
