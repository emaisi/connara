package httpapi

import (
	"apihub-go/internal/executor"
	"apihub-go/internal/jsonutil"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"apihub-go/internal/authn"
	"apihub-go/internal/model"
	"apihub-go/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (a *api) listSystemGroups(w http.ResponseWriter, r *http.Request) {
	items, err := a.Store.ListSystemGroups(r.Context())
	writeStoreResult(w, items, err, "list system groups")
}

func (a *api) saveSystemGroup(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name      string `json:"name"`
		SortOrder int    `json:"sortOrder"`
	}
	if !decodeJSON(w, r, &request, false) {
		return
	}
	request.Name = clean(request.Name, 100)
	if request.Name == "" {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "name is required")
		return
	}
	item, err := a.Store.SaveSystemGroup(r.Context(), model.SystemGroup{
		ID: chi.URLParam(r, "id"), Name: request.Name, SortOrder: request.SortOrder,
	})
	if err != nil {
		a.writeStoreError(w, r, err, "save system group")
		return
	}
	a.audit(r, "system_group.saved", "system_group", item.ID, item.Name, nil, item)
	writeJSON(w, statusForSave(r), item)
}

func (a *api) deleteSystemGroup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.DeleteSystemGroup(r.Context(), id); err != nil {
		a.writeStoreError(w, r, err, "delete system group")
		return
	}
	a.audit(r, "system_group.deleted", "system_group", id, id, nil, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) listSystems(w http.ResponseWriter, r *http.Request) {
	o, ok := listOptions(w, r)
	if !ok {
		return
	}
	items, err := a.Store.ListSystems(r.Context(), o)
	if len(items) > 0 {
		last := items[len(items)-1]
		nextCursor(w, len(items), o.Limit, last.UpdatedAt, last.ID)
	}
	writeStoreResult(w, items, err, "list systems")
}

func (a *api) getSystem(w http.ResponseWriter, r *http.Request) {
	item, err := a.Store.System(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.writeStoreError(w, r, err, "get system")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *api) createSystem(w http.ResponseWriter, r *http.Request) {
	var request struct {
		SystemKey             string   `json:"systemKey"`
		Name                  string   `json:"name"`
		GroupID               string   `json:"groupId"`
		Description           string   `json:"description"`
		HomepageURL           string   `json:"homepageUrl"`
		IconKey               string   `json:"iconKey"`
		DefaultAuthTemplateID string   `json:"defaultAuthTemplateId"`
		AuthTemplateIDs       []string `json:"authTemplateIds"`
	}
	if !decodeJSON(w, r, &request, false) {
		return
	}
	request.SystemKey = strings.ToLower(clean(request.SystemKey, 100))
	request.Name = clean(request.Name, 160)
	if !validIdentifier(request.SystemKey) || request.Name == "" || request.GroupID == "" || request.DefaultAuthTemplateID == "" {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "systemKey, name, groupId and defaultAuthTemplateId are required")
		return
	}
	item, err := a.Store.CreateSystem(r.Context(), model.System{
		SystemKey: request.SystemKey, Name: request.Name, GroupID: request.GroupID,
		AuthTemplateIDs: request.AuthTemplateIDs,
		Description:     clean(request.Description, 1000), HomepageURL: clean(request.HomepageURL, 2048),
		IconKey: clean(request.IconKey, 255),
	}, request.DefaultAuthTemplateID)
	if err != nil {
		a.writeStoreError(w, r, err, "create system")
		return
	}
	a.audit(r, "system.created", "system", item.ID, item.Name, nil, item)
	writeJSON(w, http.StatusCreated, item)
}

func (a *api) updateSystem(w http.ResponseWriter, r *http.Request) {
	var request struct {
		GroupID string `json:"groupId"`
	}
	if !decodeJSON(w, r, &request, false) {
		return
	}
	if strings.TrimSpace(request.GroupID) == "" {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "groupId is required")
		return
	}
	id := chi.URLParam(r, "id")
	before, _ := a.Store.System(r.Context(), id)
	item, err := a.Store.UpdateSystemGroup(r.Context(), id, request.GroupID)
	if err != nil {
		a.writeStoreError(w, r, err, "update system")
		return
	}
	a.audit(r, "system.updated", "system", item.ID, item.Name, before, item)
	writeJSON(w, http.StatusOK, item)
}

func (a *api) setSystemAuthTemplates(w http.ResponseWriter, r *http.Request) {
	var request struct {
		AuthTemplateIDs       []string `json:"authTemplateIds"`
		DefaultAuthTemplateID string   `json:"defaultAuthTemplateId"`
	}
	if !decodeJSON(w, r, &request, false) {
		return
	}
	id := chi.URLParam(r, "id")
	if err := a.Store.SetSystemAuthTemplates(r.Context(), id, request.AuthTemplateIDs, request.DefaultAuthTemplateID); err != nil {
		a.writeStoreError(w, r, err, "set system auth templates")
		return
	}
	item, _ := a.Store.System(r.Context(), id)
	a.audit(r, "system.auth_templates_updated", "system", id, item.Name, nil, request)
	writeJSON(w, http.StatusOK, item)
}

func (a *api) listAuthTemplates(w http.ResponseWriter, r *http.Request) {
	items, err := a.Store.ListAuthTemplates(r.Context())
	writeStoreResult(w, items, err, "list auth templates")
}

