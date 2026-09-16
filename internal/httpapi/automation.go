package httpapi

import (
	"apihub-go/internal/jsonutil"
	"apihub-go/internal/webhooksig"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"apihub-go/internal/background"
	"apihub-go/internal/model"
	"apihub-go/internal/store"
	"github.com/go-chi/chi/v5"
)

func (a *api) listSyncTasks(w http.ResponseWriter, r *http.Request) {
	items, err := a.Store.ListSyncTasks(r.Context())
	writeStoreResult(w, items, err, "list sync tasks")
}

func (a *api) saveSyncTask(w http.ResponseWriter, r *http.Request) {
	var request model.SyncTask
	if !decodeJSON(w, r, &request, false) {
		return
	}
	request.ID = chi.URLParam(r, "id")
	request.TaskKey = strings.ToLower(clean(request.TaskKey, 100))
	request.Name = clean(request.Name, 180)
	if !validIdentifier(request.TaskKey) || request.Name == "" || request.IntegrationID == "" || request.ConnectionID == "" || request.ActionID == "" {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "taskKey, name, integrationId, connectionId and actionId are required")
		return
	}
	if request.ScheduleType != "manual" && request.ScheduleType != "interval" {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "scheduleType must be manual or interval")
		return
	}
	if request.ScheduleType == "interval" && intervalDuration(request.CronExpression) == 0 {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "interval must be one of 每 15 分钟, 每 30 分钟, 每小时 or 每天")
		return
	}
	request.Status = defaultStatus(request.Status, "draft")
	if !validStatus(request.Status, "draft", "deployed", "paused", "disabled") {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "status must be draft, deployed, paused or disabled")
		return
	}
	request.RetryPolicy = defaultJSON(request.RetryPolicy, `{"maxAttempts":3}`)
	request.Input = defaultJSON(request.Input, `{}`)
	if _, err := background.ParseSyncConfig(request.SyncConfig); err != nil {
		writeAdminError(w, 400, "invalid_sync_config", err.Error())
		return
	}
	var input map[string]any
	if err := jsonutil.Unmarshal(request.Input, &input); err != nil || input == nil {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "sync input must be a JSON object")
		return
	}
	action, err := a.Store.Action(r.Context(), request.ActionID)
	if err != nil {
		a.writeStoreError(w, r, err, "get sync action")
		return
	}
	if err := a.Catalog.ValidateInput(definitionAction(action), input); err != nil {
		writeAdminError(w, http.StatusBadRequest, "invalid_sync_input", err.Error())
		return
	}
	item, err := a.Store.SaveSyncTask(r.Context(), request)
	if err != nil {
		a.writeStoreError(w, r, err, "save sync task")
		return
	}
	a.audit(r, "sync_task.saved", "sync_task", item.ID, item.Name, nil, item)
	writeJSON(w, statusForSave(r), item)
}

func (a *api) deleteSyncTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.DeleteSyncTask(r.Context(), id); err != nil {
		a.writeStoreError(w, r, err, "delete sync task")
		return
	}
	a.audit(r, "sync_task.deleted", "sync_task", id, id, nil, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) deploySyncTask(w http.ResponseWriter, r *http.Request) {
	task, err := a.Store.SyncTask(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.writeStoreError(w, r, err, "get sync task")
		return
	}
	if err := a.Store.ValidateSyncTaskReady(r.Context(), task); err != nil {
		a.writeStoreError(w, r, err, "validate sync task")
		return
	}
	next := nextTaskRun(task, now())
	item, err := a.Store.SetSyncTaskStatus(r.Context(), task.ID, "deployed", next)
	if err != nil {
		a.writeStoreError(w, r, err, "deploy sync task")
		return
	}
	a.audit(r, "sync_task.deployed", "sync_task", item.ID, item.Name, task, item)
	writeJSON(w, http.StatusOK, item)
}

