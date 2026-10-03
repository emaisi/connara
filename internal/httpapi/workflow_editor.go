package httpapi

import (
	"apihub-go/internal/jsonutil"
	"apihub-go/internal/model"
	"apihub-go/internal/safejson"
	"apihub-go/internal/store"
	"apihub-go/internal/workflow"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"net/http"
	"time"
)

func workflowAuditSummary(item model.Workflow) json.RawMessage {
	def, err := workflow.ParseDefinition(item.Graph)
	if err != nil {
		return nil
	}
	nodes := make([]map[string]any, 0, len(def.Steps))
	for _, step := range def.Steps {
		kind := step.Type
		if kind == "" {
			kind = "api"
		}
		entry := map[string]any{"id": step.ID, "type": kind}
		if kind == "code" {
			digest := sha256.Sum256([]byte(step.Code))
			entry["sourceSHA256"] = hex.EncodeToString(digest[:])
			entry["sourceBytes"] = len(step.Code)
			entry["runtimeProfile"] = step.RuntimeProfile
		}
		nodes = append(nodes, entry)
	}
	return safejson.Marshal(map[string]any{"id": item.ID, "version": item.Version, "status": item.Status, "nodes": nodes}, 16<<10)
}
func workflowExpectedVersion(w http.ResponseWriter, r *http.Request, item model.Workflow) bool {
	var request struct {
		ExpectedVersion *int64 `json:"expectedVersion"`
	}
	if !decodeJSON(w, r, &request, true) {
		return false
	}
	if request.ExpectedVersion != nil && (*request.ExpectedVersion <= 0 || *request.ExpectedVersion != item.Version) {
		writeAdminError(w, 409, "conflict", "workflow version changed")
		return false
	}
	return true
}
func (a *api) saveWorkflowLayout(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ExpectedWorkflowVersion int64           `json:"expectedWorkflowVersion"`
		LayoutVersion           int64           `json:"layoutVersion"`
		EditorLayout            json.RawMessage `json:"editorLayout"`
	}
	if !decodeJSON(w, r, &request, false) {
		return
	}
	if len(request.EditorLayout) == 0 {
		writeAdminError(w, 400, "invalid_layout", "editorLayout is required")
		return
	}
	item, err := a.Store.Workflow(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.writeStoreError(w, r, err, "get workflow")
		return
	}
	definition, err := workflow.ParseDefinition(item.Graph)
	if err != nil {
		writeWorkflowError(w, 400, "invalid_graph", err)
		return
	}
	if _, err := workflow.NormalizeLayout(request.EditorLayout, definition, false); err != nil {
		writeWorkflowError(w, 400, "invalid_layout", workflow.Located(err, "invalid_layout", "", "definition", "/editorLayout"))
		return
	}
	saved, err := a.Store.SaveWorkflowLayout(r.Context(), chi.URLParam(r, "id"), request.ExpectedWorkflowVersion, request.LayoutVersion, request.EditorLayout)
	if err != nil {
		a.writeStoreError(w, r, err, "save workflow layout")
		return
	}
	a.audit(r, "workflow.layout_saved", "workflow", saved.ID, saved.Name, nil, map[string]any{"layoutVersion": saved.LayoutVersion})
	writeJSON(w, 200, saved)
}
func (a *api) workflowCapabilities(w http.ResponseWriter, r *http.Request) {
	features := a.Store.WorkflowFeatures()
	runner := a.Store.WorkflowCodeRunner()
	available := false
	reason := "code_runtime_unavailable"
	concurrency := 2
	if runner != nil {
		concurrency = runner.Concurrency
		if features.CodeEnabled && features.V2Enabled {
			ctx, cancel := context.WithTimeout(r.Context(), workflow.CodeTimeout)
			err := runner.Probe(ctx)
			cancel()
			available = err == nil
			if err != nil {
				reason = workflow.CodeErrorCode(err)
				if reason == "code_capacity_busy" {
					available = runner.EnvironmentAvailable()
				}
			} else {
				reason = ""
			}
		}
	}
	interactive := concurrency - 1
	if concurrency == 1 {
		interactive = 1
	}
	writeJSON(w, 200, map[string]any{"v2Enabled": features.V2Enabled, "code": map[string]any{"enabled": features.CodeEnabled, "backgroundCheck": "on_execution", "local": map[string]any{"available": available, "reasonCode": reason, "runtimeProfile": "js-v1", "limits": map[string]any{"sourceBytes": 32 << 10, "inputBytes": workflow.MaxCodeData, "outputBytes": workflow.MaxCodeData, "timeoutMs": 2000, "memoryBytes": 128 << 20, "maxDepth": 16}, "capacityPolicy": map[string]any{"totalConcurrency": concurrency, "interactiveConcurrency": interactive, "formalPriority": true}}}})
}
func (a *api) previewWorkflowSchedule(w http.ResponseWriter, r *http.Request) {
	if !workflowCanReadSensitive(r) {
		writeAdminError(w, 403, "forbidden", "Your role does not permit this operation")
		return
	}
	var request struct {
		ScheduleType     string `json:"scheduleType"`
		CronExpression   string `json:"cronExpression"`
		ScheduleTimezone string `json:"scheduleTimezone"`
	}
	if !decodeJSON(w, r, &request, false) {
		return
	}
	timezone := defaultTimezone(request.ScheduleTimezone)
	if err := validateWorkflowSchedule(request.ScheduleType, request.CronExpression, timezone); err != nil {
		writeAdminError(w, 400, "invalid_schedule", err.Error())
		return
	}
	evaluated := now()
	times := []time.Time{}
	cursor := evaluated
	for i := 0; i < 3; i++ {
		next := computeNextRun(request.ScheduleType, request.CronExpression, timezone, cursor)
		if next == nil {
			if request.ScheduleType != "manual" {
				writeAdminError(w, 400, "no_future_schedule", "schedule has no future occurrence")
				return
			}
			break
		}
		times = append(times, *next)
		cursor = *next
	}
	writeJSON(w, 200, map[string]any{"evaluatedAt": evaluated, "scheduleTimezone": timezone, "nextRuns": times})
}
func workflowCanReadSensitive(r *http.Request) bool {
	role := currentAdmin(r).Role
	return role == "owner" || role == "admin" || role == "developer"
}
func (a *api) workflowRunView(w http.ResponseWriter, r *http.Request) {
	if !workflowCanReadSensitive(r) {
		writeAdminError(w, 403, "forbidden", "Your role does not permit this operation")
		return
	}
	id := chi.URLParam(r, "id")
	operation, err := a.Store.Operation(r.Context(), id)
	if err != nil || operation.Kind != "workflow" {
		writeAdminError(w, 404, "not_found", "workflow run was not found")
		return
	}
	snapshot, err := a.Store.WorkflowRunSnapshot(r.Context(), a.Codec, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeAdminError(w, 404, "not_found", "workflow run was not found")
		} else {
			writeAdminError(w, 409, "run_view_unavailable", "snapshot is unavailable")
		}
		return
	}
	nodes := make([]map[string]any, 0, len(snapshot.Steps))
	for _, step := range snapshot.Steps {
		kind := step.Type
		if kind == "" {
			kind = "api"
		}
		entry := map[string]any{"id": step.ID, "title": step.Title, "type": kind, "dependsOn": step.DependsOn, "scope": step.Scope, "onError": step.OnError, "inputPreview": json.RawMessage(safejson.Marshal(step.Input, 4<<10)), "assignPreview": json.RawMessage(safejson.Marshal(step.Assign, 4<<10))}
		if kind == "api" {
			entry["actionName"] = step.ActionName
			entry["actionKey"] = step.ActionKey
			entry["actionVersion"] = step.ActionVersion
			entry["httpMethod"] = step.HTTPMethod
			entry["relativePath"] = step.RelativePath
		}
		if kind == "condition" {
			branches := []map[string]any{}
			for _, branch := range step.Branches {
				branches = append(branches, map[string]any{"id": branch.ID, "title": branch.Title, "default": branch.Default})
			}
			entry["branches"] = branches
		}
		if kind == "transform" {
			ops := []string{}
			for _, op := range step.Operations {
				name, _ := op["op"].(string)
				ops = append(ops, name)
			}
			entry["operations"] = ops
		}
		if kind == "code" {
			entry["runtimeProfile"] = step.RuntimeProfile
			entry["inputSchema"] = json.RawMessage(safejson.Marshal(step.InputSchema, 4<<10))
			entry["outputSchema"] = json.RawMessage(safejson.Marshal(step.OutputSchema, 4<<10))
		}
		nodes = append(nodes, entry)
	}
	variables := []map[string]any{}
	for _, variable := range snapshot.Variables {
		variables = append(variables, map[string]any{"name": variable.Name, "type": variable.Type, "nullable": variable.Nullable, "description": variable.Description})
	}
	view := map[string]any{"operationId": id, "workflowId": snapshot.WorkflowID, "workflowVersion": snapshot.WorkflowVersion, "name": snapshot.WorkflowName, "schemaVersion": snapshot.SchemaVersion, "status": operation.Status, "editorLayout": snapshot.EditorLayout, "steps": nodes, "variables": variables, "initialVariablesPreview": json.RawMessage(safejson.Marshal(snapshot.InitialVariables, 4<<10)), "triggerPreview": json.RawMessage(safejson.Marshal(snapshot.Trigger, 4<<10)), "outputPreview": json.RawMessage(safejson.Marshal(snapshot.Output, 4<<10)), "runMetadata": snapshot.RunMetadata}
	var summary map[string]any
	if jsonutil.Unmarshal(operation.Output, &summary) == nil {
		if warnings, ok := summary["hasWarnings"].(bool); ok {
			view["hasWarnings"] = warnings
		}
	}
	a.audit(r, "workflow.run_view_read", "workflow_run", id, operation.Name, nil, nil)
	writeJSON(w, 200, view)
}

func writeWorkflowError(w http.ResponseWriter, status int, code string, err error) {
	diagnostics := workflow.Diagnostics(err)
	if len(diagnostics) == 0 {
		writeAdminError(w, status, code, err.Error())
		return
	}
	writeJSON(w, status, map[string]any{"code": code, "message": err.Error(), "details": map[string]any{"diagnostics": diagnostics}})
}
