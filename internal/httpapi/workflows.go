package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"apihub-go/internal/background"
	"apihub-go/internal/cronx"
	"apihub-go/internal/jsonutil"
	"apihub-go/internal/model"
	"apihub-go/internal/policy"
	"apihub-go/internal/safejson"
	"apihub-go/internal/store"
	"apihub-go/internal/workflow"
	"github.com/go-chi/chi/v5"
	"strings"
)

// workflowSyncBudget matches the HTTP server ReadTimeout of 40s so the
// response envelope (with its operationId) is always writable.
const workflowSyncBudget = 40 * time.Second

func (a *api) listWorkflows(w http.ResponseWriter, r *http.Request) {
	items, err := a.Store.ListWorkflows(r.Context())
	writeStoreResult(w, items, err, "list workflows")
}

func (a *api) getWorkflow(w http.ResponseWriter, r *http.Request) {
	item, err := a.Store.Workflow(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.writeStoreError(w, r, err, "get workflow")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

type workflowSaveRequest struct {
	WorkflowKey      string          `json:"workflowKey"`
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	Graph            json.RawMessage `json:"graph"`
	ScheduleType     string          `json:"scheduleType"`
	CronExpression   string          `json:"cronExpression"`
	ScheduleTimezone string          `json:"scheduleTimezone"`
	RetryPolicy      json.RawMessage `json:"retryPolicy"`
	Input            json.RawMessage `json:"input"`
	Version          int64           `json:"version"`
	EditorLayout     json.RawMessage `json:"editorLayout"`
	LayoutVersion    int64           `json:"layoutVersion"`
}

func (a *api) saveWorkflow(w http.ResponseWriter, r *http.Request) {
	var request workflowSaveRequest
	if !decodeJSON(w, r, &request, false) {
		return
	}
	request.WorkflowKey = strings.ToLower(clean(request.WorkflowKey, 100))
	request.Name = clean(request.Name, 180)
	request.Description = clean(request.Description, 1000)
	if !validIdentifier(request.WorkflowKey) || request.Name == "" {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "workflowKey and name are required")
		return
	}
	if len(request.Graph) > workflow.MaxGraphBytes {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "graph exceeds 256 KiB")
		return
	}
	if len(request.Input) > workflow.MaxInputBytes {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "workflow default input exceeds 64 KiB")
		return
	}
	if request.ScheduleType == "" {
		request.ScheduleType = "manual"
	}
	if err := validateWorkflowSchedule(request.ScheduleType, request.CronExpression, defaultTimezone(request.ScheduleTimezone)); err != nil {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	if len(request.RetryPolicy) > 0 {
		var retry struct {
			MaxAttempts int `json:"maxAttempts"`
		}
		if err := jsonutil.Unmarshal(defaultJSON(request.RetryPolicy, `{"maxAttempts":1}`), &retry); err != nil || retry.MaxAttempts != 1 {
			writeAdminError(w, http.StatusBadRequest, "invalid_input", "retryPolicy.maxAttempts must be 1 in this version")
			return
		}
	} else {
		request.RetryPolicy = json.RawMessage(`{"maxAttempts":1}`)
	}
	var inputObject map[string]any
	if err := jsonutil.Unmarshal(defaultJSON(request.Input, `{}`), &inputObject); err != nil || inputObject == nil {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "workflow default input must be a JSON object")
		return
	}
	definition, err := workflow.ParseDefinition(request.Graph)
	if err != nil {
		writeWorkflowError(w, http.StatusBadRequest, "invalid_graph", err)
		return
	}
	if err := workflow.ValidateDefinition(definition, false, func(key string) bool {
		_, loadErr := a.Store.Action(r.Context(), key)
		return loadErr == nil
	}); err != nil {
		writeWorkflowError(w, http.StatusBadRequest, "invalid_graph", err)
		return
	}
	id := chi.URLParam(r, "id")
	item := model.Workflow{
		ID: id, WorkflowKey: request.WorkflowKey, Name: request.Name,
		Description: request.Description, Graph: request.Graph,
		ScheduleType: request.ScheduleType, CronExpression: clean(request.CronExpression, 120),
		ScheduleTimezone: defaultTimezone(request.ScheduleTimezone),
		RetryPolicy:      request.RetryPolicy, Input: request.Input, EditorLayout: request.EditorLayout,
	}
	if len(request.EditorLayout) > 0 {
		if _, err := workflow.NormalizeLayout(request.EditorLayout, definition, false); err != nil {
			writeWorkflowError(w, 400, "invalid_layout", workflow.Located(err, "invalid_layout", "", "definition", "/editorLayout"))
			return
		}
	}
	var saved model.Workflow
	if id == "" {
		saved, err = a.Store.SaveWorkflow(r.Context(), item, 0)
	} else {
		current, loadErr := a.Store.Workflow(r.Context(), id)
		if loadErr != nil {
			a.writeStoreError(w, r, loadErr, "get workflow")
			return
		}
		if request.Version <= 0 || request.Version != current.Version {
			writeAdminError(w, http.StatusConflict, "conflict", "工作流已被其他成员修改，请刷新后重试")
			return
		}
		if request.EditorLayout != nil {
			saved, err = a.Store.SaveWorkflow(r.Context(), item, current.Version, request.LayoutVersion)
		} else {
			saved, err = a.Store.SaveWorkflow(r.Context(), item, current.Version)
		}
	}
	if err != nil {
		a.writeStoreError(w, r, err, "save workflow")
		return
	}
	a.audit(r, "workflow.saved", "workflow", saved.ID, saved.Name, nil, workflowAuditSummary(saved))
	writeJSON(w, statusForSave(r), saved)
}