func (a *api) saveAuthTemplate(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Version          int64           `json:"version"`
		TemplateKey      string          `json:"templateKey"`
		Name             string          `json:"name"`
		FlowType         string          `json:"flowType"`
		Status           string          `json:"status"`
		CredentialSchema json.RawMessage `json:"credentialSchema"`
		TokenRequest     json.RawMessage `json:"tokenRequest"`
		InjectionRules   json.RawMessage `json:"injectionRules"`
	}
	if !decodeJSON(w, r, &request, false) {
		return
	}
	request.TemplateKey = strings.ToLower(clean(request.TemplateKey, 100))
	if !validIdentifier(request.TemplateKey) || strings.TrimSpace(request.Name) == "" || strings.TrimSpace(request.FlowType) == "" {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "templateKey, name and flowType are required")
		return
	}
	if request.FlowType != "static" && request.FlowType != "password_token" && request.FlowType != "client_credentials" {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "custom flowType must be static, password_token or client_credentials")
		return
	}
	var rules []struct {
		Target   string `json:"target"`
		Name     string `json:"name"`
		Template string `json:"template"`
	}
	if len(request.InjectionRules) == 0 || jsonutil.Unmarshal(request.InjectionRules, &rules) != nil || len(rules) > 16 {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "injectionRules must be a JSON array with at most 16 entries")
		return
	}
	for _, rule := range rules {
		if !validInjectionName(rule.Target, rule.Name) || strings.TrimSpace(rule.Template) == "" || len(rule.Template) > 1000 {
			writeAdminError(w, http.StatusBadRequest, "invalid_input", "injection rule target, name or template is invalid")
			return
		}
	}
	if request.Status == "" {
		request.Status = "draft"
	}
	if !validStatus(request.Status, "draft", "published", "disabled") {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "status must be draft, published or disabled")
		return
	}
	candidate := model.AuthTemplate{
		ID: chi.URLParam(r, "id"), TemplateKey: request.TemplateKey, Name: clean(request.Name, 160),
		Source: "custom", FlowType: clean(request.FlowType, 48), Status: request.Status,
		CredentialSchema: request.CredentialSchema, TokenRequest: request.TokenRequest,
		InjectionRules: request.InjectionRules, Version: request.Version,
	}
	if err := authn.ValidateTemplateConfiguration(candidate); err != nil {
		writeAdminError(w, 400, "invalid_auth_config", err.Error())
		return
	}
	if candidate.ID != "" && request.Version == 0 {
		writeAdminError(w, 409, "conflict", "template version is required for updates")
		return
	}
	item, err := a.Store.SaveAuthTemplate(r.Context(), candidate)
	if err != nil {
		a.writeStoreError(w, r, err, "save auth template")
		return
	}
	a.audit(r, "auth_template.saved", "auth_template", item.ID, item.Name, nil, item)
	writeJSON(w, statusForSave(r), item)
}

func validInjectionName(target, name string) bool {
	name = strings.TrimSpace(name)
	if len(name) == 0 || len(name) > 255 {
		return false
	}
	if target != "header" && target != "query" && target != "cookie" {
		return false
	}
	if target == "header" {
		for _, blocked := range []string{"Host", "Content-Length", "Transfer-Encoding", "Connection", "Trailer", "Upgrade"} {
			if strings.EqualFold(name, blocked) {
				return false
			}
		}
	}
	for _, character := range name {
		if character > 127 || (!(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') && !(character >= '0' && character <= '9') && !strings.ContainsRune("-_.", character)) {
			return false
		}
	}
	return true
}

func (a *api) setAuthTemplateStatus(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Status  string `json:"status"`
		Version int64  `json:"version"`
	}
	if !decodeJSON(w, r, &request, false) {
		return
	}
	if request.Status != "draft" && request.Status != "published" && request.Status != "disabled" {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "invalid auth template status")
		return
	}
	if request.Status == "published" {
		current, err := a.Store.AuthTemplate(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			a.writeStoreError(w, r, err, "load auth template")
			return
		}
		if current.Source == "custom" {
			if err := authn.ValidateTemplateConfiguration(current); err != nil {
				writeAdminError(w, 400, "invalid_auth_config", err.Error())
				return
			}
		}
	}
	item, err := a.Store.SetAuthTemplateStatus(r.Context(), chi.URLParam(r, "id"), request.Status, request.Version)
	if err != nil {
		a.writeStoreError(w, r, err, "set auth template status")
		return
	}
	a.audit(r, "auth_template.status_updated", "auth_template", item.ID, item.Name, nil, request)
	writeJSON(w, http.StatusOK, item)
}

func (a *api) deleteAuthTemplate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.DeleteAuthTemplate(r.Context(), id); err != nil {
		a.writeStoreError(w, r, err, "delete auth template")
		return
	}
	a.audit(r, "auth_template.deleted", "auth_template", id, id, nil, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) listAuthInstances(w http.ResponseWriter, r *http.Request) {
	items, err := a.Store.ListAuthInstances(r.Context(), r.URL.Query().Get("system"))
	writeStoreResult(w, items, err, "list auth instances")
}

func (a *api) saveAuthInstance(w http.ResponseWriter, r *http.Request) {
	a.processAuthInstance(w, r, false)
}

func (a *api) checkAuthInstance(w http.ResponseWriter, r *http.Request) {
	a.processAuthInstance(w, r, true)
}

