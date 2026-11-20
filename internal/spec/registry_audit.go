package spec

// Public, permission-gated routes verified against their owning handlers in
// the October 2026 contract audit. Runtime callbacks remain outside this list.
func init() {
	GroupDocs = append(GroupDocs, GroupDoc{Name: "Training", Description: "Manage training scenarios, evaluation runs, schedules, and results."})
	Registry = append(Registry,
		Noun{Name: "scim-tokens", Parent: "identity", Group: "Identity", Path: "/v1/identity/connections/scim-tokens", IDParam: "id", Verbs: []Verb{List, Create, Delete}, Short: "SCIM connector tokens", Description: "Manage SCIM connector credentials. Creation returns plaintext once. Bind provider_id to allow group provisioning; an unbound token can provision users only."},
		Noun{Name: "auth-policies", Parent: "identity", Group: "Identity", Path: "/v1/identity/access/auth-policies", IDParam: "id", Verbs: crud(), Short: "Authentication policies", Actions: []Action{{Verb: "observed", Method: "GET", Path: "/v1/identity/access/auth-policies/observed", Short: "List observed authentication policy evaluations"}}},
		Noun{Name: "federated-issuers", Parent: "identity", Group: "Identity", Path: "/v1/identity/federation/issuers", IDParam: "id", Verbs: listGet(), Short: "Federated identity issuers"},
		Noun{Name: "federated-policies", Parent: "identity", Group: "Identity", Path: "/v1/identity/federation/policies", IDParam: "id", Verbs: listGet(), Short: "Federated identity policies"},
		Noun{Name: "federated-exchanges", Parent: "identity", Group: "Identity", Path: "/v1/identity/federation/exchanges", Verbs: []Verb{List}, Short: "Federated identity exchanges"},
		Noun{Name: "investigation-policies", Parent: "alerts", Group: "Observability", Path: "/v1/alerts-manager/investigation-policies", IDParam: "id", Verbs: crud(), Short: "Alert investigation policies", Actions: []Action{{Verb: "links", Method: "GET", Path: "/v1/alerts-manager/investigation-policies/links/{alertID}", IDParam: "alertID", Short: "List an alert's investigation policy links"}}},
		Noun{Name: "scenarios", Parent: "training", Group: "Training", Path: "/v1/training/scenarios", IDParam: "id", Verbs: rc(), Short: "Training scenarios", Actions: []Action{
			{Verb: "versions", Method: "GET", Path: "/v1/training/scenarios/{id}/versions", IDParam: "id", Short: "List scenario versions"},
			{Verb: "add-version", Method: "POST", Path: "/v1/training/scenarios/{id}/versions", IDParam: "id", Body: true, Short: "Create a scenario version"},
		}},
		Noun{Name: "runs", Parent: "training", Group: "Training", Path: "/v1/training/runs", IDParam: "id", Verbs: rc(), Short: "Training evaluation runs", Actions: []Action{
			{Verb: "delete-batch", Method: "DELETE", Path: "/v1/training/runs", Body: true, Short: "Delete settled training runs by run_ids"},
			{Verb: "rerun", Method: "POST", Path: "/v1/training/runs/rerun", Body: true, Short: "Rerun selected training runs"},
			{Verb: "launch", Method: "POST", Path: "/v1/training/runs/{id}/launch", IDParam: "id", Short: "Launch a training run"},
			{Verb: "cancel", Method: "POST", Path: "/v1/training/runs/{id}/cancel", IDParam: "id", Short: "Cancel a training run"},
			{Verb: "results", Method: "GET", Path: "/v1/training/runs/{id}/results", IDParam: "id", Short: "Get training run results"},
		}},
		Noun{Name: "results", Parent: "training", Group: "Training", Path: "/v1/training/results", Verbs: []Verb{List}, Short: "Training results", Actions: []Action{{Verb: "efficacy", Method: "GET", Path: "/v1/training/efficacy", Short: "Get training efficacy"}}},
		Noun{Name: "attempts", Parent: "training", Group: "Training", Path: "/v1/training/attempts", IDParam: "id", Verbs: []Verb{Get}, Short: "Training attempts", Actions: []Action{{Verb: "steps", Method: "GET", Path: "/v1/training/attempts/{id}/steps", IDParam: "id", Short: "List a training attempt's steps"}}},
		Noun{Name: "schedules", Parent: "training", Group: "Training", Path: "/v1/training/schedules", IDParam: "id", Verbs: []Verb{List, Create, Update, Delete}, Short: "Training schedules"},
		Noun{Name: "skills", Parent: "sre", Group: "SRE Platform", Path: "/v1/sre-platform/skills", IDParam: "slug", Verbs: []Verb{List, Get, Create}, Short: "SRE skills", Actions: []Action{
			{Verb: "update", Method: "PATCH", Path: "/v1/sre-platform/skills/id/{id}", IDParam: "id", Body: true, Short: "Update a customer SRE skill by ID"},
			{Verb: "delete", Method: "DELETE", Path: "/v1/sre-platform/skills/id/{id}", IDParam: "id", Short: "Delete a customer SRE skill by ID"},
		}},
		Noun{Name: "scenario-runs", Parent: "sre", Group: "SRE Platform", Path: "/v1/sre-platform/scenario-runs", IDParam: "id", Verbs: rcu(), Short: "SRE scenario runs"},
		Noun{Name: "runbook-catalog", Parent: "sre", Group: "SRE Platform", Path: "/v1/sre-platform/runbook-catalog", IDParam: "slug", Verbs: listGet(), Short: "Installable SRE runbooks", Actions: []Action{
			{Verb: "match", Method: "POST", Path: "/v1/sre-platform/runbook-catalog/match", Body: true, Short: "Match a symptom to catalog runbooks"},
			{Verb: "install", Method: "POST", Path: "/v1/sre-platform/runbook-catalog/{slug}/install", IDParam: "slug", Short: "Install a catalog runbook"},
		}},
	)
	for i := range Registry {
		n := &Registry[i]
		switch n.Parent + "/" + n.Name {
		case "/workflows":
			n.Actions = append(n.Actions,
				Action{Verb: "versions", Method: "GET", Path: WorkflowsCanonicalPath + "/{id}/versions", IDParam: "id", Short: "List immutable workflow versions"},
				Action{Verb: "version", Method: "GET", Path: WorkflowsCanonicalPath + "/{id}/versions/{versionId}", IDParam: "id", IDParam2: "versionId", Short: "Get an immutable workflow version"},
				Action{Verb: "list-deployments", Method: "GET", Path: WorkflowsCanonicalPath + "/{id}/deployments", IDParam: "id", Short: "List workflow deployments"},
				Action{Verb: "access", Method: "GET", Path: WorkflowsCanonicalPath + "/{id}/access", IDParam: "id", Short: "Get the caller's workflow permissions"},
				Action{Verb: "collaborators", Method: "GET", Path: WorkflowsCanonicalPath + "/{id}/collaborators", IDParam: "id", Short: "List workflow collaborators"},
				Action{Verb: "runtime-targets", Method: "GET", Path: n.Path + "/{id}/runtime-targets", IDParam: "id", Short: "List workflow runtime targets"},
				Action{Verb: "effective-target", Method: "GET", Path: n.Path + "/{id}/effective-target", IDParam: "id", Short: "Resolve the workflow's effective runtime target"},
				Action{Verb: "agents", Method: "GET", Path: n.Path + "/{id}/agents", IDParam: "id", Short: "List a workflow's agents"},
				Action{Verb: "run-resource-approvals", Method: "GET", Path: n.Path + "/runs/{id}/resource-approvals", IDParam: "id", Short: "List a run's resource approvals"},
				Action{Verb: "run-result-items", Method: "GET", Path: n.Path + "/runs/{id}/result/items", IDParam: "id", Short: "Read workflow run result items"},
				Action{Verb: "run-result-data", Method: "GET", Path: n.Path + "/runs/{id}/result/data", IDParam: "id", Short: "Read workflow run result data"},
				Action{Verb: "run-step-items", Method: "GET", Path: n.Path + "/runs/{runID}/steps/{stepID}/items", IDParam: "runID", IDParam2: "stepID", Short: "Read workflow step items"},
				Action{Verb: "run-step-data", Method: "GET", Path: n.Path + "/runs/{runID}/steps/{stepID}/data", IDParam: "runID", IDParam2: "stepID", Short: "Read workflow step data"},
			)
		case "/agents":
			n.Actions = append(n.Actions, Action{Verb: "runtime-targets", Method: "GET", Path: n.Path + "/{id}/runtime-targets", IDParam: "id", Short: "List agent runtime targets"})
		case "/traces":
			n.Actions = append(n.Actions, Action{Verb: "logs", Method: "GET", Path: n.Path + "/{id}/logs", IDParam: "id", Short: "List logs correlated to a trace"})
		}
	}
	auditFurtherRoutes()
	auditFinalRoutes()
}

