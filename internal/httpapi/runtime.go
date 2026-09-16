package httpapi

import (
	"apihub-go/internal/jsonutil"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"apihub-go/internal/executor"
	"apihub-go/internal/model"
	"apihub-go/internal/policy"
	"github.com/go-chi/chi/v5"
)

type actionRequest struct {
	Input          map[string]any `json:"input"`
	ConnectionKey  string         `json:"connectionKey,omitempty"`
	ConnectionName string         `json:"connectionName,omitempty"`
	IntegrationID  string         `json:"integrationId,omitempty"`
}

type actionExecution struct {
	Status    int
	Body      []byte
	Operation model.OperationRun
}

func (a *api) runtimeProviders(w http.ResponseWriter, r *http.Request) {
	items, err := a.Store.ListSystems(r.Context())
	if err != nil {
		writeRuntimeError(w, http.StatusInternalServerError, "internal_error", "Could not list systems", nil)
		return
	}
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	filtered := make([]model.System, 0, len(items))
	for _, item := range items {
		if item.Status != "active" || (query != "" && !strings.Contains(strings.ToLower(item.SystemKey+" "+item.Name+" "+item.Description), query)) {
			continue
		}
		filtered = append(filtered, item)
	}
	writeRuntime(w, http.StatusOK, filtered, map[string]any{"count": len(filtered)})
}

func (a *api) runtimeActions(w http.ResponseWriter, r *http.Request) {
	items, err := a.Store.ListActions(r.Context(), "", r.URL.Query().Get("system"))
	if err != nil {
		writeRuntimeError(w, http.StatusInternalServerError, "internal_error", "Could not list actions", nil)
		return
	}
	items = allowedActions(items, runtimeToken(r))
	writeRuntime(w, http.StatusOK, items, map[string]any{"count": len(items)})
}

func (a *api) runtimeActionSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" || len(query) > 256 {
		writeRuntimeError(w, http.StatusBadRequest, "invalid_input", "q is required and must not exceed 256 characters", nil)
		return
	}
	items, err := a.Store.ListActions(r.Context(), query, r.URL.Query().Get("system"))
	if err != nil {
		writeRuntimeError(w, http.StatusInternalServerError, "internal_error", "Could not search actions", nil)
		return
	}
	items = allowedActions(items, runtimeToken(r))
	limit := parseLimit(r, 10, 50)
	if len(items) > limit {
		items = items[:limit]
	}
	writeRuntime(w, http.StatusOK, items, map[string]any{"count": len(items)})
}

func (a *api) runtimeAction(w http.ResponseWriter, r *http.Request) {
	item, err := a.Store.Action(r.Context(), chi.URLParam(r, "actionKey"))
	if err != nil || item.Status != "active" {
		writeRuntimeError(w, http.StatusNotFound, "action_not_found", "Action was not found", nil)
		return
	}
	if !policy.AllowsAction(runtimeToken(r), item.ActionKey) {
		writeRuntimeError(w, http.StatusForbidden, "policy_denied", "Runtime policy denied this action", nil)
		return
	}
	writeRuntime(w, http.StatusOK, item, nil)
}

func (a *api) executeAction(w http.ResponseWriter, r *http.Request) {
	var request actionRequest
	if !decodeJSON(w, r, &request, true) {
		return
	}
	result, err := a.executeActionCore(r.Context(), chi.URLParam(r, "actionKey"), request, runtimeToken(r), requestID(r), "runtime", strings.TrimSpace(r.Header.Get("Idempotency-Key")))
	if err != nil {
		a.writeExecutionError(w, err)
		return
	}
	writeBytes(w, result.Status, result.Body)
}

func (a *api) executeActionForAdmin(r *http.Request, actionID string, request actionRequest) (int, any) {
	result, err := a.executeActionCore(r.Context(), actionID, request, model.RuntimeToken{}, requestID(r), "manual", "")
	if err != nil {
		status, code := executionErrorStatus(err)
		return status, map[string]any{"code": code, "message": err.Error()}
	}
	return result.Status, providerData(result.Body)
}

