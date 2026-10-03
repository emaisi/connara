package authn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"apihub-go/internal/buildinfo"
	"apihub-go/internal/executor"
	"apihub-go/internal/jsonutil"
	"apihub-go/internal/model"
)

type RequestValue struct {
	Source string `json:"source"`
	Name   string `json:"name,omitempty"`
	Value  any    `json:"value,omitempty"`
}
type RequestParameter struct {
	Name   string       `json:"name"`
	Target string       `json:"target"`
	Value  RequestValue `json:"value"`
}
type RequestHeader struct {
	Name  string       `json:"name"`
	Value RequestValue `json:"value"`
}
type RequestConfig struct {
	SchemaVersion  int                `json:"schemaVersion,omitempty"`
	Method         string             `json:"method"`
	BodyType       string             `json:"bodyType"`
	CredentialMode string             `json:"credentialMode,omitempty"`
	Parameters     []RequestParameter `json:"parameters,omitempty"`
	Headers        []RequestHeader    `json:"headers,omitempty"`
	// Legacy response fields are retained when old templates are edited.
	TokenPath  string `json:"tokenPath,omitempty"`
	ExpiryPath string `json:"expiryPath,omitempty"`
}
type SuccessCondition struct {
	Path     string `json:"path"`
	Operator string `json:"operator"`
	Value    any    `json:"value"`
}
type ExpiryConfig struct {
	Mode    string `json:"mode"`
	Format  string `json:"format,omitempty"`
	Seconds int64  `json:"seconds,omitempty"`
}
type ResponseConfig struct {
	SuccessCondition *SuccessCondition `json:"successCondition,omitempty"`
	Expiry           ExpiryConfig      `json:"expiry"`
}
type RefreshConfig struct {
	Mode                          string            `json:"mode"`
	RefreshTokenPath              string            `json:"refreshTokenPath,omitempty"`
	Request                       *RequestConfig    `json:"request,omitempty"`
	FallbackOnInvalidRefreshToken bool              `json:"fallbackOnInvalidRefreshToken,omitempty"`
	InvalidRefreshCondition       *SuccessCondition `json:"invalidRefreshCondition,omitempty"`
}
type VerificationConfig struct {
	Enabled          bool              `json:"enabled"`
	Request          *RequestConfig    `json:"request,omitempty"`
	SuccessCondition *SuccessCondition `json:"successCondition,omitempty"`
}
type InjectionRule struct {
	Target   string `json:"target"`
	Name     string `json:"name"`
	Template string `json:"template"`
}
type AuthRequestConfig struct {
	SchemaVersion     int                `json:"schemaVersion"`
	RequestOverride   *RequestConfig     `json:"requestOverride,omitempty"`
	InjectionOverride *[]InjectionRule   `json:"injectionOverride,omitempty"`
	Response          ResponseConfig     `json:"response"`
	Refresh           RefreshConfig      `json:"refresh"`
	Verification      VerificationConfig `json:"verification"`
}

type CredentialField struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Secret   bool   `json:"secret"`
	Required *bool  `json:"required,omitempty"`
	Type     string `json:"type,omitempty"`
}

func strictJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("authentication configuration contains invalid or unknown fields")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("authentication configuration must contain one JSON value")
	}
	return nil
}
func ParseAuthRequest(instance model.AuthInstance) (AuthRequestConfig, error) {
	var public map[string]json.RawMessage
	if len(instance.PublicConfig) > 0 {
		if err := jsonutil.Unmarshal(instance.PublicConfig, &public); err != nil {
			return AuthRequestConfig{}, errors.New("invalid public configuration")
		}
	}
	raw := public["authRequest"]
	if len(raw) == 0 || string(raw) == "null" {
		return AuthRequestConfig{}, nil
	}
	var config AuthRequestConfig
	if err := strictJSON(raw, &config); err != nil {
		return config, err
	}
	if config.SchemaVersion != 2 {
		return config, errors.New("unsupported authentication request version")
	}
	return config, nil
}
func ParseTokenRequest(template model.AuthTemplate) (RequestConfig, error) {
	config := RequestConfig{Method: "POST", BodyType: "json", CredentialMode: "all"}
	if len(template.TokenRequest) > 0 && string(template.TokenRequest) != "null" {
		if err := strictJSON(template.TokenRequest, &config); err != nil {
			return config, err
		}
	}
	if config.SchemaVersion != 0 && config.SchemaVersion != 1 && config.SchemaVersion != 2 {
		return config, errors.New("unsupported token request version")
	}
	config.Method = strings.ToUpper(config.Method)
	if config.Method != "" && config.Method != "POST" && config.Method != "GET" {
		return config, errors.New("authentication method must be GET or POST")
	}
	if config.Method == "" {
		config.Method = "POST"
	}
	if config.BodyType == "" {
		config.BodyType = "json"
	}
	if config.CredentialMode == "" {
		config.CredentialMode = "all"
	}
	return config, nil
}
func EffectiveRequest(instance model.AuthInstance, template model.AuthTemplate) (RequestConfig, error) {
	config, err := ParseTokenRequest(template)
	if err != nil {
		return config, err
	}
	advanced, err := ParseAuthRequest(instance)
	if err != nil {
		return config, err
	}
	if advanced.RequestOverride != nil {
		config = *advanced.RequestOverride
	}
	if instance.AuthTemplateFlow != "password_token" && instance.AuthTemplateFlow != "static" {
		if advanced.RequestOverride != nil {
			return config, errors.New("standard authentication protocols do not allow login request overrides")
		}
		config.Method = "POST"
		config.BodyType = "form"
	}
	return config, nil
}
func CredentialFields(template model.AuthTemplate) ([]CredentialField, error) {
	if len(template.CredentialSchema) == 0 || (template.FlowType == "none" && string(template.CredentialSchema) == "{}") {
		return nil, nil
	}
	var schema struct {
		Type   string            `json:"type"`
		Fields []CredentialField `json:"fields"`
	}
	if err := strictJSON(template.CredentialSchema, &schema); err != nil {
		return nil, err
	}
	if schema.Type != "object" || len(schema.Fields) > 64 {
		return nil, errors.New("credential schema must be an object with at most 64 fields")
	}
	names := map[string]bool{}
	for _, field := range schema.Fields {
		if field.Name == "" || len(field.Name) > 100 || names[field.Name] || strings.ContainsAny(field.Name, " \t\r\n{}") {
			return nil, errors.New("credential field names must be unique identifiers")
		}
		names[field.Name] = true
		if field.Type != "" && field.Type != "string" && field.Type != "number" && field.Type != "boolean" {
			return nil, errors.New("unsupported credential field type")
		}
		if template.FlowType == "password_token" && isRuntimeCredential(field.Name) {
			return nil, errors.New("login credential field conflicts with a runtime token field")
		}
	}
	return schema.Fields, nil
}
func ValidateCredentials(template model.AuthTemplate, values map[string]any) error {
	fields, err := CredentialFields(template)
	if err != nil {
		return err
	}
	for _, field := range fields {
		value, exists := values[field.Name]
		if field.Name == "client_id" && !exists {
			value, exists = values["clientId"]
		}
		if field.Name == "client_secret" && !exists {
			value, exists = values["clientSecret"]
		}
		if !exists || value == nil || (reflect.TypeOf(value).Kind() == reflect.String && strings.TrimSpace(fmt.Sprint(value)) == "") {
			if field.Required == nil || *field.Required {
				return fmt.Errorf("credential field %q is required", field.Name)
			}
			continue
		}
		switch field.Type {
		case "boolean":
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("credential field %q must be boolean", field.Name)
			}
		case "number":
			if number, err := strconv.ParseFloat(fmt.Sprint(value), 64); err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
				return fmt.Errorf("credential field %q must be numeric", field.Name)
			}
		case "", "string":
			if _, ok := value.(string); !ok {
				return fmt.Errorf("credential field %q must be a string", field.Name)
			}
		}
	}
	return nil
}
func validateCondition(condition *SuccessCondition) error {
	if condition == nil {
		return nil
	}
	if _, ok := jsonutil.PathSegments(condition.Path); !ok || condition.Operator != "equals" {
		return errors.New("success condition must use a valid path and equals")
	}
	switch condition.Value.(type) {
	case nil, string, bool, json.Number, float64:
	default:
		return errors.New("success condition value must be scalar")
	}
	return nil
}
func validateRequest(config RequestConfig, fields []CredentialField, stage string) error {
	if config.Method != "GET" && config.Method != "POST" {
		return errors.New("authentication request method must be GET or POST")
	}
	if config.BodyType != "none" && config.BodyType != "json" && config.BodyType != "form" {
		return errors.New("authentication request body must be none, json or form")
	}
	if config.Method == "GET" && config.BodyType != "none" {
		return errors.New("GET authentication requests cannot have a body")
	}
	if config.CredentialMode != "all" && config.CredentialMode != "mapped" {
		return errors.New("credential mode must be all or mapped")
	}
	if stage == "verification" && config.CredentialMode != "mapped" {
		return errors.New("API verification must use explicit non-secret parameters")
	}
	if len(config.Parameters) > 64 || len(config.Headers) > 32 || (config.CredentialMode == "all" && len(config.Parameters) > 0) {
		return errors.New("too many or conflicting request parameters")
	}
	declared := map[string]CredentialField{}
	for _, field := range fields {
		declared[field.Name] = field
	}
	validateValue := func(value RequestValue) error {
		switch value.Source {
		case "credential":
			field, ok := declared[value.Name]
			if !ok {
				return fmt.Errorf("unknown credential reference %q", value.Name)
			}
			if stage == "verification" && field.Secret {
				return errors.New("API verification cannot send secret login credentials")
			}
		case "instance_secret":
			if value.Name == "" || stage == "verification" {
				return errors.New("invalid instance secret reference")
			}
		case "runtime":
			if stage == "login" || (value.Name != "token" && value.Name != "access_token" && value.Name != "refresh_token") {
				return errors.New("invalid runtime token reference")
			}
		case "literal":
			switch value.Value.(type) {
			case nil, string, bool, json.Number, float64:
			default:
				return errors.New("literal request values must be scalar")
			}
		default:
			return errors.New("unsupported request value source")
		}
		return nil
	}
	names := map[string]bool{}
	paths := [][]string{}
	for _, param := range config.Parameters {
		if param.Name == "" || len(param.Name) > 255 || (param.Target != "query" && param.Target != "body") {
			return errors.New("invalid request parameter")
		}
		key := param.Target + ":" + param.Name
		if names[key] {
			return errors.New("duplicate request parameter")
		}
		names[key] = true
		if param.Target == "body" {
			if config.BodyType == "none" {
				return errors.New("request body parameters require a body format")
			}
			if config.BodyType == "json" {
				path, err := objectPath(param.Name)
				if err != nil {
					return err
				}
				for _, previous := range paths {
					if pathPrefix(path, previous) || pathPrefix(previous, path) {
						return errors.New("conflicting JSON parameter paths")
					}
				}
				paths = append(paths, path)
			}
		}
		if param.Value.Source == "literal" {
			lower := strings.ToLower(param.Name)
			secretName := lower == "password" || strings.HasSuffix(lower, ".password") || lower == "client_secret" || lower == "api_key" || lower == "access_token" || lower == "refresh_token"
			if field, ok := declared[param.Name]; ok && field.Secret {
				secretName = true
			}
			if secretName {
				return errors.New("secret parameters must reference credentials or encrypted instance secrets")
			}
		}
		if err := validateValue(param.Value); err != nil {
			return err
		}
	}
	names = map[string]bool{}
	for _, header := range config.Headers {
		name := http.CanonicalHeaderKey(header.Name)
		for _, character := range name {
			if character > 127 || character < 33 {
				return errors.New("invalid authentication header name")
			}
		}
		if name == "" || strings.ContainsAny(name, " ()<>@,;:\\\"/[]?={}\t\r\n") || len(name) > 255 || names[strings.ToLower(name)] {
			return errors.New("invalid or duplicate authentication header")
		}
		names[strings.ToLower(name)] = true
		switch name {
		case "Host", "Content-Length", "Content-Type", "Connection", "Transfer-Encoding", "Trailer", "Upgrade", "Proxy-Authorization", "Proxy-Connection":
			return errors.New("transport headers cannot be configured")
		}
		if err := validateValue(header.Value); err != nil {
			return err
		}
		lowerName := strings.ToLower(name)
		if header.Value.Source == "literal" && (name == "Authorization" || name == "Cookie" || strings.Contains(lowerName, "api-key") || strings.Contains(lowerName, "token") || strings.Contains(lowerName, "secret")) {
			return errors.New("secret headers must reference credentials or encrypted instance secrets")
		}
		if header.Value.Source == "literal" && strings.ContainsAny(fmt.Sprint(header.Value.Value), "\r\n") {
			return errors.New("authentication header value contains a newline")
		}
	}
	return nil
}
func objectPath(name string) ([]string, error) {
	segments, ok := jsonutil.PathSegments(name)
	if !ok {
		return nil, errors.New("invalid JSON parameter path")
	}
	path := []string{}
	for _, segment := range segments {
		if segment.IsIndex {
			return nil, errors.New("JSON parameter array writes are not supported")
		}
		if segment.Key == "__proto__" || segment.Key == "constructor" || segment.Key == "prototype" {
			return nil, errors.New("reserved JSON parameter name")
		}
		path = append(path, segment.Key)
	}
	return path, nil
}
func pathPrefix(a, b []string) bool {
	if len(a) > len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func ValidateTemplateConfiguration(template model.AuthTemplate) error {
	if len(template.CredentialSchema) == 0 {
		return errors.New("credential schema is required")
	}
	fields, err := CredentialFields(template)
	if err != nil {
		return err
	}
	config, err := ParseTokenRequest(template)
	if err != nil {
		return err
	}
	if config.SchemaVersion == 2 {
		if err := validateRequest(config, fields, "login"); err != nil {
			return err
		}
		if template.FlowType == "client_credentials" && (config.Method != "POST" || config.BodyType != "form" || len(config.Parameters) > 0 || len(config.Headers) > 0) {
			return errors.New("OAuth client credentials use protocol-defined POST form requests")
		}
	}
	var rules []InjectionRule
	if err := jsonutil.Unmarshal(template.InjectionRules, &rules); err != nil {
		return errors.New("invalid injection rules")
	}
	return validateInjection(rules, fields, template.FlowType)
}
func validateInjection(rules []InjectionRule, fields []CredentialField, flow string) error {
	if len(rules) > 16 {
		return errors.New("too many injection rules")
	}
	names := map[string]bool{}
	values := map[string]any{"token": "test", "access_token": "test", "refresh_token": "test", "basic_token": "test"}
	for _, field := range fields {
		values[field.Name] = "test"
	}
	for _, rule := range rules {
		key := rule.Target + ":" + strings.ToLower(rule.Name)
		if names[key] || rule.Name == "" || strings.ContainsAny(rule.Name, "\r\n") || (rule.Target != "header" && rule.Target != "query" && rule.Target != "cookie") {
			return errors.New("invalid or duplicate authentication injection")
		}
		names[key] = true
		if rule.Target == "header" {
			check := RequestConfig{Method: "GET", BodyType: "none", CredentialMode: "mapped", Headers: []RequestHeader{{Name: rule.Name, Value: RequestValue{Source: "instance_secret", Name: "validation"}}}}
			if err := validateRequest(check, nil, "login"); err != nil {
				return err
			}
		}
		if rule.Template == "" || len(rule.Template) > 1000 {
			return errors.New("invalid injection template")
		}
		if _, err := renderTemplate(rule.Template, values); err != nil {
			return err
		}
	}
	return nil
}
func ValidateEffectiveConfiguration(instance model.AuthInstance, template model.AuthTemplate) error {
	return validateEffectiveConfiguration(instance, template, false)
}
func ValidateDraftConfiguration(instance model.AuthInstance, template model.AuthTemplate) error {
	return validateEffectiveConfiguration(instance, template, true)
}
func validateEffectiveConfiguration(instance model.AuthInstance, template model.AuthTemplate, incomplete bool) error {
	for _, path := range []string{instance.TokenPath, instance.ExpiryPath} {
		if path != "" {
			if _, ok := jsonutil.PathSegments(path); !ok {
				return errors.New("token or expiry JSON path is invalid")
			}
		}
	}
	for _, endpoint := range []string{instance.TokenURL, instance.RefreshURL} {
		if endpoint != "" {
			parsed, err := url.Parse(endpoint)
			if err != nil || !httpScheme(parsed) || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
				return errors.New("authentication URLs must be absolute http(s) URLs without user info or fragments")
			}
		}
	}
	var public struct {
		VerificationPath string `json:"verificationPath"`
	}
	if len(instance.PublicConfig) > 0 {
		if err := jsonutil.Unmarshal(instance.PublicConfig, &public); err != nil {
			return errors.New("invalid public configuration")
		}
	}
	if public.VerificationPath != "" {
		if err := executor.ValidatePath(public.VerificationPath); err != nil {
			return err
		}
	}

	advanced, err := ParseAuthRequest(instance)
	if err != nil {
		return err
	}
	fields, err := CredentialFields(template)
	if err != nil {
		return err
	}
	request, err := EffectiveRequest(instance, template)
	if err != nil {
		return err
	}
	if request.SchemaVersion == 2 {
		if err := validateRequest(request, fields, "login"); err != nil {
			return err
		}
	}
	if advanced.SchemaVersion == 0 {
		return nil
	}
	if err := validateCondition(advanced.Response.SuccessCondition); err != nil {
		return err
	}
	switch advanced.Response.Expiry.Mode {
	case "none":
	case "fixed":
		if advanced.Response.Expiry.Seconds <= 0 || advanced.Response.Expiry.Seconds > math.MaxInt64/int64(time.Second) {
			return errors.New("fixed expiry must be positive seconds within range")
		}
	case "field":
		switch advanced.Response.Expiry.Format {
		case "duration_seconds", "duration_milliseconds", "unix_seconds", "unix_milliseconds", "rfc3339":
		default:
			return errors.New("unsupported expiry format")
		}
		if instance.ExpiryPath == "" && !incomplete {
			return errors.New("expiry field path is required")
		}
	default:
		return errors.New("an explicit expiry mode is required")
	}
	if advanced.InjectionOverride != nil {
		if err := validateInjection(*advanced.InjectionOverride, fields, template.FlowType); err != nil {
			return err
		}
	}
	switch advanced.Refresh.Mode {
	case "", "relogin":
	case "refresh_token":
		if instance.AuthTemplateFlow != "password_token" {
			return errors.New("custom refresh applies only to login token flows")
		}
		if !incomplete && (instance.RefreshURL == "" || advanced.Refresh.Request == nil) {
			return errors.New("refresh URL and request are required")
		}
		if _, ok := jsonutil.PathSegments(advanced.Refresh.RefreshTokenPath); !ok && (!incomplete || advanced.Refresh.RefreshTokenPath != "") {
			return errors.New("refresh token path is required")
		}
		if advanced.Refresh.Request != nil {
			if err := validateRequest(*advanced.Refresh.Request, fields, "refresh"); err != nil {
				return err
			}
		}
		if err := validateCondition(advanced.Refresh.InvalidRefreshCondition); err != nil {
			return err
		}
		if advanced.Refresh.FallbackOnInvalidRefreshToken && advanced.Refresh.InvalidRefreshCondition == nil {
			return errors.New("refresh fallback requires an explicit invalid-token condition")
		}
	default:
		return errors.New("unsupported refresh mode")
	}
	if advanced.Verification.Enabled {
		var public struct {
			VerificationPath string `json:"verificationPath"`
		}
		_ = jsonutil.Unmarshal(instance.PublicConfig, &public)
		if !incomplete && (public.VerificationPath == "" || advanced.Verification.Request == nil) {
			return errors.New("API verification requires a path and request")
		}
		if advanced.Verification.Request != nil {
			if err := validateRequest(*advanced.Verification.Request, fields, "verification"); err != nil {
				return err
			}
		}
		if err := validateCondition(advanced.Verification.SuccessCondition); err != nil {
			return err
		}
	}
	return nil
}
func requestValue(value RequestValue, credentials, secrets map[string]any) (any, error) {
	var result any
	var ok bool
	switch value.Source {
	case "literal":
		return value.Value, nil
	case "credential", "runtime":
		result, ok = credentials[value.Name]
	case "instance_secret":
		result, ok = secrets[value.Name]
	}
	if !ok || result == nil {
		return nil, fmt.Errorf("request value %q is missing", value.Name)
	}
	return result, nil
}
func buildAuthRequest(ctx context.Context, endpoint string, config RequestConfig, fields []CredentialField, credentials, secrets map[string]any) (*http.Request, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, errors.New("invalid authentication URL")
	}
	query := parsed.Query()
	form := url.Values{}
	payload := map[string]any{}
	put := func(name, target string, value any) error {
		if target == "query" {
			if query.Has(name) {
				return errors.New("request parameter conflicts with endpoint query")
			}
			query.Set(name, fmt.Sprint(value))
			return nil
		}
		if config.BodyType == "form" {
			form.Set(name, fmt.Sprint(value))
			return nil
		}
		path, err := objectPath(name)
		if err != nil {
			return err
		}
		current := payload
		for _, part := range path[:len(path)-1] {
			if current[part] == nil {
				current[part] = map[string]any{}
			}
			next, ok := current[part].(map[string]any)
			if !ok {
				return errors.New("conflicting JSON parameters")
			}
			current = next
		}
		current[path[len(path)-1]] = value
		return nil
	}
	if config.CredentialMode == "all" {
		for _, field := range fields {
			if isRuntimeCredential(field.Name) {
				continue
			}
			value, ok := credentials[field.Name]
			if !ok || value == nil || value == "" {
				continue
			}
			target := "body"
			if config.Method == "GET" {
				target = "query"
			}
			if err := put(field.Name, target, value); err != nil {
				return nil, err
			}
		}
	} else {
		for _, param := range config.Parameters {
			value, err := requestValue(param.Value, credentials, secrets)
			if err != nil {
				return nil, err
			}
			if err := put(param.Name, param.Target, value); err != nil {
				return nil, err
			}
		}
	}
	parsed.RawQuery = query.Encode()
	var body io.Reader
	if config.BodyType == "json" {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(encoded)
	} else if config.BodyType == "form" {
		body = strings.NewReader(form.Encode())
	}
	request, err := http.NewRequestWithContext(ctx, config.Method, parsed.String(), body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", buildinfo.UserAgent)
	if config.BodyType == "json" {
		request.Header.Set("Content-Type", "application/json")
	} else if config.BodyType == "form" {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, header := range config.Headers {
		value, err := requestValue(header.Value, credentials, secrets)
		if err != nil {
			return nil, err
		}
		text := fmt.Sprint(value)
		if strings.ContainsAny(text, "\r\n") {
			return nil, errors.New("authentication header contains a newline")
		}
		request.Header.Set(header.Name, text)
	}
	return request, nil
}
func matchesCondition(response any, condition *SuccessCondition) bool {
	if condition == nil {
		return true
	}
	value, ok := lookupJSONPath(response, condition.Path)
	if !ok {
		return false
	}
	a, _ := json.Marshal(value)
	b, _ := json.Marshal(condition.Value)
	return bytes.Equal(a, b)
}
func responseExpiry(response any, path string, config ExpiryConfig, now time.Time) (*time.Time, error) {
	if config.Mode == "none" {
		return nil, nil
	}
	if config.Mode == "fixed" {
		if config.Seconds <= 0 || config.Seconds > math.MaxInt64/int64(time.Second) {
			return nil, errors.New("invalid fixed expiry")
		}
		result := now.Add(time.Duration(config.Seconds) * time.Second)
		return &result, nil
	}
	value, ok := lookupJSONPath(response, path)
	if !ok {
		return nil, errors.New("configured expiry field is missing")
	}
	var result time.Time
	if config.Format == "rfc3339" {
		text, ok := value.(string)
		if !ok {
			return nil, errors.New("expiry must be an RFC3339 string")
		}
		parsed, err := time.Parse(time.RFC3339, text)
		if err != nil {
			return nil, errors.New("expiry must be an RFC3339 string")
		}
		result = parsed
	} else {
		text := fmt.Sprint(value)
		for _, char := range text {
			if char < '0' || char > '9' {
				return nil, errors.New("expiry must be a positive integer")
			}
		}
		number, err := strconv.ParseInt(text, 10, 64)
		if err != nil || number <= 0 {
			return nil, errors.New("expiry must be a positive integer within range")
		}
		switch config.Format {
		case "duration_seconds", "duration_milliseconds":
			unit := time.Second
			if config.Format == "duration_milliseconds" {
				unit = time.Millisecond
			}
			if number > math.MaxInt64/int64(unit) {
				return nil, errors.New("expiry duration exceeds range")
			}
			result = now.Add(time.Duration(number) * unit)
		case "unix_seconds":
			result = time.Unix(number, 0)
		case "unix_milliseconds":
			result = time.UnixMilli(number)
		default:
			return nil, errors.New("unsupported expiry format")
		}
	}
	if !result.After(now) {
		return nil, errors.New("token expiry is already in the past")
	}
	return &result, nil
}