func (a *api) deleteWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.DeleteWorkflow(r.Context(), id); err != nil {
		a.writeStoreError(w, r, err, "delete workflow")
		return
	}
	a.audit(r, "workflow.deleted", "workflow", id, id, nil, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) deployWorkflow(w http.ResponseWriter, r *http.Request) {
	item, err := a.Store.Workflow(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.writeStoreError(w, r, err, "get workflow")
		return
	}
	if !workflowExpectedVersion(w, r, item) {
		return
	}
	if err := a.Store.CheckWorkflowAdmission(item); err != nil {
		writeAdminError(w, 409, "workflow_capability_disabled", err.Error())
		return
	}
	resolved, err := a.Store.ResolveWorkflowBindings(r.Context(), item)
	if err != nil {
		a.writeStoreError(w, r, err, "resolve workflow bindings")
		return
	}
	definition, err := workflow.ParseDefinition(resolved.Graph)
	if err != nil {
		a.writeStoreError(w, r, err, "parse workflow graph")
		return
	}
	if err := workflow.ValidateDefinition(definition, true, nil); err != nil {
		writeWorkflowError(w, http.StatusConflict, "workflow_not_deployable", err)
		return
	}
	if err := validateWorkflowSchedule(resolved.ScheduleType, resolved.CronExpression, resolved.ScheduleTimezone); err != nil {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	if definition.SchemaVersion == 2 && resolved.ScheduleType != "manual" {
		var input map[string]any
		if jsonutil.Unmarshal(resolved.Input, &input) != nil || input == nil {
			writeAdminError(w, 400, "invalid_input", "scheduled default input must be an object")
			return
		}
		if err := workflow.ValidateTrigger(definition.InputSchema, input); err != nil {
			writeConfigurationError(w, "input", err)
			return
		}
		metadata, err := workflow.NewRunMetadata(now(), nil, resolved.ScheduleTimezone)
		if err == nil {
			_, err = workflow.InitialVariables(definition, input, metadata)
		}
		if err != nil {
			writeAdminError(w, 400, "invalid_variables", "scheduled variable initialization is invalid")
			return
		}
	}
	compileCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	for _, step := range definition.Steps {
		if step.Type == "code" {
			if err := a.Store.WorkflowCodeRunner().Compile(compileCtx, step, false); err != nil {
				writeWorkflowError(w, 409, workflow.CodeErrorCode(err), err)
				return
			}
		}
	}
	a.audited(func(w http.ResponseWriter, r *http.Request) {
		next := computeNextRun(resolved.ScheduleType, resolved.CronExpression, resolved.ScheduleTimezone, now())
		saved, err := a.Store.DeployWorkflow(r.Context(), resolved.ID, resolved.Version, resolved.Graph, next)
		if err != nil {
			a.writeStoreError(w, r, err, "deploy workflow")
			return
		}
		a.audit(r, "workflow.deployed", "workflow", saved.ID, saved.Name, nil, workflowAuditSummary(saved))
		writeJSON(w, http.StatusOK, saved)
	})(w, r)
}

func (a *api) pauseWorkflow(w http.ResponseWriter, r *http.Request) {
	item, err := a.Store.Workflow(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.writeStoreError(w, r, err, "get workflow")
		return
	}
	if !workflowExpectedVersion(w, r, item) {
		return
	}
	saved, err := a.Store.PauseWorkflow(r.Context(), item.ID, item.Version)
	if err != nil {
		a.writeStoreError(w, r, err, "pause workflow")
		return
	}
	a.audit(r, "workflow.paused", "workflow", saved.ID, saved.Name, nil, nil)
	writeJSON(w, http.StatusOK, saved)
}

// runWorkflowNow queues a manual control-plane run; editing or pausing never
// affects accepted runs, which execute their pinned snapshot.
func (a *api) runWorkflowNow(w http.ResponseWriter, r *http.Request) {
	item, err := a.Store.Workflow(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.writeStoreError(w, r, err, "get workflow")
		return
	}
	if item.Status != "deployed" {
		writeAdminError(w, http.StatusConflict, "workflow_not_deployed", "workflow must be deployed before it can run")
		return
	}
	trigger, ok := a.effectiveTrigger(w, r, item)
	if !ok {
		return
	}
	operation, err := a.Store.EnqueueWorkflow(r.Context(), store.WorkflowTriggerInput{
		Workflow: item, Trigger: trigger,
		Principal: workflow.Principal{Kind: "admin"},
		Source:    "manual", RequestID: requestID(r), Codec: a.Codec,
	})
	if err != nil {
		a.writeStoreError(w, r, err, "enqueue workflow")
		return
	}
	a.audit(r, "workflow.run_queued", "workflow", item.ID, item.Name, nil, map[string]any{"operationId": operation.ID})
	writeJSON(w, http.StatusAccepted, operation)
}

// workflowRunResult decrypts the final output for owners, admins and
// developers; viewers are denied and every read is audited.
func (a *api) workflowRunResult(w http.ResponseWriter, r *http.Request) {
	identity := currentAdmin(r)
	if identity.Role != "owner" && identity.Role != "admin" && identity.Role != "developer" {
		writeAdminError(w, http.StatusForbidden, "forbidden", "Your role does not permit this operation")
		return
	}
	id := chi.URLParam(r, "id")
	operation, err := a.Store.Operation(r.Context(), id)
	if err != nil || operation.Kind != "workflow" {
		writeAdminError(w, http.StatusNotFound, "not_found", "workflow run was not found")
		return
	}
	if operation.Status != "success" {
		writeAdminError(w, http.StatusConflict, "run_not_finished", "workflow run has no final output yet")
		return
	}
	output, err := a.Store.WorkflowRunResult(r.Context(), a.Codec, id)
	if err != nil {
		a.writeStoreError(w, r, err, "decrypt workflow result")
		return
	}
	a.audit(r, "workflow.result_read", "workflow_run", id, operation.Name, nil, nil)
	writeJSON(w, http.StatusOK, map[string]any{"operationId": id, "output": json.RawMessage(output)})
}

// effectiveTrigger reads the optional {"input": {...}} body. Omitted input
// falls back to the workflow default; an explicit object overrides it.
func (a *api) effectiveTrigger(w http.ResponseWriter, r *http.Request, item model.Workflow) (map[string]any, bool) {
	var request struct {
		Input           json.RawMessage `json:"input"`
		ExpectedVersion *int64          `json:"expectedVersion"`
	}
	if !decodeJSON(w, r, &request, true) {
		return nil, false
	}
	if request.ExpectedVersion != nil && (*request.ExpectedVersion <= 0 || *request.ExpectedVersion != item.Version) {
		writeAdminError(w, 409, "conflict", "workflow version changed")
		return nil, false
	}
	if len(request.Input) == 0 || string(request.Input) == "null" {
		var defaults map[string]any
		if err := jsonutil.Unmarshal(item.Input, &defaults); err != nil || defaults == nil {
			writeAdminError(w, http.StatusBadRequest, "invalid_input", "workflow default input must be a JSON object")
			return nil, false
		}
		return defaults, true
	}
	if len(request.Input) > workflow.MaxInputBytes {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "workflow trigger input exceeds 64 KiB")
		return nil, false
	}
	var trigger map[string]any
	if err := jsonutil.Unmarshal(request.Input, &trigger); err != nil || trigger == nil {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "workflow trigger input must be a JSON object")
		return nil, false
	}
	return trigger, true
}