func (a *api) executeActionCore(ctx context.Context, actionID string, request actionRequest, token model.RuntimeToken, reqID, source, idempotencyKey string) (actionExecution, error) {
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	action, err := a.Store.Action(ctx, actionID)
	if err != nil || action.Status != "active" {
		return actionExecution{}, executionError{http.StatusNotFound, "action_not_found", "action was not found"}
	}
	if token.ID != "" && !policy.AllowsAction(token, action.ActionKey) {
		return actionExecution{}, executionError{http.StatusForbidden, "policy_denied", "runtime policy denied this action"}
	}
	if !action.Executable || action.RelativePath == "" {
		return actionExecution{}, executionError{http.StatusNotImplemented, "action_not_executable", "action has no executable HTTP definition"}
	}
	if request.Input == nil {
		request.Input = map[string]any{}
	}
	catalogAction := definitionAction(action)
	if err := a.Catalog.ValidateInput(catalogAction, request.Input); err != nil {
		return actionExecution{}, executionError{http.StatusBadRequest, "invalid_input", err.Error()}
	}
	integrationID := request.IntegrationID
	if integrationID == "" {
		integrationID = action.IntegrationID
	}
	integration, err := a.Store.IntegrationForSystem(ctx, action.SystemKey, integrationID)
	if err != nil {
		return actionExecution{}, executionError{http.StatusNotFound, "integration_not_found", "a ready integration was not found"}
	}
	connectionKey := request.ConnectionKey
	if connectionKey == "" {
		connectionKey = request.ConnectionName
	}
	connection, err := a.Store.ConnectionForIntegration(ctx, integration.ID, connectionKey)
	if err != nil {
		return actionExecution{}, executionError{http.StatusNotFound, "connection_not_found", "an active connection was not found"}
	}
	if token.ID != "" && !policy.AllowsConnection(token, connection) {
		return actionExecution{}, executionError{http.StatusForbidden, "policy_denied", "runtime policy denied this connection"}
	}
	resolved, err := a.Auth.ResolveConnection(ctx, connection)
	if err != nil {
		return actionExecution{}, executionError{http.StatusBadGateway, "authentication_failed", err.Error()}
	}
	provider := model.Provider{Service: action.SystemKey, DisplayName: action.SystemKey, BaseURL: integration.BaseURL}

	fingerprintBytes, _ := json.Marshal(map[string]any{"action": action.ActionKey, "connection": connection.ID, "input": request.Input})
	fingerprint := sha256.Sum256(fingerprintBytes)
	var idempotency model.IdempotencyRecord
	if token.ID != "" && idempotencyKey != "" {
		if len(idempotencyKey) > 256 {
			return actionExecution{}, executionError{http.StatusBadRequest, "invalid_idempotency_key", "Idempotency-Key must not exceed 256 characters"}
		}
		idempotency = model.IdempotencyRecord{ID: newID("idempotency"), RuntimeTokenID: token.ID, Scope: "action:" + action.ActionKey, Key: idempotencyKey, Fingerprint: hex.EncodeToString(fingerprint[:]), ExpiresAt: now().Add(24 * time.Hour)}
		current, claimed, claimErr := a.Store.ClaimIdempotency(ctx, idempotency)
		if claimErr != nil {
			return actionExecution{}, claimErr
		}
		if !claimed {
			if current.Fingerprint != idempotency.Fingerprint {
				return actionExecution{}, executionError{http.StatusConflict, "idempotency_conflict", "Idempotency-Key was used with a different request"}
			}
			if current.Status != "completed" {
				return actionExecution{}, executionError{http.StatusConflict, "idempotency_in_progress", "a request with this Idempotency-Key is still running"}
			}
			return actionExecution{Status: current.HTTPStatus, Body: current.Response}, nil
		}
	}

	dispatched := false
	defer func() {
		if idempotency.ID != "" && !dispatched {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := a.Store.ReleaseIdempotency(cleanup, idempotency.ID); err != nil {
				a.Logger.Error("release idempotency claim", "error", err)
			}
		}
	}()
	startedAt := now()
	expiresAt, err := a.Store.OperationExpiresAt(ctx, startedAt)
	if err != nil {
		return actionExecution{}, err
	}
	run := model.OperationRun{ID: newID("operation"), RequestID: reqID, Kind: "action", Name: action.Name, Status: "running", SystemID: action.SystemID, IntegrationID: integration.ID, ConnectionID: connection.ID, ActionID: action.ID, RuntimeTokenID: token.ID, Source: source, Input: auditJSON(request.Input), StartedAt: startedAt, ExpiresAt: expiresAt}
	if err := a.Store.CreateOperation(ctx, run); err != nil {
		return actionExecution{}, err
	}
	_ = a.Store.AddOperationEvent(ctx, run.ID, "info", "开始调用上游系统", nil)

	if idempotency.ID != "" {
		if err := a.Store.StartIdempotency(ctx, idempotency.ID, run.ID); err != nil {
			return actionExecution{}, err
		}
	}
	dispatched = true
	providerResult, callErr := a.Executor.Action(ctx, provider, catalogAction, request.Input, nil, executor.RequestAuth{
		Headers: resolved.Headers, Query: resolved.Query, Cookies: resolved.Cookies,
	})

	finishCtx, cancelFinish := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFinish()
	if callErr != nil {
		body := runtimeBytes(nil, map[string]any{"operationId": run.ID}, "provider_request_failed", callErr.Error())
		if err := a.Store.FinishRuntime(finishCtx, run.ID, "unknown", http.StatusBadGateway, nil, "provider_request_failed", callErr.Error(), idempotency, body); err != nil {
			a.Logger.Error("persist unknown runtime result", "operation_id", run.ID, "error", err)
		}
		return actionExecution{Status: http.StatusBadGateway, Body: body, Operation: run}, nil
	}
	status := providerResult.Status
	if status < 200 || status > 599 {
		status = http.StatusBadGateway
	}
	runStatus, code, message := "success", "", ""
	if providerResult.Status < 200 || providerResult.Status >= 300 {
		runStatus, code, message = "failed", "provider_error", "provider returned an error"
	}
	data := providerData(providerResult.Body)
	meta := map[string]any{"operationId": run.ID, "providerStatus": providerResult.Status}
	body := runtimeBytes(data, meta, code, message)
	if err := a.Store.FinishRuntime(finishCtx, run.ID, runStatus, status, auditJSON(data), code, message, idempotency, body); err != nil {
		return actionExecution{}, fmt.Errorf("provider completed but result persistence failed (operation %s): %w", run.ID, err)
	}
	_ = a.Store.AddOperationEvent(ctx, run.ID, "info", "上游系统调用完成", auditJSON(meta))
	a.Store.MarkConnectionUsed(ctx, connection.ID)
	return actionExecution{Status: status, Body: body, Operation: run}, nil
}

