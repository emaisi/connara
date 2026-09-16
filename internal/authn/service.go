package authn

import (
	"apihub-go/internal/executor"
	"apihub-go/internal/jsonutil"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"apihub-go/internal/model"
	"apihub-go/internal/rediscache"
	"apihub-go/internal/secret"
	"apihub-go/internal/store"
)

const maxTokenResponse = 1 << 20

var ErrReauthorization = errors.New("OAuth reauthorization is required")

type Service struct {
	store  *store.Store
	codec  *secret.Codec
	cache  *rediscache.Cache
	client *http.Client
}

type Resolved struct {
	Connection  model.Connection
	Credentials map[string]any
	HeaderName  string
	HeaderValue string
	Headers     map[string]string
	Query       map[string]string
	Cookies     map[string]string
}

type OAuthTokens struct {
	Credentials map[string]any
	ExpiresAt   *time.Time
}

func New(database *store.Store, codec *secret.Codec, cache *rediscache.Cache, client *http.Client) *Service {
	return &Service{store: database, codec: codec, cache: cache, client: client}
}

func (s *Service) SealCredentials(connectionID string, revision int64, credentials map[string]any) ([]byte, error) {
	plain, err := json.Marshal(credentials)
	if err != nil {
		return nil, fmt.Errorf("encode credentials: %w", err)
	}
	return s.codec.Encrypt(plain, credentialAAD(s.store.WorkspaceID(), connectionID, revision))
}

func (s *Service) OpenCredentials(connection model.Connection) (map[string]any, error) {
	plain, err := s.codec.Decrypt(connection.CredentialBlob, credentialAAD(connection.WorkspaceID, connection.ID, connection.Revision))
	if err != nil {
		return nil, err
	}
	var values map[string]any
	if err := jsonutil.Unmarshal(plain, &values); err != nil {
		return nil, fmt.Errorf("decode credentials: %w", err)
	}
	return values, nil
}

func (s *Service) Resolve(ctx context.Context, connectionID string) (Resolved, error) {
	connection, err := s.store.Connection(ctx, connectionID)
	if err != nil {
		return Resolved{}, err
	}
	return s.ResolveConnection(ctx, connection)
}

func (s *Service) ResolveConnection(ctx context.Context, connection model.Connection) (Resolved, error) {
	credentials, err := s.OpenCredentials(connection)
	if err != nil {
		return Resolved{}, err
	}
	instance, err := s.store.AuthInstance(ctx, connection.AuthInstanceID)
	if err != nil {
		return Resolved{}, err
	}
	if instance.Status != "ready" {
		return Resolved{}, fmt.Errorf("authentication instance is not ready")
	}
	template, err := s.store.AuthTemplate(ctx, instance.AuthTemplateID)
	if err != nil {
		return Resolved{}, err
	}
	if template.Status != "published" {
		return Resolved{}, fmt.Errorf("authentication template is not published")
	}
	if !SupportsFlow(instance.AuthTemplateFlow) {
		return Resolved{}, fmt.Errorf("authentication flow %q requires a reviewed Go extension or enterprise gateway", instance.AuthTemplateFlow)
	}
	if needsRefresh(connection, instance.AuthTemplateFlow, credentials) {
		connection, credentials, err = s.refresh(ctx, connection, instance, credentials)
		if err != nil {
			if errors.Is(err, ErrReauthorization) {
				s.store.MarkConnectionError(ctx, connection.ID, "reauthorization_required", err.Error())
			}
			return Resolved{}, err
		}
	}
	if instance.AuthTemplateKey == "basic" && firstString(credentials, "basic_token") == "" {
		username := firstString(credentials, "username")
		password := firstString(credentials, "password")
		if username != "" || password != "" {
			credentials["basic_token"] = base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
		}
	}
	headerValue, err := renderTemplate(instance.HeaderValueTemplate, credentials)
	if err != nil {
		return Resolved{}, err
	}
	resolved := Resolved{
		Connection:  connection,
		Credentials: credentials,
		HeaderName:  instance.HeaderName,
		HeaderValue: headerValue,
		Headers:     map[string]string{},
		Query:       map[string]string{},
		Cookies:     map[string]string{},
	}
	if instance.HeaderName != "" {
		resolved.Headers[instance.HeaderName] = headerValue
	}
	var rules []struct {
		Target   string `json:"target"`
		Name     string `json:"name"`
		Template string `json:"template"`
	}
	if len(template.InjectionRules) > 0 && jsonutil.Unmarshal(template.InjectionRules, &rules) != nil {
		return Resolved{}, errors.New("authentication injection rules are invalid")
	}
	for _, rule := range rules {
		value, renderErr := renderTemplate(rule.Template, credentials)
		if renderErr != nil {
			return Resolved{}, renderErr
		}
		switch rule.Target {
		case "header":
			resolved.Headers[rule.Name] = value
		case "query":
			resolved.Query[rule.Name] = value
		case "cookie":
			resolved.Cookies[rule.Name] = value
		default:
			return Resolved{}, fmt.Errorf("unsupported credential placement %q", rule.Target)
		}
	}
	return resolved, nil
}