func (a *api) processAuthInstance(w http.ResponseWriter, r *http.Request, checkOnly bool) {
	var request struct {
		InstanceKey         string          `json:"instanceKey"`
		Name                string          `json:"name"`
		SystemID            string          `json:"systemId"`
		AuthTemplateID      string          `json:"authTemplateId"`
		Status              string          `json:"status"`
		TokenURL            string          `json:"tokenUrl"`
		RefreshURL          string          `json:"refreshUrl"`
		TokenPath           string          `json:"tokenPath"`
		ExpiryPath          string          `json:"expiryPath"`
		HeaderName          string          `json:"headerName"`
		HeaderValueTemplate string          `json:"headerValueTemplate"`
		PublicConfig        json.RawMessage `json:"publicConfig"`
		Secrets             map[string]any  `json:"secrets"`
		Version             int64           `json:"version"`
		TestCredentials     map[string]any  `json:"testCredentials"`
		Authenticate        bool            `json:"authenticate"`
		VerifyAPI           bool            `json:"verifyApi"`
		TestTarget          struct {
			IntegrationID string `json:"integrationId"`
			BaseURL       string `json:"baseUrl"`
		} `json:"testTarget"`
	}
	if !decodeJSON(w, r, &request, false) {
		return
	}
	request.InstanceKey = strings.ToLower(clean(request.InstanceKey, 100))
	if !validIdentifier(request.InstanceKey) || strings.TrimSpace(request.Name) == "" || request.SystemID == "" || request.AuthTemplateID == "" {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "instanceKey, name, systemId and authTemplateId are required")
		return
	}
	request.Status = defaultStatus(request.Status, "draft")
	if !validStatus(request.Status, "ready", "draft", "disabled") ||
		(request.TokenURL != "" && !validAbsoluteURL(request.TokenURL)) ||
		(request.RefreshURL != "" && !validAbsoluteURL(request.RefreshURL)) ||
		(request.HeaderName != "" && !validInjectionName("header", request.HeaderName)) {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "status, authentication URLs or header name are invalid")
		return
	}
	template, err := a.Store.AuthTemplate(r.Context(), request.AuthTemplateID)
	if err != nil {
		a.writeStoreError(w, r, err, "get auth template")
		return
	}
	if (checkOnly || request.Status == "ready") && (template.Status != "published" || !authn.SupportsTemplate(template.TemplateKey, template.FlowType)) {
		writeAdminError(w, http.StatusConflict, "auth_template_not_executable", "ready auth instances require a published template with a supported authentication flow")
		return
	}
	if checkOnly || request.Status == "ready" {
		if err := authn.ValidateInstanceConfiguration(model.AuthInstance{AuthTemplateFlow: template.FlowType, TokenURL: request.TokenURL, RefreshURL: request.RefreshURL, TokenPath: request.TokenPath, ExpiryPath: request.ExpiryPath, PublicConfig: request.PublicConfig}); err != nil {
			writeAuthConfigurationError(w, err.Error())
			return
		}
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		id = newID("auth-instance")
	}

	var secretBlob []byte
	sealSecrets := len(request.Secrets) > 0
	version := int64(0)
	if chi.URLParam(r, "id") != "" {
		current, loadErr := a.Store.AuthInstance(r.Context(), id)
		if loadErr != nil {
			a.writeStoreError(w, r, loadErr, "load auth instance")
			return
		}
		version = current.Version
		if request.Version != 0 && request.Version != version {
			a.writeStoreError(w, r, store.ErrConflict, "stale auth instance")
			return
		}
		if len(request.Secrets) > 0 {
			merged, openErr := a.Auth.AuthInstanceSecrets(current)
			if openErr != nil {
				a.writeStoreError(w, r, openErr, "open auth secrets")
				return
			}
			for key, value := range request.Secrets {
				if value == nil {
					delete(merged, key)
				} else {
					merged[key] = value
				}
			}
			request.Secrets = merged
		}
	}
	err = nil
	if sealSecrets {
		plain, _ := json.Marshal(request.Secrets)
		secretBlob, err = a.Codec.Encrypt(plain, []byte(a.Store.WorkspaceID()+":auth-instance:"+id))
		if err != nil {
			writeAdminError(w, http.StatusInternalServerError, "secret_encryption_failed", "Could not encrypt auth instance secrets")
			return
		}
	}
	item := model.AuthInstance{
		ID: id, InstanceKey: request.InstanceKey, Name: clean(request.Name, 160),
		SystemID: request.SystemID, AuthTemplateID: request.AuthTemplateID, Status: request.Status,
		TokenURL: clean(request.TokenURL, 2048), RefreshURL: clean(request.RefreshURL, 2048),
		TokenPath: clean(request.TokenPath, 255), ExpiryPath: clean(request.ExpiryPath, 255),
		HeaderName: clean(request.HeaderName, 255), HeaderValueTemplate: clean(request.HeaderValueTemplate, 1000),
		Version: version, PublicConfig: request.PublicConfig, SecretBlob: secretBlob, KeyVersion: a.Codec.Version(),
		AuthTemplateKey: template.TemplateKey, AuthTemplateFlow: template.FlowType,
	}
	if !sealSecrets && chi.URLParam(r, "id") != "" {
		current, loadErr := a.Store.AuthInstance(r.Context(), id)
		if loadErr != nil {
			a.writeStoreError(w, r, loadErr, "load instance secrets")
			return
		}
		item.SecretBlob = current.SecretBlob
	}
	validateConfiguration := authn.ValidateEffectiveConfiguration
	if request.Status == "draft" && !checkOnly {
		validateConfiguration = authn.ValidateDraftConfiguration
	}
	if err := validateConfiguration(item, template); err != nil {
		writeAuthConfigurationError(w, err.Error())
		return
	}
	if checkOnly || request.Status == "ready" {
		if err := a.Auth.ValidateInstanceSecrets(item); err != nil {
			writeAuthConfigurationError(w, err.Error())
			return
		}
		if err := a.Auth.ValidateRequestSecrets(item, template); err != nil {
			writeAuthConfigurationError(w, err.Error())
			return
		}
	}
	if checkOnly {
		system, err := a.Store.System(r.Context(), item.SystemID)
		if err != nil {
			a.writeStoreError(w, r, err, "get system")
			return
		}
		compatible := false
		for _, templateID := range system.AuthTemplateIDs {
			if templateID == template.ID {
				compatible = true
			}
		}
		if !compatible {
			writeAdminError(w, http.StatusConflict, "invalid_auth_config", "system does not allow this auth template")
			return
		}
		if request.Authenticate {
			baseURL := request.TestTarget.BaseURL
			if request.TestTarget.IntegrationID != "" {
				if baseURL != "" {
					writeAdminError(w, 400, "invalid_auth_config", "test target must select one source")
					return
				}
				integration, err := a.Store.Integration(r.Context(), request.TestTarget.IntegrationID)
				if err != nil {
					a.writeStoreError(w, r, err, "load test integration")
					return
				}
				if integration.SystemID != item.SystemID || (integration.AuthInstanceID != "" && integration.AuthInstanceID != item.ID) {
					writeAdminError(w, 409, "invalid_auth_config", "test integration does not belong to this instance")
					return
				}
				baseURL = integration.BaseURL
			}
			result, testErr := a.Auth.TestConfiguration(r.Context(), item, template, request.TestCredentials, request.VerifyAPI, baseURL)
			if testErr != nil {
				status := http.StatusBadGateway
				if strings.Contains(testErr.Error(), "credential field") || strings.Contains(testErr.Error(), "requires browser authorization") {
					status = http.StatusBadRequest
				}
				if errors.Is(testErr, context.DeadlineExceeded) {
					status = http.StatusGatewayTimeout
				}
				writeJSON(w, status, map[string]any{"code": "auth_test_failed", "message": testErr.Error(), "details": result})
				return
			}
			writeJSON(w, http.StatusOK, result)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"configured": true, "message": "当前配置结构有效；尚未请求认证接口，真实凭据需通过连接账号验证"})
		return
	}
	item, err = a.Store.SaveAuthInstance(r.Context(), item)
	if err != nil {
		a.writeStoreError(w, r, err, "save auth instance")
		return
	}
	a.audit(r, "auth_instance.saved", "auth_instance", item.ID, item.Name, nil, item)
	writeJSON(w, statusForSave(r), item)
}

