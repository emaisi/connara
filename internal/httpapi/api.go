package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"apihub-go/internal/authn"
	"apihub-go/internal/buildinfo"
	"apihub-go/internal/catalog"
	"apihub-go/internal/executor"
	"apihub-go/internal/rediscache"
	"apihub-go/internal/secret"
	"apihub-go/internal/store"
	"apihub-go/internal/webui"
	"apihub-go/internal/workflow"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const maxJSONBody = 2 << 20

type Dependencies struct {
	Store         *store.Store
	Catalog       *catalog.Catalog
	Codec         *secret.Codec
	Auth          *authn.Service
	Executor      *executor.Executor
	Cache         *rediscache.Cache
	Logger        *slog.Logger
	PublicBaseURL string
}

type api struct {
	Dependencies
	previewMu     sync.Mutex
	previewActive bool
	previewNext   time.Time
}

type runtimeSuccess struct {
	Success bool           `json:"success"`
	Data    any            `json:"data"`
	Meta    map[string]any `json:"meta"`
}

type runtimeFailure struct {
	Success   bool           `json:"success"`
	Message   string         `json:"message"`
	Data      any            `json:"data"`
	ErrorCode string         `json:"errorCode"`
	Meta      map[string]any `json:"meta"`
}

func New(deps Dependencies) http.Handler {
	a := &api{Dependencies: deps}
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Request-ID", middleware.GetReqID(r.Context()))
			next.ServeHTTP(w, r)
		})
	})
	r.Use(middleware.Recoverer)
	r.Use(a.accessLog)
	r.Use(securityHeaders)

	r.Get("/health", a.ready)
	r.Get("/health/live", a.live)
	r.Get("/health/ready", a.ready)
	r.Get("/oauth/callback/{systemKey}", a.oauthCallback)
	r.Post("/webhooks/inbound/{sourceKey}", a.inboundWebhook)

	r.Route("/api", func(r chi.Router) {
		r.Post("/auth/login", a.audited(a.login))
		r.Post("/auth/accept-invitation", a.acceptInvitation)
		r.Group(func(r chi.Router) {
			r.Use(a.requireAdmin)
			r.Get("/auth/session", a.currentSession)
			r.Post("/auth/logout", a.audited(a.logout))
			r.Get("/meta", a.meta)

			r.Get("/lookups", a.catalogLookups)
			r.Get("/system-groups", a.listSystemGroups)
			r.Post("/system-groups", a.audited(a.saveSystemGroup))
			r.Patch("/system-groups/{id}", a.audited(a.saveSystemGroup))
			r.Delete("/system-groups/{id}", a.audited(a.deleteSystemGroup))
			r.Get("/systems", a.listSystems)
			r.Get("/systems/{id}", a.getSystem)
			r.Post("/systems", a.audited(a.createSystem))
			r.Patch("/systems/{id}", a.audited(a.updateSystem))
			r.Put("/systems/{id}/auth-templates", a.audited(a.setSystemAuthTemplates))

			r.Get("/auth-templates", a.listAuthTemplates)
			r.Post("/auth-templates", a.audited(a.saveAuthTemplate))
			r.Patch("/auth-templates/{id}", a.audited(a.saveAuthTemplate))
			r.Post("/auth-templates/{id}/status", a.audited(a.setAuthTemplateStatus))
			r.Delete("/auth-templates/{id}", a.audited(a.deleteAuthTemplate))
			r.Get("/auth-instances", a.listAuthInstances)
			r.Post("/auth-instances", a.audited(a.saveAuthInstance))
			r.Post("/auth-instances/check", a.checkAuthInstance)
			r.Post("/auth-instances/{id}/check", a.checkAuthInstance)
			r.Patch("/auth-instances/{id}", a.audited(a.saveAuthInstance))
			r.Post("/auth-instances/{id}/test", a.testAuthInstance)

			r.Get("/integrations", a.listIntegrations)
			r.Post("/integrations", a.audited(a.saveIntegration))
			r.Patch("/integrations/{id}", a.audited(a.saveIntegration))
			r.Get("/connections", a.listConnections)
			r.Post("/connections", a.audited(a.saveConnection))
			r.Patch("/connections/{id}", a.audited(a.saveConnection))
			r.Post("/connections/{id}/verify", a.verifyConnection)
			r.Post("/connections/{id}/enabled", a.audited(a.setConnectionEnabled))
			r.Post("/oauth/start", a.oauthStart)

			r.Get("/actions", a.listActions)
			r.Get("/actions/{id}", a.getAction)
			r.Post("/actions", a.audited(a.saveAction))
			r.Patch("/actions/{id}", a.audited(a.saveAction))
			r.Post("/actions/{id}/test", a.testAction)
			r.Post("/actions/{id}/preview", a.previewAction)
			r.Get("/actions/{id}/execution-options", a.actionExecutionOptions)

			r.Get("/sync-tasks", a.listSyncTasks)
			r.Post("/sync-tasks", a.audited(a.saveSyncTask))
			r.Patch("/sync-tasks/{id}", a.audited(a.saveSyncTask))
			r.Delete("/sync-tasks/{id}", a.audited(a.deleteSyncTask))
			r.Post("/sync-tasks/{id}/deploy", a.audited(a.deploySyncTask))
			r.Post("/sync-tasks/{id}/pause", a.audited(a.pauseSyncTask))
			r.Post("/sync-tasks/{id}/run", a.audited(a.runSyncTask))
			r.Get("/sync-tasks/{id}/records", a.listSyncRecords)

			r.Get("/workflows/capabilities", a.workflowCapabilities)
			r.Post("/workflows/preview-step", a.previewWorkflowStep)
			r.Post("/workflows/preview-schedule", a.previewWorkflowSchedule)
			r.Get("/workflows", a.listWorkflows)
			r.Post("/workflows", a.audited(a.saveWorkflow))
			r.Get("/workflows/{id}", a.getWorkflow)
			r.Patch("/workflows/{id}", a.audited(a.saveWorkflow))
			r.Patch("/workflows/{id}/layout", a.audited(a.saveWorkflowLayout))
			r.Delete("/workflows/{id}", a.audited(a.deleteWorkflow))
			r.Post("/workflows/{id}/deploy", a.deployWorkflow)
			r.Post("/workflows/{id}/pause", a.audited(a.pauseWorkflow))
			r.Post("/workflows/{id}/run", a.audited(a.runWorkflowNow))
			r.Get("/workflow-runs/{id}/view", a.workflowRunView)
			r.Get("/workflow-runs/{id}/result", a.workflowRunResult)

			r.Get("/webhook-endpoints", a.listWebhookEndpoints)
			r.Post("/webhook-endpoints", a.audited(a.saveWebhookEndpoint))
			r.Patch("/webhook-endpoints/{id}", a.audited(a.saveWebhookEndpoint))
			r.Post("/webhook-endpoints/{id}/test", a.audited(a.testWebhookEndpoint))
			r.Get("/webhook-sources", a.listWebhookSources)
			r.Post("/webhook-sources", a.audited(a.saveWebhookSource))
			r.Patch("/webhook-sources/{id}", a.audited(a.saveWebhookSource))
			r.Get("/webhook-deliveries", a.listWebhookDeliveries)
			r.Get("/webhook-deliveries/{id}", a.getWebhookDelivery)
			r.Get("/webhook-deliveries/{id}/attempts", a.listWebhookDeliveryAttempts)
			r.Post("/webhook-deliveries/{id}/retry", a.audited(a.retryWebhookDelivery))

			r.Get("/runtime-tokens", a.listRuntimeTokens)
			r.Post("/runtime-tokens", a.audited(a.createRuntimeToken))
			r.Delete("/runtime-tokens/{id}", a.audited(a.revokeRuntimeToken))
			r.Get("/operations", a.listOperations)
			r.Get("/operations/{id}", a.getOperation)
			r.Get("/metrics", a.metrics)
			r.Get("/audit-logs", a.listAudit)
			r.Get("/team/members", a.listTeamMembers)
			r.Post("/team/members", a.audited(a.inviteTeamMember))
			r.Patch("/team/members/{id}", a.audited(a.updateTeamMember))
			r.Delete("/team/members/{id}", a.audited(a.removeTeamMember))
			r.Get("/settings", a.getSettings)
			r.Patch("/settings", a.audited(a.saveSettings))
		})
	})

	r.Route("/v1", func(r chi.Router) {
		r.Use(a.requireRuntime)
		r.Get("/health", a.runtimeHealth)
		r.Get("/providers", a.runtimeProviders)
		r.Get("/actions", a.runtimeActions)
		r.Get("/actions/search", a.runtimeActionSearch)
		r.Get("/actions/{actionKey}", a.runtimeAction)
		r.Post("/actions/{actionKey}", a.executeAction)
		r.Get("/workflows", a.runtimeWorkflows)
		r.Get("/workflows/{key}", a.runtimeWorkflow)
		r.Post("/workflows/{key}", a.triggerWorkflow)
		r.Get("/workflow-runs/{id}", a.runtimeWorkflowRun)
		r.HandleFunc("/proxy/*", func(w http.ResponseWriter, _ *http.Request) {
			writeRuntimeError(w, http.StatusGone, "proxy_removed", "Provider proxy is not part of this API", nil)
		})
	})

	web := webui.Handler()
	r.Get("/", web.ServeHTTP)
	r.Handle("/*", web)
	return r
}