func (a *api) runSyncTask(w http.ResponseWriter, r *http.Request) {
	task, err := a.Store.SyncTask(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.writeStoreError(w, r, err, "get sync task")
		return
	}
	if task.Status != "deployed" {
		writeAdminError(w, http.StatusConflict, "sync_task_not_deployed", "sync task must be deployed before it can run")
		return
	}
	if err := a.Store.ValidateSyncTaskReady(r.Context(), task); err != nil {
		a.writeStoreError(w, r, err, "validate sync task")
		return
	}
	operation, err := a.Store.EnqueueSync(r.Context(), task, "manual", requestID(r))
	if err != nil {
		a.writeStoreError(w, r, err, "enqueue sync task")
		return
	}
	a.audit(r, "sync_task.run_queued", "sync_task", task.ID, task.Name, nil, operation)
	writeJSON(w, http.StatusAccepted, operation)
}

func (a *api) listSyncRecords(w http.ResponseWriter, r *http.Request) {
	options, ok := listOptions(w, r)
	if !ok {
		return
	}
	items, err := a.Store.ListSyncRecords(r.Context(), chi.URLParam(r, "id"), options.Limit, options)
	if len(items) == options.Limit {
		nextCursor(w, len(items), options.Limit, items[len(items)-1].FirstSeenAt, items[len(items)-1].ID)
	}
	writeStoreResult(w, items, err, "list sync records")
}

func (a *api) listWebhookEndpoints(w http.ResponseWriter, r *http.Request) {
	items, err := a.Store.ListWebhookEndpoints(r.Context())
	writeStoreResult(w, items, err, "list webhook endpoints")
}

func (a *api) saveWebhookEndpoint(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name             string   `json:"name"`
		PrimaryURL       string   `json:"primaryUrl"`
		FallbackURL      string   `json:"fallbackUrl"`
		Secret           string   `json:"secret"`
		Status           string   `json:"status"`
		SubscribedEvents []string `json:"subscribedEvents"`
	}
	if !decodeJSON(w, r, &request, false) {
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		id = store.StableID("webhook-endpoint", a.Store.WorkspaceID()+":"+strings.ToLower(strings.TrimSpace(request.Name)))
	}
	if request.Name == "" || !validHTTPSURL(request.PrimaryURL) || (request.FallbackURL != "" && !validHTTPSURL(request.FallbackURL)) {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "name and an HTTPS primaryUrl are required")
		return
	}
	request.Status = defaultStatus(request.Status, "active")
	if !validStatus(request.Status, "active", "paused", "disabled") {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "status must be active, paused or disabled")
		return
	}
	var blob []byte
	var err error
	if request.Secret != "" {
		blob, err = a.sealNamedSecret("webhook-endpoint", id, request.Secret)
	} else if chi.URLParam(r, "id") != "" {
		blob, err = a.Store.WebhookEndpointSecretBlob(r.Context(), id)
	} else {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "secret is required when creating a webhook endpoint")
		return
	}
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "secret_encryption_failed", "Could not encrypt webhook secret")
		return
	}
	item, err := a.Store.SaveWebhookEndpoint(r.Context(), model.WebhookEndpoint{ID: id, Name: clean(request.Name, 180), PrimaryURL: request.PrimaryURL, FallbackURL: request.FallbackURL, SecretBlob: blob, KeyVersion: a.Codec.Version(), Status: request.Status, SubscribedEvents: cleanPatterns(request.SubscribedEvents)})
	if err != nil {
		a.writeStoreError(w, r, err, "save webhook endpoint")
		return
	}
	a.audit(r, "webhook_endpoint.saved", "webhook_endpoint", item.ID, item.Name, nil, item)
	writeJSON(w, statusForSave(r), item)
}

func (a *api) testWebhookEndpoint(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	event := model.OutboxEvent{ID: newID("event"), EventType: "webhook.test", AggregateType: "webhook_endpoint", AggregateID: id, DedupeKey: "webhook-test:" + id + ":" + requestID(r), Payload: store.MarshalJSON(map[string]any{"test": true, "createdAt": now()})}
	deliveryID, err := a.Store.QueueWebhookTest(r.Context(), id, event)
	if err != nil {
		a.writeStoreError(w, r, err, "queue webhook test")
		return
	}
	a.audit(r, "webhook_endpoint.test_queued", "webhook_endpoint", id, id, nil, map[string]any{"deliveryId": deliveryID})
	writeJSON(w, http.StatusAccepted, map[string]any{"queued": true, "eventId": event.ID, "deliveryId": deliveryID})
}