func (a *api) testAuthInstance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	item, err := a.Store.AuthInstance(r.Context(), id)
	if err != nil {
		a.writeStoreError(w, r, err, "get auth instance")
		return
	}
	if !authn.SupportsTemplate(item.AuthTemplateKey, item.AuthTemplateFlow) {
		_ = a.Store.SetAuthInstanceTestResult(r.Context(), id, "unsupported")
		writeAdminError(w, http.StatusNotImplemented, "auth_extension_required", "authentication flow requires a reviewed Go extension or enterprise gateway")
		return
	}
	if err := authn.ValidateInstanceConfiguration(item); err != nil {
		_ = a.Store.SetAuthInstanceTestResult(r.Context(), id, "failed")
		writeAdminError(w, http.StatusBadRequest, "invalid_auth_config", err.Error())
		return
	}
	if err := a.Auth.ValidateInstanceSecrets(item); err != nil {
		_ = a.Store.SetAuthInstanceTestResult(r.Context(), id, "failed")
		writeAdminError(w, http.StatusBadRequest, "invalid_auth_config", err.Error())
		return
	}
	for _, rawURL := range []string{item.TokenURL, item.RefreshURL} {
		if rawURL == "" {
			continue
		}
		parsed, parseErr := url.Parse(rawURL)
		if parseErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			_ = a.Store.SetAuthInstanceTestResult(r.Context(), id, "failed")
			writeAdminError(w, http.StatusBadRequest, "invalid_auth_url", "Authentication URLs must be absolute http(s) URLs")
			return
		}
	}
	if (item.AuthTemplateFlow == "password_token" || item.AuthTemplateFlow == "client_credentials") && (item.TokenURL == "" || item.TokenPath == "") {
		_ = a.Store.SetAuthInstanceTestResult(r.Context(), id, "failed")
		writeAdminError(w, http.StatusBadRequest, "invalid_auth_config", "token URL and token JSON path are required")
		return
	}
	if item.AuthTemplateFlow == "oauth2_code" {
		var publicConfig struct {
			AuthorizationURL string `json:"authorizationUrl"`
		}
		_ = jsonutil.Unmarshal(item.PublicConfig, &publicConfig)
		if !validAbsoluteURL(publicConfig.AuthorizationURL) || item.TokenURL == "" {
			_ = a.Store.SetAuthInstanceTestResult(r.Context(), id, "failed")
			writeAdminError(w, http.StatusBadRequest, "invalid_oauth_config", "OAuth authorization URL and token URL are required")
			return
		}
		secrets, secretErr := a.Auth.AuthInstanceSecrets(item)
		if secretErr != nil || firstMapString(secrets, "clientId", "client_id") == "" {
			_ = a.Store.SetAuthInstanceTestResult(r.Context(), id, "failed")
			writeAdminError(w, http.StatusBadRequest, "invalid_oauth_config", "OAuth client ID is required")
			return
		}
	}
	if err := a.Store.SetAuthInstanceTestResult(r.Context(), id, "configured"); err != nil {
		a.writeStoreError(w, r, err, "test auth instance")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"configured": true, "message": "配置结构有效；请创建连接账号验证真实凭据"})
}

