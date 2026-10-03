package httpapi

import (
	"apihub-go/internal/jsonutil"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"apihub-go/internal/model"
	"apihub-go/internal/store"
	"github.com/go-chi/chi/v5"
)

type oauthState struct {
	IntegrationID  string `json:"integrationId"`
	SystemKey      string `json:"systemKey"`
	EndUserKey     string `json:"endUserKey"`
	EndUserName    string `json:"endUserName"`
	ConnectionKey  string `json:"connectionKey"`
	ConnectionName string `json:"connectionName"`
	ReturnPath     string `json:"returnPath"`
	RedirectURI    string `json:"redirectUri"`
	Verifier       string `json:"verifier"`
	Nonce          string `json:"nonce,omitempty"`
}

func (a *api) oauthStart(w http.ResponseWriter, r *http.Request) {
	var request struct {
		IntegrationID  string `json:"integrationId"`
		EndUserKey     string `json:"endUserKey"`
		EndUserName    string `json:"endUserName"`
		ConnectionKey  string `json:"connectionKey"`
		ConnectionName string `json:"connectionName"`
		ReturnPath     string `json:"returnPath"`
	}
	if !decodeJSON(w, r, &request, false) {
		return
	}
	if request.IntegrationID == "" || clean(request.EndUserKey, 255) == "" || !validIdentifier(request.ConnectionKey) {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "integrationId, endUserKey and connectionKey are required")
		return
	}
	integration, err := a.Store.Integration(r.Context(), request.IntegrationID)
	if err != nil {
		a.writeStoreError(w, r, err, "get OAuth integration")
		return
	}
	if integration.Status != "ready" {
		writeAdminError(w, http.StatusConflict, "integration_not_ready", "OAuth integration must be ready")
		return
	}
	instance, err := a.Store.AuthInstance(r.Context(), integration.AuthInstanceID)
	if err != nil || instance.Status != "ready" || (instance.AuthTemplateFlow != "oauth2_code" && instance.AuthTemplateFlow != "oidc") {
		writeAdminError(w, http.StatusConflict, "oauth_not_supported", "integration does not use OAuth authorization code")
		return
	}
	template, err := a.Store.AuthTemplate(r.Context(), instance.AuthTemplateID)
	if err != nil || template.Status != "published" {
		writeAdminError(w, http.StatusConflict, "oauth_not_supported", "OAuth authentication template is not published")
		return
	}
	config := struct {
		AuthorizationURL    string            `json:"authorizationUrl"`
		Scopes              []string          `json:"scopes"`
		AuthorizationParams map[string]string `json:"authorizationParams"`
	}{}
	_ = jsonutil.Unmarshal(instance.PublicConfig, &config)
	authorizeURL, err := url.Parse(config.AuthorizationURL)
	if err != nil || (authorizeURL.Scheme != "http" && authorizeURL.Scheme != "https") || authorizeURL.Host == "" {
		writeAdminError(w, http.StatusConflict, "oauth_not_configured", "authorizationUrl is missing from the authentication instance publicConfig")
		return
	}
	secrets, err := a.Auth.AuthInstanceSecrets(instance)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "oauth_secret_error", "Could not read OAuth client settings")
		return
	}
	clientID := firstMapString(secrets, "clientId", "client_id")
	if clientID == "" {
		writeAdminError(w, http.StatusConflict, "oauth_not_configured", "OAuth clientId is missing")
		return
	}
	if instance.AuthTemplateFlow == "oidc" {
		if err := a.Auth.VerifyOIDCProvider(r.Context(), instance); err != nil {
			writeAdminError(w, http.StatusConflict, "oidc_not_configured", err.Error())
			return
		}
	}
	base := ""
	settings, settingsErr := a.Store.Settings(r.Context())
	if settingsErr == nil {
		base = strings.TrimRight(settings.PublicBaseURL, "/")
	}
	if base == "" {
		base = strings.TrimRight(a.PublicBaseURL, "/")
	}
	if base == "" {
		writeAdminError(w, http.StatusConflict, "public_url_missing", "Configure publicBaseUrl before starting OAuth")
		return
	}
	redirectURI := base + "/oauth/callback/" + url.PathEscape(integration.SystemKey)
	stateToken := newToken()
	verifier := strings.TrimPrefix(newToken(), "hub_rt_")
	challengeBytes := sha256.Sum256([]byte(verifier))
	state := oauthState{IntegrationID: integration.ID, SystemKey: integration.SystemKey, EndUserKey: request.EndUserKey, EndUserName: request.EndUserName, ConnectionKey: request.ConnectionKey, ConnectionName: request.ConnectionName, ReturnPath: safeReturnPath(request.ReturnPath), RedirectURI: redirectURI, Verifier: verifier}
	if instance.AuthTemplateFlow == "oidc" {
		state.Nonce = newToken()
	}
	encoded, _ := json.Marshal(state)
	if err := a.Cache.PutOAuthState(r.Context(), tokenHash(stateToken), encoded, 10*time.Minute); err != nil {
		writeAdminError(w, http.StatusServiceUnavailable, "oauth_state_unavailable", "Could not create OAuth state")
		return
	}
	query := authorizeURL.Query()
	query.Set("response_type", "code")
	query.Set("client_id", clientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("state", stateToken)
	query.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challengeBytes[:]))
	query.Set("code_challenge_method", "S256")
	if len(config.Scopes) > 0 {
		query.Set("scope", strings.Join(config.Scopes, " "))
	}
	if state.Nonce != "" {
		query.Set("scope", strings.TrimSpace("openid "+query.Get("scope")))
		query.Set("nonce", state.Nonce)
	}
	for key, value := range config.AuthorizationParams {
		if key != "state" && key != "redirect_uri" && key != "client_id" && key != "response_type" && key != "code_challenge" && key != "code_challenge_method" && key != "nonce" && key != "scope" {
			query.Set(key, value)
		}
	}
	authorizeURL.RawQuery = query.Encode()
	writeJSON(w, http.StatusOK, map[string]any{"authorizationUrl": authorizeURL.String(), "expiresIn": 600})
}