func (a *api) listWebhookSources(w http.ResponseWriter, r *http.Request) {
	items, err := a.Store.ListWebhookSources(r.Context())
	writeStoreResult(w, items, err, "list webhook sources")
}

func (a *api) saveWebhookSource(w http.ResponseWriter, r *http.Request) {
	var request struct {
		SourceKey        string          `json:"sourceKey"`
		Name             string          `json:"name"`
		IntegrationID    string          `json:"integrationId"`
		Status           string          `json:"status"`
		SignatureType    string          `json:"signatureType"`
		Secret           string          `json:"secret"`
		SubscribedEvents []string        `json:"subscribedEvents"`
		Settings         json.RawMessage `json:"settings"`
	}
	if !decodeJSON(w, r, &request, false) {
		return
	}
	request.SourceKey = strings.ToLower(clean(request.SourceKey, 100))
	if !validIdentifier(request.SourceKey) || request.Name == "" || request.IntegrationID == "" || (request.SignatureType != "none" && request.SignatureType != "hmac-sha256") {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "sourceKey, name, integrationId and a supported signatureType are required")
		return
	}
	request.Status = defaultStatus(request.Status, "active")
	if !validStatus(request.Status, "active", "disabled") {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "status must be active or disabled")
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		id = store.StableID("webhook-source", a.Store.WorkspaceID()+":"+request.SourceKey)
	}
	var blob []byte
	var err error
	if request.SignatureType == "hmac-sha256" {
		switch {
		case request.Secret != "":
			blob, err = a.sealNamedSecret("webhook-source", id, request.Secret)
		case chi.URLParam(r, "id") != "":
			blob, err = a.Store.WebhookSourceSecretBlob(r.Context(), id)
		default:
			writeAdminError(w, http.StatusBadRequest, "invalid_input", "secret is required for an HMAC webhook source")
			return
		}
	}
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "secret_encryption_failed", "Could not preserve or encrypt webhook source secret")
		return
	}
	item, err := a.Store.SaveWebhookSource(r.Context(), model.WebhookSource{ID: id, SourceKey: request.SourceKey, Name: clean(request.Name, 180), IntegrationID: request.IntegrationID, Status: request.Status, SignatureType: request.SignatureType, SecretBlob: blob, KeyVersion: a.Codec.Version(), SubscribedEvents: cleanPatterns(request.SubscribedEvents), Settings: defaultJSON(request.Settings, `{}`)})
	if err != nil {
		a.writeStoreError(w, r, err, "save webhook source")
		return
	}
	a.audit(r, "webhook_source.saved", "webhook_source", item.ID, item.Name, nil, item)
	writeJSON(w, statusForSave(r), item)
}

func (a *api) listWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	o, ok := listOptions(w, r)
	if !ok {
		return
	}
	items, err := a.Store.ListWebhookDeliveries(r.Context(), o.Limit, o)
	if len(items) > 0 {
		last := items[len(items)-1]
		nextCursor(w, len(items), o.Limit, last.CreatedAt, last.ID)
	}
	writeStoreResult(w, items, err, "list webhook deliveries")
}

func (a *api) listWebhookDeliveryAttempts(w http.ResponseWriter, r *http.Request) {
	items, err := a.Store.ListWebhookDeliveryAttempts(r.Context(), chi.URLParam(r, "id"))
	writeStoreResult(w, items, err, "list webhook delivery attempts")
}

func (a *api) retryWebhookDelivery(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.RetryWebhookDelivery(r.Context(), id); err != nil {
		a.writeStoreError(w, r, err, "retry webhook delivery")
		return
	}
	a.audit(r, "webhook_delivery.retried", "webhook_delivery", id, id, nil, nil)
	writeJSON(w, http.StatusAccepted, map[string]any{"queued": true})
}