func (a *api) listIntegrations(w http.ResponseWriter, r *http.Request) {
	items, err := a.Store.ListIntegrations(r.Context())
	writeStoreResult(w, items, err, "list integrations")
}

func (a *api) saveIntegration(w http.ResponseWriter, r *http.Request) {
	var request struct {
		IntegrationKey string          `json:"integrationKey"`
		Name           string          `json:"name"`
		SystemID       string          `json:"systemId"`
		AuthInstanceID string          `json:"authInstanceId"`
		BaseURL        string          `json:"baseUrl"`
		Status         string          `json:"status"`
		Settings       json.RawMessage `json:"settings"`
		Version        int64           `json:"version"`
	}
	if !decodeJSON(w, r, &request, false) {
		return
	}
	request.IntegrationKey = strings.ToLower(clean(request.IntegrationKey, 100))
	request.BaseURL = strings.TrimSpace(request.BaseURL)
	baseURL, err := url.Parse(strings.TrimSpace(request.BaseURL))
	if !validIdentifier(request.IntegrationKey) || strings.TrimSpace(request.Name) == "" || request.SystemID == "" || request.AuthInstanceID == "" ||
		err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" || baseURL.User != nil || baseURL.Fragment != "" || baseURL.RawQuery != "" ||
		!validStatus(defaultStatus(request.Status, "draft"), "ready", "draft", "disabled") {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "integrationKey, name, systemId, authInstanceId and an absolute http(s) baseUrl are required")
		return
	}
	if baseURL.Path != "" {
		if err := executor.ValidatePath(baseURL.EscapedPath()); err != nil {
			writeAdminError(w, 400, "invalid_input", "Base URL contains an unsafe path prefix")
			return
		}
	}
	if chi.URLParam(r, "id") != "" && request.Version < 1 {
		writeAdminError(w, 409, "version_required", "Refresh the integration before saving")
		return
	}
	item, err := a.Store.SaveIntegration(r.Context(), model.Integration{
		ID: chi.URLParam(r, "id"), IntegrationKey: request.IntegrationKey,
		Name: clean(request.Name, 160), SystemID: request.SystemID,
		AuthInstanceID: request.AuthInstanceID, BaseURL: request.BaseURL,
		Status: defaultStatus(request.Status, "draft"), Settings: request.Settings, Version: request.Version,
	})
	if err != nil {
		a.writeStoreError(w, r, err, "save integration")
		return
	}
	a.audit(r, "integration.saved", "integration", item.ID, item.Name, nil, item)
	writeJSON(w, statusForSave(r), item)
}

func (a *api) listConnections(w http.ResponseWriter, r *http.Request) {
	o, ok := listOptions(w, r)
	if !ok {
		return
	}
	items, err := a.Store.ListConnections(r.Context(), o)
	if len(items) > 0 {
		last := items[len(items)-1]
		nextCursor(w, len(items), o.Limit, last.UpdatedAt, last.ID)
	}
	writeStoreResult(w, items, err, "list connections")
}

