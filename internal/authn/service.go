package authn

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"apihub-go/internal/buildinfo"
	"apihub-go/internal/executor"
	"apihub-go/internal/jsonutil"
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
	Sign        func(context.Context, *http.Request) error
	TLS         *tls.Config
}

func (r Resolved) RequestAuth() executor.RequestAuth {
	return executor.RequestAuth{Headers: r.Headers, Query: r.Query, Cookies: r.Cookies, Sign: r.Sign, TLS: r.TLS}
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
	if !connection.Enabled {
		return Resolved{}, errors.New("account is disabled")
	}
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
	if !SupportsTemplate(instance.AuthTemplateKey, instance.AuthTemplateFlow) {
		return Resolved{}, fmt.Errorf("authentication flow %q requires a reviewed Go extension or enterprise gateway", instance.AuthTemplateFlow)
	}
	if err := ValidateEffectiveConfiguration(instance, template); err != nil {
		return Resolved{}, err
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
	resolved, err := s.resolveTest(instance, template, credentials)
	if err != nil {
		return Resolved{}, err
	}
	resolved.Connection = connection
	return resolved, nil
}

func (s *Service) Verify(ctx context.Context, connectionID string) (model.Connection, error) {
	connection, err := s.store.Connection(ctx, connectionID)
	if err != nil {
		return model.Connection{}, err
	}
	integration, err := s.store.Integration(ctx, connection.IntegrationID)
	if err != nil {
		return model.Connection{}, err
	}
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
	advanced, err := ParseAuthRequest(instance)
	if err != nil {
		return model.Connection{}, err
	}
	if config.VerificationPath == "" || (advanced.SchemaVersion == 2 && !advanced.Verification.Enabled) {
		return s.store.MarkConnectionConfigured(ctx, connectionID)
	}
	if err := executor.ValidatePath(config.VerificationPath); err != nil {
		return model.Connection{}, err
	}
	if err := s.verifyResolved(ctx, instance, integration.BaseURL, resolved); err != nil {
		return model.Connection{}, err
	}
	return s.store.MarkConnectionVerifiedVersion(ctx, connectionID, resolved.Connection.Revision, integration.TargetVersion)
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
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}}
	authHeader, err := oauthClientAuth(instance, secrets, form, instance.TokenURL)
	if err != nil {
		return OAuthTokens{}, err
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
	request.Header.Set("User-Agent", buildinfo.UserAgent)
	if authHeader != "" {
		request.Header.Set("Authorization", authHeader)
	}
	client, cleanup, err := s.tokenClient(instance, secrets)
	if err != nil {
		return OAuthTokens{}, err
	}
	defer cleanup()
	response, err := client.Do(request)
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
	template, err := s.store.AuthTemplate(ctx, instance.AuthTemplateID)
	if err != nil {
		return connection, nil, err
	}
	if connection.LastErrorCode == "auth_configuration_changed" && instance.AuthTemplateFlow == "password_token" {
		credentials = maps.Clone(credentials)
		delete(credentials, "refresh_token")
	}
	return s.requestInstanceToken(ctx, connection, instance, template, credentials, false)
}

