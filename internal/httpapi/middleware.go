package httpapi

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"apihub-go/internal/model"
	"github.com/go-chi/chi/v5/middleware"
)

type contextKey string

const runtimeTokenKey contextKey = "runtime-token"
const adminIdentityKey contextKey = "admin-identity"

type adminIdentity struct {
	SessionID   string `json:"-"`
	UserID      string `json:"userId"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
}

func (a *api) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(adminSessionCookie)
		if err != nil || cookie.Value == "" {
			clearAdminSessionCookie(w, r)
			writeAdminError(w, http.StatusUnauthorized, "unauthorized", "A signed-in account is required")
			return
		}
		session, err := a.Store.AdminSessionByTokenHash(r.Context(), tokenHash(cookie.Value))
		if err != nil {
			clearAdminSessionCookie(w, r)
			writeAdminError(w, http.StatusUnauthorized, "unauthorized", "The login session is invalid or expired")
			return
		}
		if !allowsAdmin(session.Role, r.Method, r.URL.Path) {
			writeAdminError(w, http.StatusForbidden, "forbidden", "Your role does not permit this operation")
			return
		}
		allowed, _, err := a.Cache.Allow(r.Context(), "admin:"+session.UserID+":"+remoteIP(r), 300, time.Minute)
		// The control plane remains available during Redis outages; OAuth state,
		// token refresh locks and runtime traffic still fail closed in their paths.
		if err == nil && !allowed {
			writeAdminError(w, http.StatusTooManyRequests, "rate_limited", "Too many requests")
			return
		}
		identity := adminIdentity{SessionID: session.ID, UserID: session.UserID, Email: session.Email, DisplayName: session.DisplayName, Role: session.Role}
		next.ServeHTTP(w, r.WithContext(withAdminIdentity(r.Context(), identity)))
	})
}

func allowsAdmin(role, method, route string) bool {
	if role != "owner" && role != "admin" && role != "developer" && role != "viewer" {
		return false
	}
	if method == http.MethodGet || method == http.MethodHead || route == "/api/auth/logout" {
		return true
	}
	if role == "owner" || role == "admin" {
		return true
	}
	if role == "viewer" {
		return false
	}
	for _, prefix := range []string{"/api/system-groups", "/api/systems", "/api/auth-templates", "/api/auth-instances", "/api/integrations", "/api/connections", "/api/actions", "/api/sync-tasks", "/api/webhook-endpoints", "/api/webhook-sources", "/api/webhook-deliveries", "/api/oauth/start"} {
		if route == prefix || strings.HasPrefix(route, prefix+"/") {
			return true
		}
	}
	return false
}

func (a *api) requireRuntime(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := bearerToken(r)
		if value == "" {
			writeRuntimeError(w, http.StatusUnauthorized, "unauthorized", "A runtime bearer token is required", nil)
			return
		}
		token, err := a.Store.RuntimeTokenByHash(r.Context(), tokenHash(value))
		if err != nil {
			writeRuntimeError(w, http.StatusUnauthorized, "unauthorized", "Runtime token is invalid or revoked", nil)
			return
		}
		allowed, _, err := a.Cache.Allow(r.Context(), "runtime:"+token.ID, 600, time.Minute)
		if err != nil {
			writeRuntimeError(w, http.StatusServiceUnavailable, "rate_limit_unavailable", "Rate limiter is unavailable", nil)
			return
		}
		if !allowed {
			writeRuntimeError(w, http.StatusTooManyRequests, "rate_limited", "Too many requests", nil)
			return
		}
		ctx := context.WithValue(r.Context(), runtimeTokenKey, token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func runtimeToken(r *http.Request) model.RuntimeToken {
	token, _ := r.Context().Value(runtimeTokenKey).(model.RuntimeToken)
	return token
}

func withAdminIdentity(ctx context.Context, identity adminIdentity) context.Context {
	return context.WithValue(ctx, adminIdentityKey, identity)
}

func currentAdmin(r *http.Request) adminIdentity {
	identity, _ := r.Context().Value(adminIdentityKey).(adminIdentity)
	return identity
}

func secureEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func bearerToken(r *http.Request) string {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func (a *api) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrapped := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		started := time.Now()
		next.ServeHTTP(wrapped, r)
		a.Logger.Log(r.Context(), slog.LevelInfo, "HTTP request",
			"request_id", middleware.GetReqID(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.Status(),
			"bytes", wrapped.BytesWritten(),
			"duration_ms", time.Since(started).Milliseconds(),
		)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