func (a *api) saveConnection(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Revision      int64           `json:"revision"`
		ConnectionKey string          `json:"connectionKey"`
		Name          string          `json:"name"`
		IntegrationID string          `json:"integrationId"`
		EndUserKey    string          `json:"endUserKey"`
		EndUserName   string          `json:"endUserName"`
		EndUserEmail  string          `json:"endUserEmail"`
		Metadata      json.RawMessage `json:"metadata"`
		Credentials   map[string]any  `json:"credentials"`
		Tags          []string        `json:"tags"`
	}
	if !decodeJSON(w, r, &request, false) {
		return
	}
	request.ConnectionKey = strings.ToLower(clean(request.ConnectionKey, 100))
	request.EndUserKey = clean(request.EndUserKey, 255)
	if !validIdentifier(request.ConnectionKey) || strings.TrimSpace(request.Name) == "" || request.IntegrationID == "" || request.EndUserKey == "" {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "connectionKey, name, integrationId and endUserKey are required")
		return
	}
	integration, err := a.Store.Integration(r.Context(), request.IntegrationID)
	if err != nil {
		a.writeStoreError(w, r, err, "get connection integration")
		return
	}
	if integration.Status != "ready" {
		writeAdminError(w, http.StatusConflict, "integration_not_ready", "connection integration must be ready")
		return
	}
	authInstance, err := a.Store.AuthInstance(r.Context(), integration.AuthInstanceID)
	if err != nil {
		a.writeStoreError(w, r, err, "get connection authentication instance")
		return
	}
	if authInstance.Status != "ready" {
		writeAdminError(w, http.StatusConflict, "auth_instance_not_ready", "connection authentication instance must be ready")
		return
	}
	id := chi.URLParam(r, "id")
	revision := int64(1)
	credentials := request.Credentials
	if id == "" {
		id = newID("connection")
	} else if current, currentErr := a.Store.Connection(r.Context(), id); currentErr == nil {
		if request.Revision > 0 && request.Revision != current.Revision {
			writeAdminError(w, 409, "conflict", "连接已被其他成员修改，请刷新后重试")
			return
		}
		if current.IntegrationID != integration.ID || current.AuthInstanceID != integration.AuthInstanceID {
			writeAdminError(w, 409, "conflict", "连接绑定的集成或认证实例已改变，请创建替代连接")
			return
		}
		revision = current.Revision + 1
		if len(credentials) == 0 {
			credentials, err = a.Auth.OpenCredentials(current)
			if err != nil {
				writeAdminError(w, http.StatusInternalServerError, "secret_decryption_failed", "Could not preserve existing connection credentials")
				return
			}
		}
	}
	accountTemplate, loadErr := a.Store.AuthTemplate(r.Context(), authInstance.AuthTemplateID)
	if loadErr != nil {
		a.writeStoreError(w, r, loadErr, "load credential schema")
		return
	}
	if accountTemplate.Source == "custom" {
		values, _, credentialErr := a.Auth.CredentialValues(authInstance, credentials)
		if credentialErr != nil {
			writeAdminError(w, 400, "invalid_auth_config", credentialErr.Error())
			return
		}
		if err := authn.ValidateCredentials(accountTemplate, values); err != nil {
			writeAdminError(w, 400, "invalid_credentials", err.Error())
			return
		}
	}
	credentialBlob, err := a.Auth.SealCredentials(id, revision, credentials)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "secret_encryption_failed", "Could not encrypt connection credentials")
		return
	}
	item, err := a.Store.SaveConnection(r.Context(), store.SaveConnectionInput{
		Connection: model.Connection{
			ID: id, ConnectionKey: request.ConnectionKey, Name: clean(request.Name, 160),
			IntegrationID: integration.ID, AuthInstanceID: integration.AuthInstanceID,
			Status: "pending", CredentialBlob: credentialBlob, KeyVersion: a.Codec.Version(), Tags: cleanTags(request.Tags),
		},
		EndUser: model.EndUser{
			ExternalKey: request.EndUserKey, DisplayName: clean(request.EndUserName, 160),
			Email: clean(request.EndUserEmail, 320), Metadata: request.Metadata,
		},
		ExpectedRevision: revision - 1,
	})
	if err != nil {
		a.writeStoreError(w, r, err, "save connection")
		return
	}
	a.audit(r, "connection.saved", "connection", item.ID, item.Name, nil, item)
	writeJSON(w, statusForSave(r), item)
}

func (a *api) setConnectionEnabled(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Enabled  bool  `json:"enabled"`
		Revision int64 `json:"revision"`
	}
	if !decodeJSON(w, r, &request, false) {
		return
	}
	item, err := a.Store.SetConnectionEnabled(r.Context(), chi.URLParam(r, "id"), request.Revision, request.Enabled)
	if err != nil {
		a.writeStoreError(w, r, err, "set account enabled")
		return
	}
	a.audit(r, "connection.enabled", "connection", item.ID, item.Name, nil, map[string]any{"enabled": item.Enabled})
	writeJSON(w, 200, item)
}

func (a *api) verifyConnection(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ActionID string         `json:"actionId"`
		Input    map[string]any `json:"input"`
	}
	if r.ContentLength != 0 && !decodeJSON(w, r, &request, false) {
		return
	}
	if request.ActionID == "" {
		item, err := a.Auth.Verify(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			writeAdminError(w, 502, "connection_verification_failed", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"verified": item.LastVerifiedAt != nil, "connection": item})
		return
	}
	c, err := a.Store.Connection(r.Context(), chi.URLParam(r, "id"))
	if err == nil && !c.Enabled {
		writeAdminError(w, 409, "account_disabled", "Enable this account before verification")
		return
	}
	if err != nil {
		a.writeStoreError(w, r, err, "verify account")
		return
	}
	i, err := a.Store.Integration(r.Context(), c.IntegrationID)
	if err != nil {
		a.writeStoreError(w, r, err, "verify account")
		return
	}
	action, err := a.Store.Action(r.Context(), request.ActionID)
	if err != nil || action.SystemID != i.SystemID || action.Status != "active" {
		writeAdminError(w, 400, "invalid_verification_api", "Choose an enabled API from this system")
		return
	}
	catalogAction := definitionAction(action)
	if request.Input == nil {
		_ = jsonutil.Unmarshal(action.ExampleInput, &request.Input)
	}
	if err := a.Catalog.ValidateInput(catalogAction, request.Input); err != nil {
		writeAdminError(w, 400, "invalid_input", err.Error())
		return
	}
	resolved, err := a.Auth.ResolveConnection(r.Context(), c)
	if err != nil {
		writeAdminError(w, 502, "authentication_failed", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), executor.Budget(action.ExecutionConfig))
	defer cancel()
	result, err := a.Executor.Action(ctx, model.Provider{BaseURL: i.BaseURL}, catalogAction, request.Input, nil, resolved.RequestAuth())
	if err == nil {
		err = executor.CheckResponse(catalogAction.Runtime, result.Status, result.Body, catalogAction.OutputSchema)
	}
	if err != nil {
		writeAdminError(w, 502, "connection_verification_failed", err.Error())
		return
	}
	item, err := a.Store.MarkConnectionVerifiedVersion(r.Context(), c.ID, resolved.Connection.Revision, i.TargetVersion)
	if err != nil {
		a.writeStoreError(w, r, err, "verify account")
		return
	}
	writeJSON(w, 200, map[string]any{"verified": true, "connection": item})
}