func (s *Service) Verify(ctx context.Context, connectionID string) (model.Connection, error) {
	resolved, err := s.Resolve(ctx, connectionID)
	if err != nil {
		return model.Connection{}, err
	}
	instance, err := s.store.AuthInstance(ctx, resolved.Connection.AuthInstanceID)
	if err != nil {
		return model.Connection{}, err
	}
	var config struct {
		VerificationPath string `json:"verificationPath"`
	}
	if err := jsonutil.Unmarshal(instance.PublicConfig, &config); err != nil {
		return model.Connection{}, err
	}
	if config.VerificationPath == "" {
		return s.store.MarkConnectionConfigured(ctx, connectionID)
	}
	if err := executor.ValidatePath(config.VerificationPath); err != nil {
		return model.Connection{}, err
	}
	integration, err := s.store.Integration(ctx, resolved.Connection.IntegrationID)
	if err != nil {
		return model.Connection{}, err
	}
	result, err := executor.New(s.client).Action(ctx, model.Provider{BaseURL: integration.BaseURL}, model.Action{Runtime: &model.HTTPActionRuntime{Method: "GET", Path: config.VerificationPath}}, map[string]any{}, nil, executor.RequestAuth{Headers: resolved.Headers, Query: resolved.Query, Cookies: resolved.Cookies})
	if err != nil {
		return model.Connection{}, err
	}
	if result.Status < 200 || result.Status >= 300 {
		return model.Connection{}, fmt.Errorf("verification endpoint returned HTTP %d", result.Status)
	}
	return s.store.MarkConnectionVerified(ctx, connectionID)
}

func (s *Service) AuthInstanceSecrets(instance model.AuthInstance) (map[string]any, error) {
	if len(instance.SecretBlob) == 0 {
		return map[string]any{}, nil
	}
	plain, err := s.codec.Decrypt(instance.SecretBlob, []byte(s.store.WorkspaceID()+":auth-instance:"+instance.ID))
	if err != nil {
		return nil, err
	}
	values := map[string]any{}
	if err := jsonutil.Unmarshal(plain, &values); err != nil {
		return nil, fmt.Errorf("decode authentication secrets: %w", err)
	}
	return values, nil
}

