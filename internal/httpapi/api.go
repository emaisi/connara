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
	"time"

	"apihub-go/internal/authn"
	"apihub-go/internal/catalog"
	"apihub-go/internal/executor"
	"apihub-go/internal/rediscache"
	"apihub-go/internal/secret"
	"apihub-go/internal/store"
	"apihub-go/internal/webui"
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

type api struct{ Dependencies }

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
		r.Post("/auth/login", a.login)
		r.Post("/auth/accept-invitation", a.acceptInvitation)
		r.Group(func(r chi.Router) {
			r.Use(a.requireAdmin)
			r.Get("/auth/session", a.currentSession)
			r.Post("/auth/logout", a.logout)
			r.Get("/meta", a.meta)

			r.Get("/lookups", a.catalogLookups)
			r.Get("/system-groups", a.listSystemGroups)
			r.Post("/system-groups", a.saveSystemGroup)
			r.Patch("/system-groups/{id}", a.saveSystemGroup)
			r.Delete("/system-groups/{id}", a.deleteSystemGroup)
			r.Get("/systems", a.listSystems)
			r.Get("/systems/{id}", a.getSystem)
			r.Post("/systems", a.createSystem)
			r.Patch("/systems/{id}", a.updateSystem)
			r.Put("/systems/{id}/auth-templates", a.setSystemAuthTemplates)

			r.Get("/auth-templates", a.listAuthTemplates)
			r.Post("/auth-templates", a.saveAuthTemplate)
			r.Patch("/auth-templates/{id}", a.saveAuthTemplate)
			r.Post("/auth-templates/{id}/status", a.setAuthTemplateStatus)
			r.Delete("/auth-templates/{id}", a.deleteAuthTemplate)
			r.Get("/auth-instances", a.listAuthInstances)
			r.Post("/auth-instances", a.saveAuthInstance)
			r.Patch("/auth-instances/{id}", a.saveAuthInstance)
			r.Post("/auth-instances/{id}/test", a.testAuthInstance)

			r.Get("/integrations", a.listIntegrations)
			r.Post("/integrations", a.saveIntegration)
			r.Patch("/integrations/{id}", a.saveIntegration)
			r.Get("/connections", a.listConnections)
			r.Post("/connections", a.saveConnection)
			r.Patch("/connections/{id}", a.saveConnection)
			r.Post("/connections/{id}/verify", a.verifyConnection)
			r.Post("/oauth/start", a.oauthStart)

			r.Get("/actions", a.listActions)
			r.Get("/actions/{id}", a.getAction)
			r.Post("/actions", a.saveAction)
			r.Patch("/actions/{id}", a.saveAction)
			r.Post("/actions/{id}/test", a.testAction)

			r.Get("/sync-tasks", a.listSyncTasks)
			r.Post("/sync-tasks", a.saveSyncTask)
			r.Patch("/sync-tasks/{id}", a.saveSyncTask)
			r.Delete("/sync-tasks/{id}", a.deleteSyncTask)
			r.Post("/sync-tasks/{id}/deploy", a.deploySyncTask)
			r.Post("/sync-tasks/{id}/pause", a.pauseSyncTask)
			r.Post("/sync-tasks/{id}/run", a.runSyncTask)
			r.Get("/sync-tasks/{id}/records", a.listSyncRecords)

			r.Get("/webhook-endpoints", a.listWebhookEndpoints)
			r.Post("/webhook-endpoints", a.saveWebhookEndpoint)
			r.Patch("/webhook-endpoints/{id}", a.saveWebhookEndpoint)
			r.Post("/webhook-endpoints/{id}/test", a.testWebhookEndpoint)
			r.Get("/webhook-sources", a.listWebhookSources)
			r.Post("/webhook-sources", a.saveWebhookSource)
			r.Patch("/webhook-sources/{id}", a.saveWebhookSource)
			r.Get("/webhook-deliveries", a.listWebhookDeliveries)
			r.Get("/webhook-deliveries/{id}", a.getWebhookDelivery)
			r.Get("/webhook-deliveries/{id}/attempts", a.listWebhookDeliveryAttempts)
			r.Post("/webhook-deliveries/{id}/retry", a.retryWebhookDelivery)

			r.Get("/runtime-tokens", a.listRuntimeTokens)
			r.Post("/runtime-tokens", a.createRuntimeToken)
			r.Delete("/runtime-tokens/{id}", a.revokeRuntimeToken)
			r.Get("/operations", a.listOperations)
			r.Get("/operations/{id}", a.getOperation)
			r.Get("/metrics", a.metrics)
			r.Get("/audit-logs", a.listAudit)
			r.Get("/team/members", a.listTeamMembers)
			r.Post("/team/members", a.inviteTeamMember)
			r.Patch("/team/members/{id}", a.updateTeamMember)
			r.Delete("/team/members/{id}", a.removeTeamMember)
			r.Get("/settings", a.getSettings)
			r.Patch("/settings", a.saveSettings)
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
		"name": name, "version": "0.2.0", "workspaceId": a.Store.WorkspaceID(),
		"providerCount": len(a.Catalog.Providers()),
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
	if len(value) > maximum {
		return value[:maximum]
	}
	return value
}