func (a *api) listActions(w http.ResponseWriter, r *http.Request) {
	o, ok := listOptions(w, r)
	if !ok {
		return
	}
	items, err := a.Store.ListActions(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("system"), o)
	if len(items) > 0 {
		last := items[len(items)-1]
		nextCursor(w, len(items), o.Limit, last.UpdatedAt, last.ID)
	}
	writeStoreResult(w, items, err, "list actions")
}

func (a *api) getAction(w http.ResponseWriter, r *http.Request) {
	item, err := a.Store.Action(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.writeStoreError(w, r, err, "get action")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *api) saveAction(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ActionKey       string          `json:"actionKey"`
		Name            string          `json:"name"`
		Description     string          `json:"description"`
		SystemID        string          `json:"systemId"`
		Version         int64           `json:"version"`
		RequestConfig   json.RawMessage `json:"requestConfig"`
		ResponseConfig  json.RawMessage `json:"responseConfig"`
		ExecutionConfig json.RawMessage `json:"executionConfig"`
		HTTPMethod      string          `json:"httpMethod"`
		RelativePath    string          `json:"relativePath"`
		RequiredScopes  []string        `json:"requiredScopes"`
		InputSchema     json.RawMessage `json:"inputSchema"`
		OutputSchema    json.RawMessage `json:"outputSchema"`
		ExampleInput    json.RawMessage `json:"exampleInput"`
		Status          string          `json:"status"`
	}
	if !decodeJSON(w, r, &request, false) {
		return
	}
	request.ActionKey = strings.ToLower(clean(request.ActionKey, 180))
	request.HTTPMethod = strings.ToUpper(request.HTTPMethod)
	request.Status = defaultStatus(request.Status, "active")
	if !validIdentifier(request.ActionKey) || strings.TrimSpace(request.Name) == "" || request.SystemID == "" ||
		!validMethod(request.HTTPMethod) || !validRelativePath(request.RelativePath) ||
		!validStatus(request.Status, "active", "draft", "disabled") {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "actionKey, supported httpMethod and a safe relativePath are required")
		return
	}
	if chi.URLParam(r, "id") != "" && request.Version < 1 {
		writeAdminError(w, 409, "version_required", "Refresh the API before editing")
		return
	}
	runtime := &model.HTTPActionRuntime{Method: request.HTTPMethod, Path: request.RelativePath, RequestConfig: request.RequestConfig, ResponseConfig: request.ResponseConfig, ExecutionConfig: request.ExecutionConfig}
	if err := executor.ValidateConfig(runtime); err != nil {
		writeConfigurationError(w, "requestConfig", err)
		return
	}
	for field, raw := range map[string]json.RawMessage{"inputSchema": request.InputSchema, "outputSchema": request.OutputSchema} {
		if err := executor.ValidateSchema(raw); err != nil {
			writeConfigurationError(w, field, err)
			return
		}
	}
	if err := executor.ValidateInputContract(runtime, request.InputSchema); err != nil {
		writeConfigurationError(w, "inputSchema", err)
		return
	}
	item, err := a.Store.SaveAction(r.Context(), model.ActionDefinition{
		ID: chi.URLParam(r, "id"), ActionKey: request.ActionKey, Name: clean(request.Name, 180),
		Description: clean(request.Description, 1000), SystemID: request.SystemID,
		RequestConfig: request.RequestConfig, ResponseConfig: request.ResponseConfig, ExecutionConfig: request.ExecutionConfig, Version: request.Version, HTTPMethod: request.HTTPMethod,
		RelativePath: request.RelativePath, RequiredScopes: cleanPatterns(request.RequiredScopes),
		InputSchema: request.InputSchema, OutputSchema: request.OutputSchema,
		ExampleInput: request.ExampleInput, Status: request.Status,
	})
	if err != nil {
		a.writeStoreError(w, r, err, "save action")
		return
	}
	a.audit(r, "action.saved", "action", item.ID, item.ActionKey, nil, item)
	writeJSON(w, statusForSave(r), item)
}

func (a *api) testAction(w http.ResponseWriter, r *http.Request) {
	var request actionRequest
	if !decodeJSON(w, r, &request, true) {
		return
	}
	status, data := a.executeActionForAdmin(r, chi.URLParam(r, "id"), request)
	writeJSON(w, status, data)
}

func (a *api) listRuntimeTokens(w http.ResponseWriter, r *http.Request) {
	items, err := a.Store.ListRuntimeTokens(r.Context())
	writeStoreResult(w, items, err, "list runtime tokens")
}