func allowedActions(items []model.ActionDefinition, token model.RuntimeToken) []model.ActionDefinition {
	result := make([]model.ActionDefinition, 0, len(items))
	for _, item := range items {
		if item.Status == "active" && policy.AllowsAction(token, item.ActionKey) {
			result = append(result, item)
		}
	}
	return result
}

func definitionAction(item model.ActionDefinition) model.Action {
	input, output := map[string]any{}, map[string]any{}
	_ = jsonutil.Unmarshal(item.InputSchema, &input)
	_ = jsonutil.Unmarshal(item.OutputSchema, &output)
	return model.Action{ID: item.ActionKey, Service: item.SystemKey, Name: item.Name, Description: item.Description, RequiredScopes: item.RequiredScopes, InputSchema: input, OutputSchema: output, Runtime: &model.HTTPActionRuntime{Method: item.HTTPMethod, Path: item.RelativePath}, Executable: item.Executable}
}

type executionError struct {
	status  int
	code    string
	message string
}

func (e executionError) Error() string { return e.message }

func executionErrorStatus(err error) (int, string) {
	var typed executionError
	if errors.As(err, &typed) {
		return typed.status, typed.code
	}
	return http.StatusInternalServerError, "internal_error"
}

func (a *api) writeExecutionError(w http.ResponseWriter, err error) {
	status, code := executionErrorStatus(err)
	if status == http.StatusInternalServerError {
		a.Logger.Error("execute action", "error", err)
	}
	writeRuntimeError(w, status, code, err.Error(), nil)
}

func providerData(data []byte) any {
	if len(data) == 0 {
		return nil
	}
	var value any
	if jsonutil.Unmarshal(data, &value) == nil {
		return value
	}
	return string(data)
}

func auditJSON(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var normalized any
	if jsonutil.Unmarshal(data, &normalized) == nil {
		data, err = json.Marshal(redactAuditValue(normalized))
		if err != nil {
			return nil
		}
	}
	if len(data) > 64<<10 {
		return []byte(`{"truncated":true}`)
	}
	return data
}

func redactAuditValue(value any) any {
	switch current := value.(type) {
	case map[string]any:
		redacted := make(map[string]any, len(current))
		for key, item := range current {
			if sensitiveAuditKey(key) {
				redacted[key] = "[REDACTED]"
				continue
			}
			redacted[key] = redactAuditValue(item)
		}
		return redacted
	case []any:
		redacted := make([]any, len(current))
		for index, item := range current {
			redacted[index] = redactAuditValue(item)
		}
		return redacted
	default:
		return current
	}
}

func sensitiveAuditKey(key string) bool {
	normalized := strings.Map(func(char rune) rune {
		if char >= 'A' && char <= 'Z' {
			return char + ('a' - 'A')
		}
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			return char
		}
		return -1
	}, key)
	if normalized == "authorization" || normalized == "proxyauthorization" ||
		normalized == "cookie" || normalized == "setcookie" ||
		normalized == "apikey" || normalized == "accesskey" || normalized == "privatekey" {
		return true
	}
	return strings.HasSuffix(normalized, "password") ||
		strings.HasSuffix(normalized, "passwd") ||
		strings.HasSuffix(normalized, "secret") ||
		strings.HasSuffix(normalized, "token")
}

func runtimeBytes(data any, meta map[string]any, code, message string) []byte {
	if meta == nil {
		meta = map[string]any{}
	}
	var value any = runtimeSuccess{Success: true, Data: data, Meta: meta}
	if code != "" {
		value = runtimeFailure{Success: false, Message: message, Data: data, ErrorCode: code, Meta: meta}
	}
	encoded, _ := json.Marshal(value)
	return append(encoded, '\n')
}

func writeBytes(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