func auditFurtherRoutes() {
	for i := range Registry {
		n := &Registry[i]
		switch n.Parent + "/" + n.Name {
		case "/logs":
			n.Actions = append(n.Actions,
				Action{Verb: "settings", Method: "GET", Path: "/v1/logs-manager/settings", Short: "Get log retention settings"},
				Action{Verb: "set-settings", Method: "PUT", Path: "/v1/logs-manager/settings", Body: true, Short: "Update log retention settings"})
		case "sre/findings":
			n.Actions = append(n.Actions,
				Action{Verb: "artifact", Method: "GET", Path: n.Path + "/{id}/artifact", IDParam: "id", Short: "Read a finding artifact"},
				Action{Verb: "recommendations", Method: "GET", Path: n.Path + "/{id}/recommendations", IDParam: "id", Short: "List finding recommendations"},
				Action{Verb: "add-recommendation", Method: "POST", Path: n.Path + "/{id}/recommendations", IDParam: "id", Body: true, Short: "Create a finding recommendation"},
				Action{Verb: "resolve", Method: "POST", Path: n.Path + "/{id}/recommendations/{rid}/resolve", IDParam: "id", IDParam2: "rid", Body: true, Short: "Preview or apply a finding recommendation"},
				Action{Verb: "revert", Method: "POST", Path: n.Path + "/{id}/recommendations/{rid}/revert", IDParam: "id", IDParam2: "rid", Body: true, Short: "Preview or apply a finding recommendation rollback"})
		case "/workflows":
			n.Actions = append(n.Actions,
				Action{Verb: "save-draft", Method: "PUT", Path: WorkflowsCanonicalPath + "/{id}/draft", IDParam: "id", Body: true, Short: "Write a workflow draft with --header If-Match=<etag>"},
				Action{Verb: "validate-draft", Method: "POST", Path: WorkflowsCanonicalPath + "/{id}/validate", IDParam: "id", Body: true, Short: "Validate a workflow definition"},
				Action{Verb: "retry-run", Method: "POST", Path: n.Path + "/runs/{id}/retry", IDParam: "id", Body: true, Short: "Retry a settled execution with --header Idempotency-Key=<key>"},
				Action{Verb: "create-deployment", Method: "POST", Path: WorkflowsCanonicalPath + "/{id}/deployments", IDParam: "id", Body: true, Short: "Create a workflow deployment with --header Idempotency-Key=<key>"})
		}
	}
	Registry = append(Registry,
		Noun{Name: "repair-settings", Parent: "sre", Group: "SRE Platform", Path: "/v1/sre-platform/repair-settings", Short: "Account repair policy", Actions: []Action{
			{Verb: "show", Method: "GET", Path: "/v1/sre-platform/repair-settings", Short: "Read account repair policy"},
			{Verb: "set", Method: "PUT", Path: "/v1/sre-platform/repair-settings", Body: true, Short: "Update account repair policy"},
		}},
		Noun{Name: "deployments", Parent: "workflows", Group: "Agent Platform", Path: "/v1/automation-platform/deployments", IDParam: "deploymentId", Verbs: []Verb{Get, Delete}, Short: "Workflow deployments", Actions: []Action{
			{Verb: "add-revision", Method: "POST", Path: "/v1/automation-platform/deployments/{deploymentId}/revisions", IDParam: "deploymentId", Body: true, Short: "Create a deployment revision"},
			{Verb: "activate", Method: "POST", Path: "/v1/automation-platform/deployments/{deploymentId}/activate", IDParam: "deploymentId", Short: "Activate a deployment revision"},
			{Verb: "deactivate", Method: "POST", Path: "/v1/automation-platform/deployments/{deploymentId}/deactivate", IDParam: "deploymentId", Short: "Deactivate a deployment"},
			{Verb: "set-default", Method: "POST", Path: "/v1/automation-platform/deployments/{deploymentId}/set-default", IDParam: "deploymentId", Short: "Make a deployment the workflow default"},
			{Verb: "rotate-webhook", Method: "POST", Path: "/v1/automation-platform/deployments/{deploymentId}/webhooks/{nodeId}/rotate", IDParam: "deploymentId", IDParam2: "nodeId", Short: "Rotate a webhook signing key (revealed once)"},
			{Verb: "run", Method: "POST", Path: "/v1/automation-platform/deployments/{deploymentId}/run", IDParam: "deploymentId", Body: true, Short: "Execute a deployment with --header Idempotency-Key=<key>"},
		}},
	)
	for i := range Registry {
		for j := range Registry[i].Actions {
			a := &Registry[i].Actions[j]
			switch a.Verb {
			case "save-draft":
				a.Headers = map[string]string{"if_match": "If-Match"}
			case "retry-run", "create-deployment":
				a.Headers = map[string]string{"idempotency_key": "Idempotency-Key"}
			case "add-revision":
				if Registry[i].Parent == "workflows" {
					a.Headers = map[string]string{"if_match": "If-Match", "idempotency_key": "Idempotency-Key"}
				}
			case "run":
				if Registry[i].Parent == "workflows" {
					a.Headers = map[string]string{"idempotency_key": "Idempotency-Key"}
				}
			}
		}
	}

}