func (s *Service) ExchangeOAuthCode(ctx context.Context, instance model.AuthInstance, code, redirectURI, verifier string) (OAuthTokens, error) {
	if instance.TokenURL == "" {
		return OAuthTokens{}, errors.New("OAuth authentication instance has no token URL")
	}
	secrets, err := s.AuthInstanceSecrets(instance)
	if err != nil {
		return OAuthTokens{}, err
	}
	clientID := firstString(secrets, "clientId", "client_id")
	clientSecret := firstString(secrets, "clientSecret", "client_secret")
	if clientID == "" {
		return OAuthTokens{}, errors.New("OAuth clientId is missing")
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}, "client_id": {clientID}}
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}
	if verifier != "" {
		form.Set("code_verifier", verifier)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, instance.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return OAuthTokens{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("User-Agent", "apihub-go/0.2")
	response, err := s.client.Do(request)
	if err != nil {
		return OAuthTokens{}, fmt.Errorf("OAuth token request failed: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxTokenResponse+1))
	if err != nil || len(body) > maxTokenResponse {
		return OAuthTokens{}, errors.New("OAuth token response is invalid or too large")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return OAuthTokens{}, fmt.Errorf("OAuth token endpoint returned HTTP %d", response.StatusCode)
	}
	credentials := map[string]any{}
	if err := jsonutil.Unmarshal(body, &credentials); err != nil {
		return OAuthTokens{}, errors.New("OAuth token endpoint did not return a JSON object")
	}
	path := instance.TokenPath
	if path == "" {
		path = "$.access_token"
	}
	value, ok := lookupJSONPath(credentials, path)
	if !ok || strings.TrimSpace(fmt.Sprint(value)) == "" {
		return OAuthTokens{}, fmt.Errorf("OAuth access token path %q was not found", path)
	}
	credentials["token"] = fmt.Sprint(value)
	credentials["access_token"] = fmt.Sprint(value)
	var expiresAt *time.Time
	expiryValue := credentials["expires_in"]
	if instance.ExpiryPath != "" {
		if value, ok := lookupJSONPath(credentials, instance.ExpiryPath); ok {
			expiryValue = value
		}
	}
	if seconds, parseErr := strconv.ParseInt(fmt.Sprint(expiryValue), 10, 64); parseErr == nil && seconds > 0 {
		value := time.Now().UTC().Add(time.Duration(seconds) * time.Second)
		expiresAt = &value
	}
	return OAuthTokens{Credentials: credentials, ExpiresAt: expiresAt}, nil
}

func (s *Service) refresh(ctx context.Context, connection model.Connection, instance model.AuthInstance, credentials map[string]any) (model.Connection, map[string]any, error) {

	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	var unlock func(context.Context) error
	for {
		release, acquired, err := s.cache.AcquireRefreshLock(ctx, connection.ID, 60*time.Second)
		if err != nil {
			return connection, nil, fmt.Errorf("acquire refresh lock: %w", err)
		}
		if acquired {
			unlock = release
			break
		}
		select {
		case <-ctx.Done():
			return connection, nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
		current, err := s.store.Connection(ctx, connection.ID)
		if err != nil {
			return connection, nil, err
		}
		if current.Revision != connection.Revision {
			values, err := s.OpenCredentials(current)
			return current, values, err
		}
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = unlock(cleanup)
	}()

	current, err := s.store.Connection(ctx, connection.ID)
	if err != nil {
		return connection, nil, err
	}
	if current.Revision != connection.Revision {
		values, openErr := s.OpenCredentials(current)
		return current, values, openErr
	}
	tokenURL := instance.TokenURL
	if instance.AuthTemplateFlow == "oauth2_code" && instance.RefreshURL != "" {
		tokenURL = instance.RefreshURL
	}
	if tokenURL == "" {
		return connection, nil, errors.New("authentication instance has no token URL")
	}
	template, err := s.store.AuthTemplate(ctx, instance.AuthTemplateID)
	if err != nil {
		return connection, nil, err
	}
	var requestConfig struct {
		Method   string `json:"method"`
		BodyType string `json:"bodyType"`
	}
	_ = jsonutil.Unmarshal(template.TokenRequest, &requestConfig)
	method := strings.ToUpper(requestConfig.Method)
	if method == "" {
		method = http.MethodPost
	}
	var body io.Reader
	contentType := "application/json"
	if instance.AuthTemplateFlow == "oauth2_code" {
		refreshToken := firstString(credentials, "refresh_token", "refreshToken")
		if refreshToken == "" {
			return connection, nil, fmt.Errorf("%w: refresh token missing", ErrReauthorization)
		}
		secrets, secretErr := s.AuthInstanceSecrets(instance)
		if secretErr != nil {
			return connection, nil, secretErr
		}
		form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}}
		if clientID := firstString(secrets, "clientId", "client_id"); clientID != "" {
			form.Set("client_id", clientID)
		}
		if clientSecret := firstString(secrets, "clientSecret", "client_secret"); clientSecret != "" {
			form.Set("client_secret", clientSecret)
		}
		body = strings.NewReader(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	} else if requestConfig.BodyType == "form" {
		form := url.Values{}
		if instance.AuthTemplateFlow == "client_credentials" {
			form.Set("grant_type", "client_credentials")
		}
		for key, value := range credentials {
			if isRuntimeCredential(key) {
				continue
			}
			form.Set(key, fmt.Sprint(value))
		}
		body = strings.NewReader(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	} else {
		payload := make(map[string]any)
		for key, value := range credentials {
			if !isRuntimeCredential(key) {
				payload[key] = value
			}
		}
		encoded, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return connection, nil, marshalErr
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, tokenURL, body)
	if err != nil {
		return connection, nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("User-Agent", "apihub-go/0.2")
	response, err := s.client.Do(request)
	if err != nil {
		return connection, nil, fmt.Errorf("token request failed: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxTokenResponse+1))
	if err != nil {
		return connection, nil, fmt.Errorf("read token response: %w", err)
	}
	if len(responseBody) > maxTokenResponse {
		return connection, nil, errors.New("token response exceeds 1 MiB")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		_ = jsonutil.Unmarshal(responseBody, &failure)
		if failure.Error == "invalid_grant" {
			return connection, nil, ErrReauthorization
		}
		return connection, nil, fmt.Errorf("token endpoint returned HTTP %d", response.StatusCode)
	}
	var responseValue any
	if err := jsonutil.Unmarshal(responseBody, &responseValue); err != nil {
		return connection, nil, errors.New("token endpoint did not return JSON")
	}
	token, ok := lookupJSONPath(responseValue, instance.TokenPath)
	if !ok || strings.TrimSpace(fmt.Sprint(token)) == "" {
		return connection, nil, fmt.Errorf("token path %q was not found", instance.TokenPath)
	}
	credentials["token"] = fmt.Sprint(token)
	credentials["access_token"] = fmt.Sprint(token)
	if responseObject, ok := responseValue.(map[string]any); ok {
		if refreshToken := firstString(responseObject, "refresh_token", "refreshToken"); refreshToken != "" {
			credentials["refresh_token"] = refreshToken
		}
	}
	var expiresAt *time.Time
	if expiry, ok := lookupJSONPath(responseValue, instance.ExpiryPath); ok {
		if seconds, parseErr := strconv.ParseInt(fmt.Sprint(expiry), 10, 64); parseErr == nil && seconds > 0 {
			value := time.Now().UTC().Add(time.Duration(seconds) * time.Second)
			expiresAt = &value
		}
	}
	newRevision := connection.Revision + 1
	blob, err := s.SealCredentials(connection.ID, newRevision, credentials)
	if err != nil {
		return connection, nil, err
	}
	persist, cancelPersist := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelPersist()
	updated, err := s.store.UpdateConnectionCredential(persist, connection.ID, connection.Revision, blob, expiresAt, s.codec.Version())
	return updated, credentials, err
}

func needsRefresh(connection model.Connection, flow string, credentials map[string]any) bool {
	if flow != "password_token" && flow != "client_credentials" && flow != "oauth2_code" {
		return false
	}
	if token := firstString(credentials, "token", "access_token", "accessToken"); token == "" {
		return true
	}
	return connection.TokenExpiresAt != nil && connection.TokenExpiresAt.Before(time.Now().UTC().Add(30*time.Second))
}

func SupportsFlow(flow string) bool {
	switch flow {
	case "none", "static", "password_token", "client_credentials", "oauth2_code", "gateway":
		return true
	default:
		return false
	}
}

func renderTemplate(template string, values map[string]any) (string, error) {
	if strings.TrimSpace(template) == "" {
		return "", nil
	}
	var result strings.Builder
	for remaining := template; remaining != ""; {
		start := strings.Index(remaining, "{{")
		if start < 0 {
			result.WriteString(remaining)
			break
		}
		result.WriteString(remaining[:start])
		endOffset := strings.Index(remaining[start+2:], "}}")
		if endOffset < 0 {
			return "", errors.New("invalid credential template")
		}
		end := start + 2 + endOffset
		key := strings.TrimSpace(remaining[start+2 : end])
		value := firstString(values, key)
		if value == "" && key == "token" {
			value = firstString(values, "access_token", "accessToken", "apiKey", "api_key")
		}
		if value == "" {
			return "", fmt.Errorf("credential field %q is required", key)
		}
		result.WriteString(value)
		remaining = remaining[end+2:]
	}
	return result.String(), nil
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key]; ok && value != nil && strings.TrimSpace(fmt.Sprint(value)) != "" {
			return fmt.Sprint(value)
		}
	}
	return ""
}

func lookupJSONPath(value any, path string) (any, bool) {
	path = strings.TrimSpace(strings.TrimPrefix(path, "$"))
	path = strings.TrimPrefix(path, ".")
	if path == "" {
		return value, true
	}
	current := value
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func isRuntimeCredential(key string) bool {
	switch key {
	case "token", "access_token", "accessToken", "refresh_token", "refreshToken", "expires_at":
		return true
	default:
		return false
	}
}

func credentialAAD(workspaceID, connectionID string, revision int64) []byte {
	return []byte(fmt.Sprintf("%s:connection:%s:%d", workspaceID, connectionID, revision))
}