func (a *api) oauthCallback(w http.ResponseWriter, r *http.Request) {
	if providerError := clean(r.URL.Query().Get("error"), 200); providerError != "" {
		if stateToken := r.URL.Query().Get("state"); stateToken != "" && a.Cache != nil {
			encoded, err := a.Cache.TakeOAuthState(r.Context(), tokenHash(stateToken))
			var state oauthState
			if err == nil && jsonutil.Unmarshal(encoded, &state) == nil && state.SystemKey == chi.URLParam(r, "systemKey") {
				if target := oauthResultPath(state.ReturnPath, "error", ""); target != "" {
					http.Redirect(w, r, target, http.StatusFound)
					return
				}
			}
		}
		writeAdminError(w, http.StatusBadRequest, "oauth_provider_error", providerError)
		return
	}
	stateToken, code := r.URL.Query().Get("state"), r.URL.Query().Get("code")
	if stateToken == "" || code == "" {
		writeAdminError(w, http.StatusBadRequest, "invalid_oauth_callback", "state and code are required")
		return
	}
	encoded, err := a.Cache.TakeOAuthState(r.Context(), tokenHash(stateToken))
	if err != nil {
		writeAdminError(w, http.StatusBadRequest, "oauth_state_invalid", "OAuth state is invalid or expired")
		return
	}
	var state oauthState
	if jsonutil.Unmarshal(encoded, &state) != nil || state.SystemKey != chi.URLParam(r, "systemKey") {
		writeAdminError(w, http.StatusBadRequest, "oauth_state_invalid", "OAuth state does not match this callback")
		return
	}
	integration, err := a.Store.Integration(r.Context(), state.IntegrationID)
	if err != nil || integration.Status != "ready" || integration.SystemKey != state.SystemKey {
		writeAdminError(w, http.StatusBadRequest, "oauth_integration_invalid", "OAuth integration no longer exists")
		return
	}
	instance, err := a.Store.AuthInstance(r.Context(), integration.AuthInstanceID)
	if err != nil || instance.Status != "ready" || (instance.AuthTemplateFlow != "oauth2_code" && instance.AuthTemplateFlow != "oidc") {
		writeAdminError(w, http.StatusBadRequest, "oauth_auth_invalid", "OAuth authentication instance no longer exists")
		return
	}
	template, err := a.Store.AuthTemplate(r.Context(), instance.AuthTemplateID)
	if err != nil || template.Status != "published" {
		writeAdminError(w, http.StatusBadRequest, "oauth_auth_invalid", "OAuth authentication template is no longer published")
		return
	}
	startedAt := now()
	expiresAt, operationErr := a.Store.OperationExpiresAt(r.Context(), startedAt)
	if operationErr != nil {
		a.writeStoreError(w, r, operationErr, "prepare OAuth operation")
		return
	}
	operation := model.OperationRun{
		ID:            newID("operation"),
		RequestID:     requestID(r),
		Kind:          "auth",
		Name:          "OAuth 授权 · " + integration.Name,
		Status:        "running",
		SystemID:      integration.SystemID,
		IntegrationID: integration.ID,
		Source:        "oauth_callback",
		StartedAt:     startedAt,
		ExpiresAt:     expiresAt,
	}
	if createErr := a.Store.CreateOperation(r.Context(), operation); createErr != nil {
		a.writeStoreError(w, r, createErr, "create OAuth operation")
		return
	}
	_ = a.Store.AddOperationEvent(r.Context(), operation.ID, "info", "开始交换 OAuth 授权码", nil)
	tokens, err := a.Auth.ExchangeOAuthCode(r.Context(), instance, code, state.RedirectURI, state.Verifier)
	if err != nil {
		_ = a.Store.CompleteOperation(r.Context(), operation.ID, "failed", http.StatusBadGateway, nil, "oauth_exchange_failed", err.Error())
		a.Logger.ErrorContext(r.Context(), "exchange OAuth code", "system", state.SystemKey, "error", err)
		writeAdminError(w, http.StatusBadGateway, "oauth_exchange_failed", err.Error())
		return
	}
	if instance.AuthTemplateFlow == "oidc" {
		if err := a.Auth.VerifyOIDCTokens(r.Context(), instance, tokens, state.Nonce); err != nil {
			_ = a.Store.CompleteOperation(r.Context(), operation.ID, "failed", http.StatusBadGateway, nil, "oidc_verification_failed", err.Error())
			writeAdminError(w, http.StatusBadGateway, "oidc_verification_failed", err.Error())
			return
		}
	}
	id := store.StableID("connection", a.Store.WorkspaceID()+":"+state.ConnectionKey)
	expectedRevision := int64(0)
	if current, currentErr := a.Store.Connection(r.Context(), id); currentErr == nil {
		expectedRevision = current.Revision
	}
	blob, err := a.Auth.SealCredentials(id, expectedRevision+1, tokens.Credentials)
	if err != nil {
		_ = a.Store.CompleteOperation(r.Context(), operation.ID, "failed", http.StatusInternalServerError, nil, "secret_encryption_failed", "Could not encrypt OAuth tokens")
		writeAdminError(w, http.StatusInternalServerError, "secret_encryption_failed", "Could not encrypt OAuth tokens")
		return
	}
	name := clean(state.ConnectionName, 160)
	if name == "" {
		name = state.ConnectionKey
	}
	connection, err := a.Store.SaveConnection(r.Context(), store.SaveConnectionInput{Connection: model.Connection{ID: id, ConnectionKey: state.ConnectionKey, Name: name, IntegrationID: integration.ID, AuthInstanceID: instance.ID, Status: "active", CredentialBlob: blob, KeyVersion: a.Codec.Version(), TokenExpiresAt: tokens.ExpiresAt}, EndUser: model.EndUser{ExternalKey: state.EndUserKey, DisplayName: state.EndUserName, Metadata: json.RawMessage(`{}`)}, ExpectedRevision: expectedRevision})
	if err != nil {
		_ = a.Store.CompleteOperation(r.Context(), operation.ID, "failed", http.StatusConflict, nil, "connection_save_failed", err.Error())
		a.writeStoreError(w, r, err, "save OAuth connection")
		return
	}
	_ = a.Store.AddOperationEvent(r.Context(), operation.ID, "info", "OAuth 令牌已加密保存", nil)
	_ = a.Store.CompleteOperation(r.Context(), operation.ID, "success", http.StatusCreated, store.MarshalJSON(map[string]any{"connectionId": connection.ID}), "", "")
	if target := oauthResultPath(state.ReturnPath, "success", connection.ID); target != "" {
		http.Redirect(w, r, target, http.StatusFound)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"connected": true, "connection": connection})
}

func firstMapString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func safeReturnPath(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//") && !strings.Contains(value, "\\") {
		return value
	}
	return ""
}

func oauthResultPath(returnPath, result, connectionID string) string {
	target, err := url.Parse(safeReturnPath(returnPath))
	if err != nil || target.Path == "" {
		return ""
	}
	query := target.Query()
	query.Set("oauth", result)
	if connectionID != "" {
		query.Set("connectionId", connectionID)
	}
	target.RawQuery = query.Encode()
	return target.String()
}