func validateWorkflowSchedule(scheduleType, cronExpression, timezone string) error {
	if _, err := cronx.LoadTimezone(timezone); err != nil {
		return fmt.Errorf("schedule timezone invalid: %v", err)
	}
	switch scheduleType {
	case "manual":
		return nil
	case "interval":
		if cronx.IntervalDuration(cronExpression) == 0 {
			return errors.New("interval must be one of 每 15 分钟, 每 30 分钟, 每小时 or 每天")
		}
		return nil
	case "cron":
		if _, err := cronx.Parse(cronExpression); err != nil {
			return fmt.Errorf("cron expression invalid: %v", err)
		}
		if _, err := cronx.LoadTimezone(timezone); err != nil {
			return fmt.Errorf("schedule timezone invalid: %v", err)
		}
		return nil
	default:
		return errors.New("scheduleType must be manual, interval or cron")
	}
}

func defaultTimezone(value string) string {
	if value == "" {
		return "UTC"
	}
	return value
}

func computeNextRun(scheduleType, cronExpression, timezone string, from time.Time) *time.Time {
	switch scheduleType {
	case "interval":
		duration := cronx.IntervalDuration(cronExpression)
		if duration == 0 {
			return nil
		}
		next := from.Add(duration).UTC()
		return &next
	case "cron":
		schedule, err := cronx.Parse(cronExpression)
		if err != nil {
			return nil
		}
		location, err := cronx.LoadTimezone(timezone)
		if err != nil {
			return nil
		}
		next, ok := schedule.Next(from, location)
		if !ok {
			return nil
		}
		next = next.UTC()
		return &next
	default:
		return nil
	}
}