func (a *api) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *api) ready(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.Ready(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "component": "postgresql"})
		return
	}
	if err := a.Cache.Ready(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "component": "redis"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *api) runtimeHealth(w http.ResponseWriter, _ *http.Request) {
	writeRuntime(w, http.StatusOK, map[string]any{"ok": true}, nil)
}

func (a *api) meta(w http.ResponseWriter, r *http.Request) {
	name := "APIHub"
	if settings, err := a.Store.Settings(r.Context()); err == nil && settings.PlatformName != "" {
		name = settings.PlatformName
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": name, "version": buildinfo.Version, "workspaceId": a.Store.WorkspaceID(),
		"providerCount": len(a.Catalog.Providers()), "supportedAuthFlows": authn.ExecutableFlows(),
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeAdminError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"code": code, "message": message})
}

func writeRuntime(w http.ResponseWriter, status int, data any, meta map[string]any) {
	if meta == nil {
		meta = map[string]any{}
	}
	writeJSON(w, status, runtimeSuccess{Success: true, Data: data, Meta: meta})
}

func writeRuntimeError(w http.ResponseWriter, status int, code, message string, meta map[string]any) {
	if meta == nil {
		meta = map[string]any{}
	}
	writeJSON(w, status, runtimeFailure{Success: false, Message: message, Data: nil, ErrorCode: code, Meta: meta})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any, allowEmpty bool) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		if allowEmpty && errors.Is(err, io.EOF) {
			return true
		}
		writeAdminError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON body: "+err.Error())
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeAdminError(w, http.StatusBadRequest, "invalid_json", "JSON body must contain exactly one value")
		return false
	}
	return true
}

