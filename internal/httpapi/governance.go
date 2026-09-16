package httpapi

import (
	"encoding/json"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"apihub-go/internal/model"
	"github.com/go-chi/chi/v5"
)

func (a *api) listAudit(w http.ResponseWriter, r *http.Request) {
	o, ok := listOptions(w, r)
	if !ok {
		return
	}
	items, err := a.Store.ListAudit(r.Context(), o.Query, o.Limit, o)
	if len(items) > 0 {
		last := items[len(items)-1]
		nextCursor(w, len(items), o.Limit, last.CreatedAt, last.ID)
	}
	writeStoreResult(w, items, err, "list audit logs")
}

func (a *api) listTeamMembers(w http.ResponseWriter, r *http.Request) {
	items, err := a.Store.ListTeamMembers(r.Context())
	writeStoreResult(w, items, err, "list team members")
}

func (a *api) inviteTeamMember(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if !decodeJSON(w, r, &request, false) {
		return
	}
	request.Email = strings.ToLower(strings.TrimSpace(request.Email))
	if _, err := mail.ParseAddress(request.Email); err != nil || !validRole(request.Role) || request.Role == "owner" {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "a valid email and non-owner role are required")
		return
	}
	plain := newToken()
	item, err := a.Store.InviteMember(r.Context(), request.Email, request.Role, currentAdmin(r).UserID, tokenHash(plain), now().Add(7*24*time.Hour))
	if err != nil {
		a.writeStoreError(w, r, err, "invite team member")
		return
	}
	a.audit(r, "team_member.invited", "team_member", item.ID, item.Email, nil, item)
	writeJSON(w, http.StatusCreated, map[string]any{"member": item, "invitationToken": plain, "expiresIn": "168h"})
}

func (a *api) updateTeamMember(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Role    string `json:"role"`
		Version int64  `json:"version"`
	}
	if !decodeJSON(w, r, &request, false) {
		return
	}
	if !validRole(request.Role) || request.Version < 1 {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "role and version are required")
		return
	}
	before, loadErr := a.Store.TeamMember(r.Context(), chi.URLParam(r, "id"))
	if loadErr != nil {
		a.writeStoreError(w, r, loadErr, "load team member")
		return
	}
	if currentAdmin(r).Role != "owner" && (before.Role == "owner" || request.Role == "owner") {
		writeAdminError(w, http.StatusForbidden, "forbidden", "Only an owner may change ownership")
		return
	}
	item, err := a.Store.UpdateMemberRole(r.Context(), chi.URLParam(r, "id"), request.Role, request.Version)
	if err != nil {
		a.writeStoreError(w, r, err, "update team member")
		return
	}
	a.audit(r, "team_member.updated", "team_member", item.ID, item.Email, before, item)
	writeJSON(w, http.StatusOK, item)
}

func (a *api) removeTeamMember(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	before, _ := a.Store.TeamMember(r.Context(), id)
	if err := a.Store.RemoveMember(r.Context(), id); err != nil {
		a.writeStoreError(w, r, err, "remove team member")
		return
	}
	a.audit(r, "team_member.removed", "team_member", id, before.Email, before, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) getSettings(w http.ResponseWriter, r *http.Request) {
	item, err := a.Store.Settings(r.Context())
	writeStoreResult(w, item, err, "get settings")
}

func (a *api) saveSettings(w http.ResponseWriter, r *http.Request) {
	var request model.PlatformSettings
	if !decodeJSON(w, r, &request, false) {
		return
	}
	request.PlatformName = clean(request.PlatformName, 160)
	if request.PlatformName == "" || request.Version < 1 || request.OperationRetentionDays < 1 || request.OperationRetentionDays > 3650 ||
		(request.AuditRetentionDays != nil && (*request.AuditRetentionDays < 1 || *request.AuditRetentionDays > 3650)) ||
		(request.PublicBaseURL != "" && !validPublicBaseURL(request.PublicBaseURL)) || (len(request.RuntimeParameters) > 0 && !json.Valid(request.RuntimeParameters)) {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "platformName, version, valid retention and HTTPS publicBaseUrl are required")
		return
	}
	before, _ := a.Store.Settings(r.Context())
	item, err := a.Store.SaveSettings(r.Context(), request, currentAdmin(r).UserID)
	if err != nil {
		a.writeStoreError(w, r, err, "save settings")
		return
	}
	a.audit(r, "settings.updated", "settings", a.Store.WorkspaceID(), item.PlatformName, before, item)
	writeJSON(w, http.StatusOK, item)
}

func validPublicBaseURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	host := parsed.Hostname()
	return parsed.Scheme == "http" && (host == "localhost" || host == "127.0.0.1" || host == "::1")
}

func validRole(role string) bool {
	switch role {
	case "owner", "admin", "developer", "viewer":
		return true
	default:
		return false
	}
}