func (a *api) inboundWebhook(w http.ResponseWriter, r *http.Request) {
	source, err := a.Store.WebhookSourceByKey(r.Context(), chi.URLParam(r, "sourceKey"))
	if err != nil || source.Status != "active" {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxJSONBody+1))
	if err != nil || len(body) > maxJSONBody || !json.Valid(body) {
		writeAdminError(w, http.StatusBadRequest, "invalid_payload", "Webhook body must be valid JSON within 2 MiB")
		return
	}
	if source.SignatureType == "hmac-sha256" {
		plain, decryptErr := a.Codec.Decrypt(source.SecretBlob, []byte(a.Store.WorkspaceID()+":webhook-source:"+source.ID))
		if decryptErr != nil || !webhooksig.Verify(plain, r.Header.Get("X-APIHub-Timestamp"), r.Header.Get("X-APIHub-Event-ID"), r.Header.Get("X-APIHub-Event"), body, r.Header.Get("X-APIHub-Signature"), time.Now()) {
			writeAdminError(w, http.StatusUnauthorized, "invalid_signature", "Webhook signature is invalid")
			return
		}
	}
	eventType := r.Header.Get("X-APIHub-Event")
	if eventType == "" {
		eventType = "webhook.received"
	}
	if len(source.SubscribedEvents) > 0 && !matchesAny(source.SubscribedEvents, eventType) {
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": false, "reason": "event_not_subscribed"})
		return
	}
	providerID := r.Header.Get("X-APIHub-Event-ID")
	if providerID == "" {
		sum := sha256.Sum256(body)
		providerID = hex.EncodeToString(sum[:])
	}
	var payloadValue any
	_ = jsonutil.Unmarshal(body, &payloadValue)
	created, operationID, err := a.Store.RecordInboundWebhook(
		r.Context(), source, providerID, eventType, body, auditJSON(payloadValue), requestID(r),
	)
	if err != nil {
		a.Logger.Error("record inbound webhook", "error", err)
		writeAdminError(w, http.StatusInternalServerError, "internal_error", "Could not record webhook")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "duplicate": !created, "operationId": operationID})
}

func (a *api) sealNamedSecret(kind, id, value string) ([]byte, error) {
	if value == "" {
		return nil, nil
	}
	return a.Codec.Encrypt([]byte(value), []byte(a.Store.WorkspaceID()+":"+kind+":"+id))
}

func validHTTPSURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.Fragment == ""
}

func defaultJSON(value json.RawMessage, fallback string) json.RawMessage {
	if len(value) == 0 || !json.Valid(value) {
		return json.RawMessage(fallback)
	}
	return value
}

func cleanPatterns(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = clean(value, 160)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func matchesAny(patterns []string, value string) bool {
	for _, pattern := range patterns {
		if pattern == "*" || pattern == value || (strings.HasSuffix(pattern, ".*") && strings.HasPrefix(value, strings.TrimSuffix(pattern, "*"))) {
			return true
		}
	}
	return false
}

func nextTaskRun(task model.SyncTask, from time.Time) *time.Time {
	if task.ScheduleType == "manual" {
		return nil
	}
	duration := intervalDuration(task.CronExpression)
	if duration == 0 {
		// Cron syntax remains in the schema for a later scheduler extension. The
		// first version schedules only the explicit intervals exposed by the UI.
		duration = 30 * time.Minute
	}
	next := from.Add(duration)
	return &next
}

func intervalDuration(value string) time.Duration {
	switch strings.TrimSpace(value) {
	case "每 15 分钟":
		return 15 * time.Minute
	case "每 30 分钟":
		return 30 * time.Minute
	case "每小时":
		return time.Hour
	case "每天":
		return 24 * time.Hour
	default:
		return 0
	}
}

func (a *api) pauseSyncTask(w http.ResponseWriter, r *http.Request) {
	task, err := a.Store.SyncTask(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.writeStoreError(w, r, err, "get sync task")
		return
	}
	item, err := a.Store.SetSyncTaskStatus(r.Context(), task.ID, "paused", nil)
	if err != nil {
		a.writeStoreError(w, r, err, "pause sync task")
		return
	}
	a.audit(r, "sync_task.paused", "sync_task", item.ID, item.Name, task, item)
	writeJSON(w, 200, item)
}

func (a *api) getWebhookDelivery(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	items, err := a.Store.ListWebhookDeliveries(r.Context(), 1, store.ListOptions{Limit: 1, Query: id})
	if err != nil {
		a.writeStoreError(w, r, err, "load webhook delivery")
		return
	}
	if len(items) != 1 || items[0].ID != id {
		writeAdminError(w, 404, "not_found", "投递记录不存在")
		return
	}
	writeJSON(w, 200, items[0])
}