func newToken() string {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	return "hub_rt_" + base64.RawURLEncoding.EncodeToString(value)
}

func newID(namespace string) string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
}

func parseLimit(r *http.Request, fallback, maximum int) int {
	value, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || value <= 0 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func validIdentifier(value string) bool {
	if value == "" || len(value) > 180 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.' {
			continue
		}
		return false
	}
	return true
}

func tokenHash(value string) string {
	digest := sha256Sum(value)
	return hex.EncodeToString(digest)
}

func sha256Sum(value string) []byte {
	result := sha256.Sum256([]byte(value))
	return result[:]
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func statusForStoreError(err error) (int, string) {
	switch {
	case errors.Is(err, workflow.ErrFeatureDisabled):
		return http.StatusConflict, "workflow_feature_disabled"
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, store.ErrConflict), store.IsUniqueViolation(err):
		return http.StatusConflict, "conflict"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}

func now() time.Time { return time.Now().UTC() }

func requestID(r *http.Request) string {
	if value := middleware.GetReqID(r.Context()); value != "" {
		return value
	}
	return newID("request")
}

func clean(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > maximum {
		return string([]rune(value)[:maximum])
	}
	return value
}

func writeConfigurationError(w http.ResponseWriter, field string, err error) {
	issue := &executor.FieldError{Field: field, Path: "$", Constraint: err.Error()}
	var typed *executor.FieldError
	if errors.As(err, &typed) {
		issue = typed
	}
	writeJSON(w, 400, map[string]any{"code": "invalid_configuration", "message": issue.Error(), "details": map[string]any{"fieldErrors": []*executor.FieldError{issue}}})
}