func (a *api) createRuntimeToken(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name               string     `json:"name"`
		AllowedActions     []string   `json:"allowedActions"`
		BlockedActions     []string   `json:"blockedActions"`
		AllowedConnections []string   `json:"allowedConnections"`
		ExpiresAt          *time.Time `json:"expiresAt"`
	}
	if !decodeJSON(w, r, &request, false) {
		return
	}
	request.Name = clean(request.Name, 160)
	if request.Name == "" {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "name is required")
		return
	}
	for _, values := range [][]string{request.AllowedActions, request.BlockedActions, request.AllowedConnections} {
		for _, value := range values {
			if strings.TrimSpace(value) == "" || len(value) > 255 {
				writeAdminError(w, http.StatusBadRequest, "invalid_input", "policy values must be non-empty and at most 255 characters")
				return
			}
		}
	}
	for _, pattern := range append(append([]string{}, request.AllowedActions...), request.BlockedActions...) {
		if _, err := path.Match(pattern, "validation.action"); err != nil {
			writeAdminError(w, http.StatusBadRequest, "invalid_input", "action policy contains an invalid glob pattern")
			return
		}
	}
	plain := newToken()
	token := model.RuntimeToken{
		ID: newID("runtime-token"), Name: request.Name, TokenPrefix: plain[:min(18, len(plain))],
		TokenHash: tokenHash(plain), Status: "active", AllowedActions: nonNilStrings(request.AllowedActions),
		BlockedActions: nonNilStrings(request.BlockedActions), AllowedConnections: nonNilStrings(request.AllowedConnections),
		ExpiresAt: request.ExpiresAt, CreatedBy: currentAdmin(r).UserID, CreatedAt: now(),
	}
	if err := a.Store.CreateRuntimeToken(r.Context(), token); err != nil {
		a.writeStoreError(w, r, err, "create runtime token")
		return
	}
	a.audit(r, "runtime_token.created", "runtime_token", token.ID, token.Name, nil, token)
	writeJSON(w, http.StatusCreated, map[string]any{"token": plain, "runtimeToken": token})
}

func (a *api) revokeRuntimeToken(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.RevokeRuntimeToken(r.Context(), id); err != nil {
		a.writeStoreError(w, r, err, "revoke runtime token")
		return
	}
	a.audit(r, "runtime_token.revoked", "runtime_token", id, id, nil, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) listOperations(w http.ResponseWriter, r *http.Request) {
	o, ok := listOptions(w, r)
	if !ok {
		return
	}
	items, err := a.Store.ListOperations(r.Context(), r.URL.Query().Get("kind"), r.URL.Query().Get("status"),
		r.URL.Query().Get("q"), o.Limit, o)
	if len(items) > 0 {
		last := items[len(items)-1]
		nextCursor(w, len(items), o.Limit, last.StartedAt, last.ID)
	}
	writeStoreResult(w, items, err, "list operations")
}

func (a *api) getOperation(w http.ResponseWriter, r *http.Request) {
	item, err := a.Store.Operation(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.writeStoreError(w, r, err, "get operation")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *api) metrics(w http.ResponseWriter, r *http.Request) {
	hours := 24
	if value := r.URL.Query().Get("hours"); value == "168" || value == "720" {
		fmt.Sscan(value, &hours)
	}
	item, err := a.Store.Metrics(r.Context(), time.Now().UTC().Add(-time.Duration(hours)*time.Hour))
	writeStoreResult(w, item, err, "read metrics")
}

func (a *api) writeStoreError(w http.ResponseWriter, r *http.Request, err error, operation string) {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == "23505" && pgError.ConstraintName == "actions_system_key_uq" {
		writeAdminError(w, 409, "api_key_exists", "此系统中已存在相同 API 标识，请更换标识")
		return
	}
	if errors.As(err, &pgError) && pgError.Code == "23505" && pgError.ConstraintName == "connections_integration_name_uq" {
		writeAdminError(w, http.StatusConflict, "connection_name_exists", "该集成中已存在同名账号，请更换账号名称，或打开已有账号重试验证")
		return
	}
	status, code := statusForStoreError(err)
	if status == http.StatusInternalServerError {
		a.Logger.ErrorContext(r.Context(), operation, "error", err)
	}
	writeAdminError(w, status, code, err.Error())
}

func writeStoreResult(w http.ResponseWriter, value any, err error, _ string) {
	if err != nil {
		status, code := statusForStoreError(err)
		writeAdminError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (a *api) audit(r *http.Request, action, resourceType, resourceID, label string, before, after any) {
	identity := currentAdmin(r)
	item := model.AuditLog{
		ID: newID("audit"), RequestID: requestID(r), ActorType: "user",
		ActorID: identity.UserID, ActorLabel: identity.Email, Action: action,
		ResourceType: resourceType, ResourceID: resourceID, ResourceLabel: label,
		IPAddress: remoteIP(r), BeforeData: store.MarshalAuditValue(before),
		AfterData: store.MarshalAuditValue(after), Metadata: json.RawMessage(`{}`),
	}
	if err := a.Store.WriteAudit(r.Context(), item); err != nil {
		a.Logger.ErrorContext(r.Context(), "write audit", "action", action, "error", err)
		a.Store.AbortTransaction(r.Context(), fmt.Errorf("write audit: %w", err))
	}
}

func statusForSave(r *http.Request) int {
	if r.Method == http.MethodPost {
		return http.StatusCreated
	}
	return http.StatusOK
}

func defaultStatus(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func validStatus(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func cleanTags(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]bool)
	for _, value := range values {
		value = clean(value, 80)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
		if len(result) == 20 {
			break
		}
	}
	return result
}

func validMethod(value string) bool {
	switch value {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func validRelativePath(value string) bool {
	return executor.ValidatePath(value) == nil
}

func remoteIP(r *http.Request) string {
	value := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(value); err == nil {
		return host
	}
	return strings.Trim(value, "[]")
}

func isStoreConflict(err error) bool {
	return errors.Is(err, store.ErrConflict) || store.IsUniqueViolation(err)
}

func writeAuthConfigurationError(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusBadRequest, map[string]any{"code": "invalid_auth_config", "message": message, "details": authn.TestResult{Configured: false, Steps: []authn.TestStep{{Name: "configuration", Status: "failed"}, {Name: "token_request", Status: "skipped"}, {Name: "token_response", Status: "skipped"}, {Name: "api_verification", Status: "skipped"}}}})
}
