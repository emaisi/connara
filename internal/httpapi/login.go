package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"apihub-go/internal/store"
	"golang.org/x/crypto/bcrypt"
)

const (
	adminSessionCookie = "apihub_session"
	adminSessionTTL    = 12 * time.Hour
)

// This valid hash is used when an account does not exist so login failures take
// approximately the same amount of work and do not reveal registered emails.
const dummyPasswordHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

func (a *api) login(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &request, false) {
		return
	}
	request.Email = strings.ToLower(strings.TrimSpace(request.Email))
	if request.Email == "" || len(request.Email) > 320 || request.Password == "" || len(request.Password) > 4096 {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "Email and password are required")
		return
	}
	allowed, _, err := a.Cache.Allow(r.Context(), "admin-login:"+remoteIP(r), 10, 5*time.Minute)
	if err != nil {
		writeAdminError(w, http.StatusServiceUnavailable, "login_unavailable", "Login rate limiter is unavailable")
		return
	}
	if !allowed {
		writeAdminError(w, http.StatusTooManyRequests, "rate_limited", "Too many login attempts")
		return
	}

	user, lookupErr := a.Store.AdminUserByEmail(r.Context(), request.Email)
	hash := dummyPasswordHash
	if lookupErr == nil && user.PasswordHash != "" {
		hash = user.PasswordHash
	}
	passwordErr := bcrypt.CompareHashAndPassword([]byte(hash), []byte(request.Password))
	if lookupErr != nil || passwordErr != nil {
		if lookupErr != nil && !errors.Is(lookupErr, store.ErrNotFound) {
			a.Logger.ErrorContext(r.Context(), "read login account", "error", lookupErr)
			writeAdminError(w, http.StatusInternalServerError, "internal_error", "Unable to sign in")
			return
		}
		writeAdminError(w, http.StatusUnauthorized, "invalid_credentials", "Email or password is incorrect")
		return
	}

	plain := newAdminSessionToken()
	expiresAt := now().Add(adminSessionTTL)
	if err := a.Store.CreateAdminSession(r.Context(), newID("session"), user.ID, tokenHash(plain), remoteIP(r), clean(r.UserAgent(), 512), user.AuthVersion, expiresAt); err != nil {
		a.Logger.ErrorContext(r.Context(), "create admin session", "error", err)
		writeAdminError(w, http.StatusInternalServerError, "internal_error", "Unable to sign in")
		return
	}
	setAdminSessionCookie(w, r, plain, expiresAt)
	identity := adminIdentity{SessionID: "", UserID: user.ID, Email: user.Email, DisplayName: user.DisplayName, Role: user.Role}
	a.audit(r.WithContext(withAdminIdentity(r.Context(), identity)), "session.login", "user", user.ID, user.Email, nil, nil)
	writeJSON(w, http.StatusOK, identity)
}

func (a *api) currentSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, currentAdmin(r))
}

func (a *api) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(adminSessionCookie); err == nil && cookie.Value != "" {
		if err := a.Store.RevokeAdminSession(r.Context(), tokenHash(cookie.Value)); err != nil {
			a.Logger.ErrorContext(r.Context(), "revoke admin session", "error", err)
		}
	}
	identity := currentAdmin(r)
	a.audit(r, "session.logout", "user", identity.UserID, identity.Email, nil, nil)
	clearAdminSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func newAdminSessionToken() string {
	return strings.Replace(newToken(), "hub_rt_", "hub_admin_", 1)
}

func setAdminSessionCookie(w http.ResponseWriter, r *http.Request, value string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: adminSessionCookie, Value: value, Path: "/", HttpOnly: true,
		Secure: secureRequest(r), SameSite: http.SameSiteLaxMode,
		Expires: expiresAt, MaxAge: int(time.Until(expiresAt).Seconds()),
	})
}

func clearAdminSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: adminSessionCookie, Value: "", Path: "/", HttpOnly: true,
		Secure: secureRequest(r), SameSite: http.SameSiteLaxMode,
		Expires: time.Unix(1, 0), MaxAge: -1,
	})
}

func secureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func (a *api) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Token       string `json:"token"`
		Password    string `json:"password"`
		DisplayName string `json:"displayName"`
	}
	if !decodeJSON(w, r, &request, false) {
		return
	}
	if len(request.Token) < 32 || len(request.Token) > 256 || len(request.Password) < 12 || len(request.Password) > 72 || strings.TrimSpace(request.DisplayName) == "" {
		writeAdminError(w, 400, "invalid_input", "请填写邀请令牌、姓名及 12 至 72 字节的密码")
		return
	}
	allowed, _, err := a.Cache.Allow(r.Context(), "accept-invitation:"+remoteIP(r), 10, 5*time.Minute)
	if err != nil {
		writeAdminError(w, 503, "login_unavailable", "邀请服务暂不可用")
		return
	}
	if !allowed {
		writeAdminError(w, 429, "rate_limited", "请稍后重试")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(request.Password), 12)
	if err != nil {
		writeAdminError(w, 400, "invalid_input", "密码格式不正确")
		return
	}
	member, err := a.Store.AcceptInvitation(r.Context(), tokenHash(request.Token), string(hash), clean(request.DisplayName, 160))
	if errors.Is(err, store.ErrNotFound) {
		writeAdminError(w, 400, "invitation_invalid", "邀请已过期、已使用或已撤销，请联系管理员重新邀请")
		return
	}
	if err != nil {
		a.writeStoreError(w, r, err, "accept invitation")
		return
	}
	writeJSON(w, 200, map[string]any{"email": member.Email})
}