// TestInstanceToken uses the runtime token request without saving test credentials or tokens.
func (s *Service) TestInstanceToken(ctx context.Context, instance model.AuthInstance, template model.AuthTemplate, credentials map[string]any) error {
	switch instance.AuthTemplateFlow {
	case "password_token", "client_credentials", "jwt_bearer_grant", "token_exchange":
	default:
		return errors.New("this authentication flow requires a connection account and an API verification endpoint or browser authorization")
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	testCredentials := make(map[string]any, len(credentials))
	for key, value := range credentials {
		testCredentials[key] = value
	}
	_, values, err := s.requestInstanceToken(ctx, model.Connection{}, instance, template, testCredentials, true)
	if err != nil {
		return err
	}
	if _, err := renderTemplate(instance.HeaderValueTemplate, values); err != nil {
		return err
	}
	var rules []struct {
		Template string `json:"template"`
	}
	if len(template.InjectionRules) > 0 {
		if err := jsonutil.Unmarshal(template.InjectionRules, &rules); err != nil {
			return errors.New("authentication injection rules are invalid")
		}
		for _, rule := range rules {
			if _, err := renderTemplate(rule.Template, values); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) requestInstanceToken(ctx context.Context, connection model.Connection, instance model.AuthInstance, template model.AuthTemplate, credentials map[string]any, testOnly bool) (resultConnection model.Connection, resultValues map[string]any, resultError error) {
	stage := "token_request"
	defer func() {
		if resultError != nil {
			var existing *AuthStageError
			if !errors.As(resultError, &existing) {
				resultError = &AuthStageError{Stage: stage, Err: resultError}
			}
		}
	}()
	copied := make(map[string]any, len(credentials))
	for key, value := range credentials {
		copied[key] = value
	}
	credentials = copied
	tokenURL := instance.TokenURL
	if (instance.AuthTemplateFlow == "oauth2_code" || instance.AuthTemplateFlow == "oidc") && instance.RefreshURL != "" {
		tokenURL = instance.RefreshURL
	}
	if tokenURL == "" {
		return connection, nil, errors.New("authentication instance has no token URL")
	}
	requestConfig, err := EffectiveRequest(instance, template)
	if err != nil {
		return connection, nil, err
	}
	if err := ValidateEffectiveConfiguration(instance, template); err != nil {
		return connection, nil, err
	}
	advanced, err := ParseAuthRequest(instance)
	if err != nil {
		return connection, nil, err
	}
	method := requestConfig.Method
	var body io.Reader
	var authHeader string
	contentType := "application/json"
	values, config, err := s.advancedCredentials(instance, credentials)
	if err != nil {
		return connection, nil, err
	}
	if instance.AuthTemplateFlow == "oauth2_code" || instance.AuthTemplateFlow == "oidc" {
		refreshToken := firstString(credentials, "refresh_token", "refreshToken")
		if refreshToken == "" {
			return connection, nil, fmt.Errorf("%w: refresh token missing", ErrReauthorization)
		}
		form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}}
		authHeader, err = oauthClientAuth(instance, values, form, tokenURL)
		if err != nil {
			return connection, nil, err
		}
		body = strings.NewReader(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	} else if instance.AuthTemplateFlow == "client_credentials" {
		form := url.Values{"grant_type": {"client_credentials"}}
		if config.Scope != "" {
			form.Set("scope", config.Scope)
		}
		authHeader, err = oauthClientAuth(instance, values, form, tokenURL)
		if err != nil {
			return connection, nil, err
		}
		body = strings.NewReader(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	} else if instance.AuthTemplateFlow == "jwt_bearer_grant" {
		audience := config.Audience
		if audience == "" {
			audience = instance.TokenURL
		}
		assertion, signErr := signedAssertion(values, config, audience)
		if signErr != nil {
			return connection, nil, signErr
		}
		form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {assertion}}
		if config.Scope != "" {
			form.Set("scope", config.Scope)
		}
		body = strings.NewReader(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	} else if instance.AuthTemplateFlow == "token_exchange" {
		subject := firstString(credentials, "subject_token")
		if subject == "" || config.SubjectType == "" {
			return connection, nil, errors.New("subject token and token type are required")
		}
		form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:token-exchange"}, "subject_token": {subject}, "subject_token_type": {config.SubjectType}}
		if config.RequestedType != "" {
			form.Set("requested_token_type", config.RequestedType)
		}
		if config.Audience != "" {
			form.Set("audience", config.Audience)
		}
		if config.Resource != "" {
			form.Set("resource", config.Resource)
		}
		if config.Scope != "" {
			form.Set("scope", config.Scope)
		}
		if firstString(values, "clientId", "client_id") != "" || config.ClientAuth != "" {
			authHeader, err = oauthClientAuth(instance, values, form, tokenURL)
			if err != nil {
				return connection, nil, err
			}
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
	request.Header.Set("User-Agent", buildinfo.UserAgent)
	if authHeader != "" {
		request.Header.Set("Authorization", authHeader)
	}
	instanceSecrets, err := s.AuthInstanceSecrets(instance)
	if err != nil {
		return connection, nil, err
	}
	customRefresh := false
	if instance.AuthTemplateFlow == "password_token" && (requestConfig.SchemaVersion == 2 || advanced.SchemaVersion == 2) {
		fields, fieldErr := CredentialFields(template)
		if fieldErr != nil {
			return connection, nil, fieldErr
		}
		if err := ValidateCredentials(template, values); err != nil {
			return connection, nil, err
		}
		if advanced.Refresh.Mode == "refresh_token" && !testOnly && firstString(credentials, "refresh_token") != "" {
			customRefresh = true
			requestConfig = *advanced.Refresh.Request
			tokenURL = instance.RefreshURL
		}
		request, err = buildAuthRequest(ctx, tokenURL, requestConfig, fields, credentials, instanceSecrets)
		if err != nil {
			return connection, nil, err
		}
	}
	client, cleanup, err := s.tokenClient(instance, values)
	if err != nil {
		return connection, nil, err
	}
	defer cleanup()
	response, err := client.Do(request)
	if err != nil {
		return connection, nil, safeRequestError(err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxTokenResponse+1))
	if err != nil {
		return connection, nil, fmt.Errorf("read token response: %w", err)
	}
	if len(responseBody) > maxTokenResponse {
		return connection, nil, errors.New("token response exceeds 1 MiB")
	}
	if customRefresh && advanced.Refresh.FallbackOnInvalidRefreshToken {
		var failure any
		if jsonutil.Unmarshal(responseBody, &failure) == nil && matchesCondition(failure, advanced.Refresh.InvalidRefreshCondition) {
			// Only the explicitly configured invalid-token result permits one relogin.
			instanceCopy := instance
			var public map[string]any
			_ = jsonutil.Unmarshal(instance.PublicConfig, &public)
			advanced.Refresh.Mode = "relogin"
			public["authRequest"] = advanced
			instanceCopy.PublicConfig, _ = json.Marshal(public)
			return s.requestInstanceToken(ctx, connection, instanceCopy, template, credentials, testOnly)
		}
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
	stage = "token_response"
	var responseValue any
	if err := jsonutil.Unmarshal(responseBody, &responseValue); err != nil {
		return connection, nil, errors.New("token endpoint did not return JSON")
	}
	if !matchesCondition(responseValue, advanced.Response.SuccessCondition) {
		return connection, nil, errors.New("authentication response does not satisfy the success condition")
	}
	if instance.AuthTemplateFlow == "token_exchange" {
		responseObject, ok := responseValue.(map[string]any)
		if !ok || firstString(responseObject, "issued_token_type") == "" {
			return connection, nil, errors.New("token exchange response has no issued token type")
		}
		if config.RequestedType != "" && firstString(responseObject, "issued_token_type") != config.RequestedType {
			return connection, nil, errors.New("token exchange returned an unexpected token type")
		}
	}
	tokenPath := instance.TokenPath
	if tokenPath == "" && instance.AuthTemplateFlow != "password_token" {
		tokenPath = "$.access_token"
	}
	tokenValue, ok := lookupJSONPath(responseValue, tokenPath)
	token, isString := tokenValue.(string)
	if !ok || !isString || strings.TrimSpace(token) == "" {
		return connection, nil, fmt.Errorf("token path %q was not found", tokenPath)
	}
	credentials["token"] = fmt.Sprint(token)
	credentials["access_token"] = fmt.Sprint(token)
	if responseObject, ok := responseValue.(map[string]any); ok {
		if refreshToken := firstString(responseObject, "refresh_token", "refreshToken"); refreshToken != "" {
			credentials["refresh_token"] = refreshToken
		}
	}
	var expiresAt *time.Time
	expiryPath := instance.ExpiryPath
	if expiryPath == "" {
		expiryPath = "$.expires_in"
	}
	if expiry, ok := lookupJSONPath(responseValue, expiryPath); ok {
		if seconds, parseErr := strconv.ParseInt(fmt.Sprint(expiry), 10, 64); parseErr == nil && seconds > 0 {
			value := time.Now().UTC().Add(time.Duration(seconds) * time.Second)
			expiresAt = &value
		}
	}
	if advanced.SchemaVersion == 2 && instance.AuthTemplateFlow == "password_token" {
		expiresAt, err = responseExpiry(responseValue, instance.ExpiryPath, advanced.Response.Expiry, time.Now().UTC())
		if err != nil {
			return connection, nil, err
		}
		if advanced.Refresh.Mode == "refresh_token" {
			refresh, exists := lookupJSONPath(responseValue, advanced.Refresh.RefreshTokenPath)
			if text, ok := refresh.(string); exists && ok && strings.TrimSpace(text) != "" {
				credentials["refresh_token"] = text
			} else if !customRefresh {
				return connection, nil, errors.New("configured refresh token field is missing or empty")
			}
		}
	} else if testOnly && instance.ExpiryPath != "" && expiresAt == nil {
		return connection, nil, fmt.Errorf("expiry path %q must contain a positive integer in seconds", instance.ExpiryPath)
	}
	if testOnly {
		return connection, credentials, nil
	}
	if expiresAt == nil && (instance.AuthTemplateFlow == "token_exchange" || instance.AuthTemplateFlow == "jwt_bearer_grant") {
		value := time.Now().UTC()
		expiresAt = &value // Unknown lifetime: exchange again before the next request.
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
	if flow != "password_token" && flow != "client_credentials" && flow != "oauth2_code" && flow != "oidc" && flow != "jwt_bearer_grant" && flow != "token_exchange" {
		return false
	}
	if connection.LastErrorCode == "auth_configuration_changed" {
		return true
	}
	if token := firstString(credentials, "token", "access_token", "accessToken"); token == "" {
		return true
	}
	return connection.TokenExpiresAt != nil && connection.TokenExpiresAt.Before(time.Now().UTC().Add(30*time.Second))
}

func SupportsFlow(flow string) bool {
	switch flow {
	case "none", "static", "password_token", "client_credentials", "oauth2_code", "gateway", "mtls", "aws_sigv4", "jwt_direct", "jwt_bearer_grant", "token_exchange", "oidc":
		return true
	default:
		return false
	}
}

// SupportsTemplate preserves the meaning of legacy built-ins whose broad flow
// name overlaps with a newly implemented, narrower protocol.
func SupportsTemplate(key, flow string) bool {
	if key == "saml-token-exchange" {
		return false
	}
	return SupportsFlow(flow)
}

func ExecutableFlows() []string {
	return []string{"none", "static", "password_token", "client_credentials", "oauth2_code", "gateway", "mtls", "aws_sigv4", "jwt_direct", "jwt_bearer_grant", "token_exchange", "oidc"}
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

func lookupJSONPath(value any, path string) (any, bool) { return jsonutil.PathLookup(value, path) }

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