// workflowDiscoveryItem is the runtime-plane projection: no bindings, no
// credentials, no raw default input.
type workflowDiscoveryItem struct {
	WorkflowKey string              `json:"workflowKey"`
	Version     int64               `json:"version"`
	InputSchema map[string]any      `json:"inputSchema,omitempty"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Steps       []workflowStepBrief `json:"steps"`
}

type workflowStepBrief struct {
	ID     string `json:"id"`
	Title  string `json:"title,omitempty"`
	Action string `json:"action,omitempty"`
	Type   string `json:"type,omitempty"`
}

// loadDeployedWorkflows returns deployed definitions with the token policy
// precheck for every step action and bound connection, batch-loading lookups
// to avoid per-workflow queries.
func (a *api) runtimeWorkflowSummaries(ctx context.Context, token model.RuntimeToken) ([]workflowDiscoveryItem, []model.Workflow, error) {
	items, err := a.Store.ListWorkflows(ctx)
	if err != nil {
		return nil, nil, err
	}
	actions, err := a.Store.ListActions(ctx, "", "")
	if err != nil {
		return nil, nil, err
	}
	actionKeys := map[string]model.ActionDefinition{}
	counts := map[string]int{}
	for _, action := range actions {
		counts[action.ActionKey]++
	}
	for _, action := range actions {
		actionKeys[action.ID] = action
		actionKeys[action.SystemKey+"."+action.ActionKey] = action
		if counts[action.ActionKey] == 1 {
			actionKeys[action.ActionKey] = action
		}
	}
	connections, err := a.Store.ListConnections(ctx)
	if err != nil {
		return nil, nil, err
	}
	byID := map[string]model.Connection{}
	byKey := map[string]model.Connection{}
	for _, connection := range connections {
		byID[connection.ID] = connection
		byKey[connection.ConnectionKey] = connection
	}
	summaries := make([]workflowDiscoveryItem, 0, len(items))
	deployed := make([]model.Workflow, 0, len(items))
	for _, item := range items {
		if item.Status != "deployed" {
			continue
		}
		definition, err := workflow.ParseDefinition(item.Graph)
		if err != nil {
			continue
		}
		if a.Store.CheckWorkflowAdmission(item) != nil || workflow.ValidateDefinition(definition, true, nil) != nil {
			continue
		}
		allowed := true
		for _, step := range definition.Steps {
			if !step.IsAPI() {
				continue
			}
			action, exists := actionKeys[step.Action]
			if !exists || !action.Executable || action.Status != "active" || !policy.AllowsAction(token, action.ActionKey) {
				allowed = false
				break
			}
			if step.ConnectionKey == "" {
				continue
			}
			connection, ok := byID[step.ConnectionKey]
			if !ok {
				connection, ok = byKey[step.ConnectionKey]
			}
			if !ok || !policy.AllowsConnection(token, connection) {
				allowed = false
				break
			}
		}
		if !allowed {
			continue
		}
		summary := workflowDiscoveryItem{WorkflowKey: item.WorkflowKey, Version: item.Version, InputSchema: definition.InputSchema, Name: item.Name, Description: item.Description}
		for _, step := range definition.Steps {
			summary.Steps = append(summary.Steps, workflowStepBrief{ID: step.ID, Title: step.Title, Action: step.Action, Type: step.Type})
		}
		summaries = append(summaries, summary)
		deployed = append(deployed, item)
	}
	return summaries, deployed, nil
}

func (a *api) runtimeWorkflows(w http.ResponseWriter, r *http.Request) {
	summaries, _, err := a.runtimeWorkflowSummaries(r.Context(), runtimeToken(r))
	if err != nil {
		writeRuntimeError(w, http.StatusInternalServerError, "internal_error", "Could not list workflows", nil)
		return
	}
	writeRuntime(w, http.StatusOK, summaries, map[string]any{"count": len(summaries)})
}

func (a *api) runtimeWorkflow(w http.ResponseWriter, r *http.Request) {
	summaries, _, err := a.runtimeWorkflowSummaries(r.Context(), runtimeToken(r))
	if err != nil {
		writeRuntimeError(w, http.StatusInternalServerError, "internal_error", "Could not load workflow", nil)
		return
	}
	key := chi.URLParam(r, "key")
	for _, summary := range summaries {
		if summary.WorkflowKey == key {
			writeRuntime(w, http.StatusOK, summary, map[string]any{
				"inputContract": "trigger input must be a single JSON object; omitted input uses the deployed default",
			})
			return
		}
	}
	writeRuntimeError(w, http.StatusNotFound, "workflow_not_found", "workflow was not found or policy denies it", nil)
}

// triggerWorkflow runs the workflow synchronously by default; ?async=1
// queues it and answers 202 with an operationId to poll.
func (a *api) triggerWorkflow(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	token := runtimeToken(r)
	item, err := a.Store.Workflow(r.Context(), key)
	if err != nil {
		writeRuntimeError(w, http.StatusNotFound, "workflow_not_found", "workflow was not found", nil)
		return
	}
	if item.Status != "deployed" {
		writeRuntimeError(w, http.StatusConflict, "workflow_not_deployed", "workflow is not deployed", nil)
		return
	}
	definition, err := workflow.ParseDefinition(item.Graph)
	if err != nil {
		writeRuntimeError(w, http.StatusInternalServerError, "internal_error", "workflow definition is invalid", nil)
		return
	}
	if reason, ok := a.workflowPolicyCheck(r.Context(), token, definition); !ok {
		writeRuntimeError(w, http.StatusForbidden, "policy_denied", reason, nil)
		return
	}
	if err := a.Store.CheckWorkflowAdmission(item); err != nil {
		writeRuntimeError(w, 409, "workflow_capability_disabled", "workflow capability is disabled", nil)
		return
	}
	trigger, ok := a.effectiveTrigger(w, r, item)
	if !ok {
		return
	}
	async := r.URL.Query().Get("async") == "1"
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))

	if async {
		a.triggerWorkflowAsync(w, r, item, token, trigger, idempotencyKey)
		return
	}
	a.triggerWorkflowSync(w, r, item, token, trigger, idempotencyKey)
}

// workflowPolicyCheck verifies the token covers every step action and bound
// connection before anything is enqueued; step-time checks repeat per step.
func (a *api) workflowPolicyCheck(ctx context.Context, token model.RuntimeToken, definition workflow.Definition) (string, bool) {
	for _, step := range definition.Steps {
		if !step.IsAPI() {
			continue
		}
		loaded, err := a.Store.Action(ctx, step.Action)
		if err != nil {
			return "workflow API is unavailable", false
		}
		if !policy.AllowsAction(token, loaded.ActionKey) {
			return "runtime policy denied action " + step.Action, false
		}
	}
	connections, err := a.Store.ListConnections(ctx)
	if err != nil {
		return "could not load connections", false
	}
	byID := map[string]model.Connection{}
	byKey := map[string]model.Connection{}
	for _, connection := range connections {
		byID[connection.ID] = connection
		byKey[connection.ConnectionKey] = connection
	}
	for _, step := range definition.Steps {
		if !step.IsAPI() {
			continue
		}
		if step.ConnectionKey == "" {
			continue
		}
		connection, ok := byID[step.ConnectionKey]
		if !ok {
			connection, ok = byKey[step.ConnectionKey]
		}
		if !ok {
			return "bound connection for step " + step.ID + " is unavailable", false
		}
		if !policy.AllowsConnection(token, connection) {
			return "runtime policy denied the connection of step " + step.ID, false
		}
	}
	return "", true
}

func (a *api) workflowFingerprint(item model.Workflow, trigger map[string]any, mode string) string {
	fingerprintBytes, _ := json.Marshal(map[string]any{
		"workflowId": item.ID, "version": item.Version, "trigger": trigger, "mode": mode,
	})
	sum := sha256.Sum256(fingerprintBytes)
	return hex.EncodeToString(sum[:])
}

func (a *api) claimWorkflowIdempotency(w http.ResponseWriter, r *http.Request, item model.Workflow, trigger map[string]any, mode string, idempotencyKey string) (model.IdempotencyRecord, bool, bool) {
	if idempotencyKey == "" {
		return model.IdempotencyRecord{}, false, true
	}
	if len(idempotencyKey) > 256 {
		writeRuntimeError(w, http.StatusBadRequest, "invalid_idempotency_key", "Idempotency-Key must not exceed 256 characters", nil)
		return model.IdempotencyRecord{}, false, false
	}
	record := model.IdempotencyRecord{
		ID: newID("idempotency"), RuntimeTokenID: runtimeToken(r).ID,
		Scope: "workflow:" + item.ID, Key: idempotencyKey,
		Fingerprint: a.workflowFingerprint(item, trigger, mode),
		ExpiresAt:   now().Add(24 * time.Hour),
	}
	current, claimed, err := a.Store.ClaimIdempotency(r.Context(), record)
	if err != nil {
		writeRuntimeError(w, http.StatusInternalServerError, "internal_error", "Could not claim idempotency key", nil)
		return model.IdempotencyRecord{}, false, false
	}
	if claimed {
		return record, true, true
	}
	if current.Fingerprint != record.Fingerprint {
		// Redeployment changes the version and therefore the fingerprint;
		// callers must switch keys.
		writeRuntimeError(w, http.StatusConflict, "idempotency_conflict", "Idempotency-Key was used with a different request", nil)
		return model.IdempotencyRecord{}, false, false
	}
	if current.Status != "completed" {
		writeRuntimeError(w, http.StatusConflict, "idempotency_in_progress", "a request with this Idempotency-Key is still running", map[string]any{"operationId": current.OperationID})
		return model.IdempotencyRecord{}, false, false
	}
	// Replay path: rebuild the original answer from the stored reference
	// envelope, never from a plaintext response copy.
	var envelope struct {
		OperationID string `json:"operationId"`
		Mode        string `json:"mode"`
		RunStatus   string `json:"runStatus"`
		HTTPStatus  int    `json:"httpStatus"`
		Code        string `json:"code"`
		Message     string `json:"message"`
	}
	if jsonutil.Unmarshal(current.Response, &envelope) != nil || envelope.OperationID == "" {
		writeRuntimeError(w, http.StatusConflict, "idempotency_conflict", "Idempotency-Key replay is unavailable", nil)
		return model.IdempotencyRecord{}, false, false
	}
	switch {
	case envelope.Mode == "async":
		writeRuntime(w, http.StatusAccepted, map[string]any{"operationId": envelope.OperationID, "status": "queued"}, map[string]any{"operationId": envelope.OperationID})
	case envelope.RunStatus == "success":
		output, err := a.Store.WorkflowRunResult(r.Context(), a.Codec, envelope.OperationID)
		if err != nil {
			writeRuntimeError(w, http.StatusConflict, "idempotency_conflict", "Idempotency-Key result is no longer available", nil)
			return model.IdempotencyRecord{}, false, false
		}
		writeRuntime(w, envelope.HTTPStatus, json.RawMessage(output), map[string]any{"operationId": envelope.OperationID, "replayed": true})
	default:
		writeRuntimeError(w, envelope.HTTPStatus, envelope.Code, envelope.Message, map[string]any{"operationId": envelope.OperationID, "replayed": true})
	}
	return model.IdempotencyRecord{}, false, false
}

func (a *api) triggerWorkflowAsync(w http.ResponseWriter, r *http.Request, item model.Workflow, token model.RuntimeToken, trigger map[string]any, idempotencyKey string) {
	record, _, ok := a.claimWorkflowIdempotency(w, r, item, trigger, "async", idempotencyKey)
	if !ok {
		return
	}
	operation, err := a.Store.EnqueueWorkflow(r.Context(), store.WorkflowTriggerInput{
		Workflow: item, Trigger: trigger,
		Principal: workflow.Principal{Kind: "runtime_token", TokenID: token.ID},
		Source:    "runtime", RequestID: requestID(r), Codec: a.Codec, IdempotencyID: record.ID,
	})
	if err != nil {
		a.releaseIdempotency(r, record)
		a.writeWorkflowTriggerError(w, r, err)
		return
	}
	if record.ID != "" {
		record.HTTPStatus = http.StatusAccepted
		record.OperationID = operation.ID
		envelope := store.MarshalJSON(map[string]any{
			"operationId": operation.ID, "mode": "async",
			"runStatus": "queued", "httpStatus": http.StatusAccepted,
		})
		record.Response = envelope
		if err := a.Store.CompleteIdempotency(r.Context(), record); err != nil {
			a.Logger.Error("complete idempotency", "error", err)
		}
	}
	writeRuntime(w, http.StatusAccepted, map[string]any{"operationId": operation.ID, "status": "queued"}, map[string]any{"operationId": operation.ID})
}

func (a *api) triggerWorkflowSync(w http.ResponseWriter, r *http.Request, item model.Workflow, token model.RuntimeToken, trigger map[string]any, idempotencyKey string) {
	record, _, ok := a.claimWorkflowIdempotency(w, r, item, trigger, "sync", idempotencyKey)
	if !ok {
		return
	}
	operation, snapshot, err := a.Store.CreateWorkflowRun(r.Context(), store.WorkflowTriggerInput{
		Workflow: item, Trigger: trigger,
		Principal: workflow.Principal{Kind: "runtime_token", TokenID: token.ID},
		Source:    "runtime", RequestID: requestID(r), Codec: a.Codec, IdempotencyID: record.ID,
	})
	if err != nil {
		a.releaseIdempotency(r, record)
		a.writeWorkflowTriggerError(w, r, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), workflowSyncBudget)
	defer cancel()
	_ = a.Store.AddOperationEvent(ctx, operation.ID, "info", "开始执行工作流", store.MarshalJSON(map[string]any{"stepCount": len(snapshot.Steps)}))
	runner := background.NewWorkflowRunner(a.Store, a.Auth, a.Executor, a.Catalog)
	result, runErr := runner.Run(ctx, snapshot, workflow.Options{
		Principal: workflow.Principal{Kind: "runtime_token", TokenID: token.ID},
		Attempt:   1,
		OnStepStart: func(outcome workflow.StepOutcome) {
			_ = a.Store.AddOperationEvent(ctx, operation.ID, "info", "workflow.step.started", store.MarshalJSON(workflow.StepEvent(outcome, 1, true)))
		},
		OnStep: func(outcome workflow.StepOutcome) {
			_ = a.Store.AddOperationEvent(ctx, operation.ID, levelForRuntimeStep(outcome.Status), "workflow.step.finished", store.MarshalJSON(workflow.StepEvent(outcome, 1, false)))
		},
	})
	finishCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	_ = a.Store.AddOperationEvent(finishCtx, operation.ID, "info", "workflow.run.finished", store.MarshalJSON(workflow.RunEvent(result, runErr)))
	if runErr == nil {
		plain, marshalErr := json.Marshal(result.Final)
		if marshalErr != nil {
			a.Logger.Error("encode workflow output", "error", marshalErr)
		}
		cipher, sealErr := a.Codec.Encrypt(plain, []byte(a.Store.WorkspaceID()+":operation-run:"+operation.ID))
		if sealErr != nil {
			a.Logger.Error("seal workflow output", "error", sealErr)
			_ = a.Store.FailWorkflowRun(finishCtx, model.Job{}, operation.ID, "unknown", "workflow_result_unknown", "workflow completed but result encryption failed", model.IdempotencyRecord{}, nil)
			writeRuntimeError(w, http.StatusInternalServerError, "workflow_result_unknown", "workflow completed but result encryption failed", map[string]any{"operationId": operation.ID})
			return
		}
		preview := safejson.Marshal(map[string]any{
			"stepCount": len(snapshot.Steps), "hasWarnings": result.HasWarnings,
			"failedSteps": result.FailedSteps,
		}, 4<<10)
		meta := map[string]any{"operationId": operation.ID, "stepCount": len(snapshot.Steps), "hasWarnings": result.HasWarnings}
		if result.HasWarnings {
			meta["failedSteps"] = result.FailedSteps
		}
		var envelope []byte
		if record.ID != "" {
			record.HTTPStatus = http.StatusOK
			record.OperationID = operation.ID
			envelope = store.MarshalJSON(map[string]any{
				"operationId": operation.ID, "mode": "sync", "runStatus": "success", "httpStatus": http.StatusOK,
			})
		}
		if err := a.Store.FinishWorkflowRun(finishCtx, model.Job{}, operation.ID, "success", 0, preview, cipher, "", "", record, envelope); err != nil {
			a.Logger.Error("persist workflow result", "operation_id", operation.ID, "error", err)
			_ = a.Store.FailWorkflowRun(finishCtx, model.Job{}, operation.ID, "unknown", "workflow_result_unknown", "workflow completed but result persistence failed", model.IdempotencyRecord{}, nil)
			writeRuntimeError(w, http.StatusInternalServerError, "workflow_result_unknown", "workflow completed but result persistence failed", map[string]any{"operationId": operation.ID})
			return
		}
		writeRuntime(w, http.StatusOK, result.Final, meta)
		return
	}
	var typed *workflow.Error
	if !errors.As(runErr, &typed) {
		a.Logger.Error("run workflow", "operation_id", operation.ID, "error", runErr)
		typed = &workflow.Error{Code: "internal_error", Message: "workflow run failed", Status: "failed"}
	}
	status := "failed"
	httpStatus := http.StatusBadGateway
	if typed.Status == workflow.StatusUnknown {
		status = "unknown"
	}
	var envelope []byte
	if record.ID != "" {
		record.HTTPStatus = httpStatus
		record.OperationID = operation.ID
		envelope = store.MarshalJSON(map[string]any{
			"operationId": operation.ID, "mode": "sync", "runStatus": status,
			"httpStatus": httpStatus, "code": typed.Code, "message": typed.Error(),
		})
	}
	if err := a.Store.FailWorkflowRun(finishCtx, model.Job{}, operation.ID, status, typed.Code, typed.Error(), record, envelope); err != nil {
		a.Logger.Error("persist workflow failure", "operation_id", operation.ID, "error", err)
	}
	writeRuntimeError(w, httpStatus, typed.Code, typed.Error(), map[string]any{"operationId": operation.ID, "stepCount": len(snapshot.Steps)})
}

func (a *api) releaseIdempotency(r *http.Request, record model.IdempotencyRecord) {
	if record.ID == "" {
		return
	}
	cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.Store.ReleaseIdempotency(cleanup, record.ID); err != nil {
		a.Logger.Error("release idempotency claim", "error", err)
	}
}

func (a *api) writeWorkflowTriggerError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrBusy):
		writeRuntimeError(w, http.StatusTooManyRequests, "workflow_busy", "too many workflow runs are queued or running; retry later", nil)
	case errors.Is(err, store.ErrConflict), store.IsUniqueViolation(err):
		writeRuntimeError(w, http.StatusConflict, "conflict", err.Error(), nil)
	case errors.Is(err, store.ErrNotFound):
		writeRuntimeError(w, http.StatusNotFound, "workflow_not_found", "workflow or its bindings are unavailable", nil)
	default:
		a.Logger.ErrorContext(r.Context(), "trigger workflow", "error", err)
		writeRuntimeError(w, http.StatusInternalServerError, "internal_error", "Could not start the workflow run", nil)
	}
}

// runtimeWorkflowRun reports run status for the token that initiated it;
// other tokens learn nothing about the run's existence.
func (a *api) runtimeWorkflowRun(w http.ResponseWriter, r *http.Request) {
	token := runtimeToken(r)
	operation, err := a.Store.Operation(r.Context(), chi.URLParam(r, "id"))
	if err != nil || operation.Kind != "workflow" {
		writeRuntimeError(w, http.StatusNotFound, "workflow_run_not_found", "workflow run was not found", nil)
		return
	}
	if operation.RuntimeTokenID != token.ID {
		writeRuntimeError(w, http.StatusNotFound, "workflow_run_not_found", "workflow run was not found", nil)
		return
	}
	data := map[string]any{
		"operationId": operation.ID,
		"status":      operation.Status,
		"startedAt":   operation.StartedAt,
		"completedAt": operation.CompletedAt,
	}
	if operation.ErrorCode != "" {
		data["error"] = map[string]any{"code": operation.ErrorCode, "message": operation.ErrorMessage}
	}
	if operation.Status == "success" {
		var summary struct {
			HasWarnings bool     `json:"hasWarnings"`
			FailedSteps []string `json:"failedSteps"`
		}
		if jsonutil.Unmarshal(operation.Output, &summary) == nil {
			data["hasWarnings"] = summary.HasWarnings
			if summary.HasWarnings {
				data["failedSteps"] = summary.FailedSteps
			}
		}
		if output, err := a.Store.WorkflowRunResult(r.Context(), a.Codec, operation.ID); err == nil {
			data["output"] = json.RawMessage(output)
		}
	}
	writeRuntime(w, http.StatusOK, data, map[string]any{"operationId": operation.ID})
}

func levelForRuntimeStep(status string) string {
	switch status {
	case workflow.StatusSuccess, workflow.StatusSkipped:
		return "info"
	case workflow.StatusUnknown:
		return "error"
	default:
		return "warn"
	}
}