func auditFinalRoutes() {
	for i := range Registry {
		n := &Registry[i]
		if n.Parent == "" && (n.Name == "agents" || n.Name == "workflows") {
			n.Actions = append(n.Actions, Action{Verb: "validate-yaml", Method: "POST", Path: n.Path + "/validate", Body: true, BodyContentType: "application/yaml", Short: "Validate a raw YAML definition from -f"})
		}
	}
	for i := range Registry {
		n := &Registry[i]
		switch n.Parent + "/" + n.Name {
		case "/workflows":
			n.Actions = append(n.Actions, Action{Verb: "create-legacy", Method: "POST", Path: n.Path, Body: true, Short: "Create a workflow through the deprecated compatibility endpoint", SupersededBy: "create"})
			n.Actions = append(n.Actions,
				Action{Verb: "set-runtime-targets", Method: "PUT", Path: n.Path + "/{id}/runtime-targets", IDParam: "id", Body: true, Short: "Set workflow runtime targets"},
				Action{Verb: "preview-node", Method: "POST", Path: WorkflowsCanonicalPath + "/{id}/preview-node", IDParam: "id", Body: true, Short: "Preview a node against an execution"},
				Action{Verb: "run-payload", Method: "GET", Path: n.Path + "/runs/{id}/payloads/{payloadID}", IDParam: "id", IDParam2: "payloadID", Short: "Read a workflow run payload"},
				Action{Verb: "set-collaborator", Method: "PUT", Path: WorkflowsCanonicalPath + "/{id}/collaborators/{kind}/{subjectId}", IDParam: "id", IDParam2: "kind", IDParam3: "subjectId", Body: true, Headers: map[string]string{"if_match": "If-Match"}, Short: "Set workflow collaborator access at a reviewed revision"},
				Action{Verb: "remove-collaborator", Method: "DELETE", Path: WorkflowsCanonicalPath + "/{id}/collaborators/{kind}/{subjectId}", IDParam: "id", IDParam2: "kind", IDParam3: "subjectId", Headers: map[string]string{"if_match": "If-Match"}, Short: "Remove workflow collaborator access at a reviewed revision"},
			)
		case "/agents":
			n.Actions = append(n.Actions, Action{Verb: "set-runtime-targets", Method: "PUT", Path: n.Path + "/{id}/runtime-targets", IDParam: "id", Body: true, Short: "Set agent runtime targets"})
		case "trading/backtests":
			n.Actions = append(n.Actions,
				Action{Verb: "retry-shards", Method: "POST", Path: n.Path + "/{id}/retry-shards", IDParam: "id", Short: "Retry resumable backtest shards"},
				Action{Verb: "reindex-segments", Method: "POST", Path: n.Path + "/{id}/reindex-segments", IDParam: "id", Short: "Rebuild the backtest segment index from object storage"})
		case "drone/workflows":
			n.Actions = append(n.Actions,
				Action{Verb: "validate", Method: "POST", Path: n.Path + "/validate", Body: true, Short: "Validate a drone workflow"},
				Action{Verb: "run", Method: "POST", Path: n.Path + "/{id}/run", IDParam: "id", Short: "Record a pending drone workflow run (executor is not implemented)"},
				Action{Verb: "runs", Method: "GET", Path: n.Path + "/{id}/runs", IDParam: "id", Short: "List drone workflow runs"},
				Action{Verb: "run-status", Method: "GET", Path: n.Path + "/runs/{id}", IDParam: "id", Short: "Get a drone workflow run"},
				Action{Verb: "run-steps", Method: "GET", Path: n.Path + "/runs/{id}/steps", IDParam: "id", Short: "List drone workflow run steps"})
		}
	}
	Registry = append(Registry, Noun{Name: "destinations", Parent: "alerts", Group: "Observability", Path: "/v1/alerts-manager/destinations", Verbs: []Verb{List, Create}, Short: "Alert delivery destinations"})
}
