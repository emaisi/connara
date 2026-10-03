package executor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"apihub-go/internal/jsonutil"
	"apihub-go/internal/model"
	"apihub-go/internal/safejson"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

type Parameter struct {
	Name     string          `json:"name"`
	In       string          `json:"in"`
	Type     string          `json:"type"`
	Required bool            `json:"required"`
	Default  json.RawMessage `json:"default,omitempty"`
	Example  json.RawMessage `json:"example,omitempty"`
}
type FixedHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
type RequestConfig struct {
	SchemaVersion int           `json:"schemaVersion"`
	BodyFormat    string        `json:"bodyFormat"`
	Parameters    []Parameter   `json:"parameters"`
	Headers       []FixedHeader `json:"headers"`
}
type Condition struct {
	Path     string `json:"path"`
	Operator string `json:"operator"`
	Value    any    `json:"value"`
}
type ResponseConfig struct {
	StatusCodes      []int      `json:"statusCodes"`
	SuccessCondition *Condition `json:"successCondition,omitempty"`
}
type ExecutionConfig struct {
	TimeoutMs int    `json:"timeoutMs"`
	RetryMode string `json:"retryMode"`
}
type Preview struct {
	Method  string      `json:"method"`
	URL     string      `json:"url"`
	Headers http.Header `json:"headers"`
	Body    any         `json:"body"`
}

func strictConfig(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	d.DisallowUnknownFields()
	return d.Decode(dst)
}
func secretName(name string) bool {
	n := strings.ToLower(name)
	return safejson.SensitiveKey(name) || strings.Contains(n, "password") || strings.Contains(n, "secret") || strings.Contains(n, "token") || strings.Contains(n, "apikey") || strings.Contains(n, "api-key") || strings.Contains(n, "api_key") || n == "authorization" || n == "cookie"
}
func validHeader(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		if !strings.ContainsRune("!#$%&'*+-.^_`|~0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ", c) {
			return false
		}
	}
	return true
}

func ValidateConfig(runtime *model.HTTPActionRuntime) error {
	if runtime == nil {
		return errors.New("request definition is required")
	}
	if strings.ContainsAny(pathParameter.ReplaceAllString(runtime.Path, ""), "{}") {
		return &FieldError{Field: "relativePath", Path: "$", Constraint: "unmatched path placeholder"}
	}
	if err := ValidatePath(runtime.Path); err != nil {
		return &FieldError{Field: "relativePath", Path: "$", Constraint: err.Error()}
	}
	var cfg RequestConfig
	if err := strictConfig(runtime.RequestConfig, &cfg); err != nil {
		return &FieldError{Field: "requestConfig", Path: "$", Constraint: err.Error()}
	}
	if cfg.SchemaVersion != 0 && cfg.SchemaVersion != 1 {
		return errors.New("unsupported request schemaVersion")
	}
	method := strings.ToUpper(runtime.Method)
	if !strings.Contains("|GET|HEAD|POST|PUT|PATCH|DELETE|OPTIONS|", "|"+method+"|") {
		return errors.New("unsupported HTTP method")
	}
	if cfg.SchemaVersion == 1 {
		if cfg.BodyFormat != "none" && cfg.BodyFormat != "json" && cfg.BodyFormat != "form" {
			return errors.New("bodyFormat must be none, json or form")
		}
		if (method == "GET" || method == "HEAD") && cfg.BodyFormat != "none" {
			return errors.New("GET/HEAD cannot have a body")
		}
		if len(cfg.Parameters) > 64 || len(cfg.Headers) > 32 {
			return errors.New("too many parameters or headers")
		}
		names := map[string]bool{}
		pathNames := map[string]bool{}
		headerNames := map[string]bool{}
		bodyNames := []string{}
		for _, p := range cfg.Parameters {
			if p.Name == "" || len(p.Name) > 100 || names[p.Name] {
				return errors.New("parameter names must be nonempty and unique")
			}
			names[p.Name] = true
			if secretName(p.Name) {
				return &FieldError{Field: "requestConfig", Path: p.Name, Constraint: "authentication values must come from the account"}
			}
			switch p.Type {
			case "string", "number", "integer", "boolean", "object", "array":
			default:
				return &FieldError{Field: "requestConfig", Path: p.Name, Constraint: "unsupported parameter type"}
			}
			switch p.In {
			case "path":
				if !p.Required || p.Type == "object" || p.Type == "array" {
					return errors.New("path parameters must be required scalar values")
				}
				pathNames[p.Name] = true
			case "query":
				if p.Type == "object" {
					return errors.New("query objects are unsupported")
				}
			case "header":
				n := strings.ToLower(p.Name)
				if headerNames[n] {
					return &FieldError{Field: "requestConfig", Path: p.Name, Constraint: "duplicate header parameter"}
				}
				headerNames[n] = true
				if !validHeader(p.Name) || forbiddenHeader(p.Name) || p.Type == "object" || p.Type == "array" {
					return errors.New("invalid parameter header")
				}
			case "body":
				if cfg.BodyFormat == "none" {
					return errors.New("body parameters require a body format")
				}
				if cfg.BodyFormat == "form" && (p.Type == "array" || p.Type == "object") {
					return errors.New("form parameters must be scalar")
				}
				for _, other := range bodyNames {
					if strings.HasPrefix(p.Name, other+".") || strings.HasPrefix(other, p.Name+".") {
						return errors.New("body parameter paths overlap")
					}
				}
				bodyNames = append(bodyNames, p.Name)
				for _, part := range strings.Split(p.Name, ".") {
					if part == "" || part == "__proto__" || part == "prototype" || part == "constructor" {
						return errors.New("unsafe body parameter path")
					}
				}
			default:
				return errors.New("parameter location must be path, query, header or body")
			}
			if len(p.Example) > 0 {
				var v any
				if jsonutil.Unmarshal(p.Example, &v) != nil || !parameterType(p.Type, v) {
					return fmt.Errorf("%s: example does not match type", p.Name)
				}
			}
			if len(p.Default) > 0 {
				var v any
				if jsonutil.Unmarshal(p.Default, &v) != nil || !parameterType(p.Type, v) {
					return fmt.Errorf("%s: default does not match type", p.Name)
				}
			}
		}
		for _, match := range pathParameter.FindAllStringSubmatch(runtime.Path, -1) {
			if !pathNames[match[1]] {
				return fmt.Errorf("path parameter %s is missing", match[1])
			}
			delete(pathNames, match[1])
		}
		if len(pathNames) > 0 {
			return errors.New("path parameter has no placeholder")
		}
		headers := map[string]bool{}
		for _, h := range cfg.Headers {
			n := strings.ToLower(h.Name)
			if !validHeader(h.Name) || forbiddenHeader(h.Name) || secretName(h.Name) || strings.ContainsAny(h.Value, "\r\n") || headers[n] {
				return errors.New("invalid or sensitive fixed header")
			}
			for _, p := range cfg.Parameters {
				if p.In == "header" && strings.EqualFold(p.Name, h.Name) {
					return errors.New("header parameter conflicts with fixed header")
				}
			}
			headers[n] = true
		}
	}
	var response ResponseConfig
	if err := strictConfig(runtime.ResponseConfig, &response); err != nil {
		return &FieldError{Field: "responseConfig", Path: "$", Constraint: err.Error()}
	}
	for _, status := range response.StatusCodes {
		if status < 200 || status > 299 {
			return &FieldError{Field: "responseConfig", Path: "statusCodes", Constraint: "must be 2xx"}
		}
	}
	if c := response.SuccessCondition; c != nil {
		if !strings.HasPrefix(c.Path, "$") || c.Operator != "equals" {
			return errors.New("success condition needs a JSON path and equals operator")
		}
		if !jsonutil.ValidPath(c.Path) {
			return errors.New("invalid success path")
		}
	}
	var execution ExecutionConfig
	if err := strictConfig(runtime.ExecutionConfig, &execution); err != nil {
		return &FieldError{Field: "executionConfig", Path: "$", Constraint: err.Error()}
	}
	if execution.TimeoutMs != 0 && (execution.TimeoutMs < 100 || execution.TimeoutMs > 40000) {
		return &FieldError{Field: "executionConfig", Path: "timeoutMs", Constraint: "must be 100..40000"}
	}
	if execution.RetryMode != "" && execution.RetryMode != "none" {
		return errors.New("only retryMode none is supported")
	}
	return nil
}
func forbiddenHeader(n string) bool {
	switch strings.ToLower(n) {
	case "host", "content-length", "transfer-encoding", "connection", "proxy-authorization", "content-type", "authorization", "cookie":
		return true
	}
	return false
}
func parameterType(kind string, v any) bool {
	switch kind {
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "number", "integer":
		var text string
		switch n := v.(type) {
		case json.Number:
			text = n.String()
		case float64:
			text = fmt.Sprint(n)
		case int:
			text = fmt.Sprint(n)
		default:
			return false
		}
		r, ok := new(big.Rat).SetString(text)
		return ok && (kind != "integer" || r.IsInt())
	}
	return false
}
func scalar(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}
func setBody(obj map[string]any, name string, v any) {
	parts := strings.Split(name, ".")
	for _, part := range parts[:len(parts)-1] {
		if obj[part] == nil {
			obj[part] = map[string]any{}
		}
		obj = obj[part].(map[string]any)
	}
	obj[parts[len(parts)-1]] = v
}

// PrepareInput applies defaults and validates mapping types without mutating callers.
func PrepareInput(runtime *model.HTTPActionRuntime, input map[string]any) (map[string]any, error) {
	if err := ValidateConfig(runtime); err != nil {
		return nil, err
	}
	var cfg RequestConfig
	_ = strictConfig(runtime.RequestConfig, &cfg)
	values := map[string]any{}
	for k, v := range input {
		values[k] = v
	}
	if cfg.SchemaVersion == 1 {
		declared := map[string]bool{}
		for _, p := range cfg.Parameters {
			declared[p.Name] = true
			if _, ok := values[p.Name]; !ok && len(p.Default) > 0 {
				var v any
				_ = jsonutil.Unmarshal(p.Default, &v)
				values[p.Name] = v
			}
			v, ok := values[p.Name]
			if !ok {
				if p.Required {
					return nil, fmt.Errorf("%s: required parameter is missing", p.Name)
				}
				continue
			}
			if !parameterType(p.Type, v) {
				return nil, fmt.Errorf("%s: value does not match %s", p.Name, p.Type)
			}
		}
		for name := range values {
			if !declared[name] {
				return nil, fmt.Errorf("%s: unknown input parameter", name)
			}
		}
	}
	return values, nil
}

// BuildRequest is the single URL/body builder for preview, API execution and workflows.
func BuildRequest(base string, runtime *model.HTTPActionRuntime, input map[string]any) (*http.Request, error) {
	if err := ValidateConfig(runtime); err != nil {
		return nil, err
	}
	var cfg RequestConfig
	_ = strictConfig(runtime.RequestConfig, &cfg)
	values, err := PrepareInput(runtime, input)
	if err != nil {
		return nil, err
	}

	expanded, remaining, err := expandPath(runtime.Path, values)
	if err != nil {
		return nil, err
	}
	target, err := joinTarget(base, expanded)
	if err != nil {
		return nil, err
	}
	query := target.Query()
	body := map[string]any{}
	form := url.Values{}
	headers := http.Header{}
	method := strings.ToUpper(runtime.Method)
	if cfg.SchemaVersion == 0 {
		if method == "GET" || method == "HEAD" {
			addQuery(target, remaining)
		} else {
			body = remaining
			cfg.BodyFormat = "json"
		}
	} else {
		for _, p := range cfg.Parameters {
			v, ok := values[p.Name]
			if !ok {
				continue
			}
			switch p.In {
			case "query":
				if a, ok := v.([]any); ok {
					for _, item := range a {
						if item == nil || !isScalar(item) {
							return nil, fmt.Errorf("%s: query array must contain scalars", p.Name)
						}
						query.Add(p.Name, scalar(item))
					}
				} else {
					query.Set(p.Name, scalar(v))
				}
			case "header":
				str := scalar(v)
				if strings.ContainsAny(str, "\r\n") {
					return nil, errors.New("header value contains a newline")
				}
				headers.Set(p.Name, str)
			case "body":
				if cfg.BodyFormat == "form" {
					form.Set(p.Name, scalar(v))
				} else {
					setBody(body, p.Name, v)
				}
			}
		}
		target.RawQuery = query.Encode()
	}
	var encoded []byte
	if cfg.BodyFormat == "json" {
		encoded, err = json.Marshal(body)
		headers.Set("Content-Type", "application/json")
	} else if cfg.BodyFormat == "form" {
		encoded = []byte(form.Encode())
		headers.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(method, target.String(), bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header = headers
	req.Header.Set("Accept", "application/json")
	for _, h := range cfg.Headers {
		req.Header.Set(h.Name, h.Value)
	}
	return req, nil
}
func isScalar(v any) bool {
	switch v.(type) {
	case string, bool, json.Number, float64, int:
		return true
	}
	return false
}
func RequestPreview(req *http.Request) Preview {
	copyURL := *req.URL
	q := copyURL.Query()
	for name := range q {
		if safejson.SensitiveKey(name) {
			q.Set(name, "[REDACTED]")
		}
	}
	copyURL.RawQuery = q.Encode()
	p := Preview{Method: req.Method, URL: copyURL.String(), Headers: req.Header.Clone()}
	if req.GetBody != nil {
		b, err := req.GetBody()
		if err == nil {
			defer b.Close()
			var raw bytes.Buffer
			_, _ = raw.ReadFrom(b)
			if jsonutil.Unmarshal(raw.Bytes(), &p.Body) != nil {
				p.Body = raw.String()
			}
		}
	}
	// Only non-auth data reaches this helper; redact sensitive user input as defense in depth.
	for key := range p.Headers {
		if safejson.SensitiveKey(key) {
			p.Headers.Set(key, "[REDACTED]")
		}
	}
	encoded := safejson.Marshal(p.Body, 64<<10)
	if len(encoded) > 0 {
		_ = jsonutil.Unmarshal(encoded, &p.Body)
	}
	return p
}
func Budget(raw json.RawMessage) time.Duration {
	var c ExecutionConfig
	_ = strictConfig(raw, &c)
	if c.TimeoutMs == 0 {
		return 30 * time.Second
	}
	return time.Duration(c.TimeoutMs) * time.Millisecond
}

type ResponseChecks struct {
	HTTP      string `json:"http"`
	JSON      string `json:"json"`
	Business  string `json:"business"`
	Structure string `json:"structure"`
}

func CheckResponse(runtime *model.HTTPActionRuntime, status int, body []byte, schema map[string]any) error {
	_, err := CheckResponseDetailed(runtime, status, body, schema)
	return err
}
func CheckResponseDetailed(runtime *model.HTTPActionRuntime, status int, body []byte, outputSchema map[string]any) (ResponseChecks, error) {
	checks := ResponseChecks{HTTP: "skipped", JSON: "skipped", Business: "skipped", Structure: "skipped"}

	var cfg ResponseConfig
	if err := strictConfig(runtime.ResponseConfig, &cfg); err != nil {
		return checks, err
	}
	valid := status >= 200 && status < 300
	if len(cfg.StatusCodes) > 0 {
		valid = false
		for _, s := range cfg.StatusCodes {
			if s == status {
				valid = true
			}
		}
	}
	if !valid {
		checks.HTTP = "failed"
		return checks, fmt.Errorf("HTTP status %d did not satisfy response check", status)
	}
	checks.HTTP = "passed"
	if cfg.SuccessCondition == nil && len(outputSchema) == 0 {
		return checks, nil
	}
	var data any
	if err := jsonutil.Unmarshal(body, &data); err != nil {
		checks.JSON = "failed"
		return checks, errors.New("response is not valid JSON")
	}
	checks.JSON = "passed"
	if c := cfg.SuccessCondition; c != nil {
		v, ok := jsonutil.PathLookup(data, c.Path)
		if !ok || !equalValue(v, c.Value) {
			checks.Business = "failed"
			return checks, fmt.Errorf("business condition failed at %s", c.Path)
		}
		checks.Business = "passed"
	}
	if len(outputSchema) > 0 {
		checks.Structure = "failed"
		compiler := jsonschema.NewCompiler()
		compiler.DefaultDraft(jsonschema.Draft2020)
		loc := "https://apihub.local/response"
		if err := compiler.AddResource(loc, outputSchema); err != nil {
			return checks, err
		}
		schema, err := compiler.Compile(loc)
		if err != nil {
			return checks, err
		}
		if err := schema.Validate(data); err != nil {
			return checks, SchemaFailure("outputSchema", err)
		}
		checks.Structure = "passed"
	}
	return checks, nil
}
func equalValue(a, b any) bool {
	an, aok := a.(json.Number)
	bn, bok := b.(json.Number)
	if aok && bok {
		ar, ok := new(big.Rat).SetString(an.String())
		br, ok2 := new(big.Rat).SetString(bn.String())
		return ok && ok2 && ar.Cmp(br) == 0
	}
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return bytes.Equal(ab, bb)
}

// RequestFailure records whether a provider may have received the request.
type RequestFailure struct {
	Sent bool
	Err  error
}

func (e *RequestFailure) Error() string { return "provider request failed: " + e.Err.Error() }
func (e *RequestFailure) Unwrap() error { return e.Err }
func OutcomeUnknown(err error) bool     { var e *RequestFailure; return errors.As(err, &e) && e.Sent }

// ValidateSchema rejects unsupported keywords and external references at save time.
func ValidateSchema(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var schema map[string]any
	if err := jsonutil.Unmarshal(raw, &schema); err != nil {
		return err
	}
	if err := schemaKeywords(schema); err != nil {
		return err
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	loc := "https://apihub.local/configuration"
	if err := compiler.AddResource(loc, schema); err != nil {
		return err
	}
	_, err := compiler.Compile(loc)
	return err
}
func schemaKeywords(s map[string]any) error {
	for key, value := range s {
		switch key {
		case "type", "title", "description", "default", "examples", "enum", "const", "required", "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "minLength", "maxLength", "pattern", "minItems", "maxItems", "uniqueItems", "minProperties", "maxProperties":
		case "additionalProperties":
			if nested, ok := value.(map[string]any); ok {
				if err := schemaKeywords(nested); err != nil {
					return err
				}
			} else if _, ok := value.(bool); !ok {
				return errors.New("additionalProperties must be boolean or schema")
			}
		case "properties":
			obj, ok := value.(map[string]any)
			if !ok {
				return errors.New("properties must be an object")
			}
			for _, v := range obj {
				nested, ok := v.(map[string]any)
				if !ok {
					return errors.New("property schema must be an object")
				}
				if err := schemaKeywords(nested); err != nil {
					return err
				}
			}
		case "items":
			nested, ok := value.(map[string]any)
			if !ok {
				return errors.New("items must be a schema object")
			}
			if err := schemaKeywords(nested); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported schema keyword: %s", key)
		}
	}
	return nil
}

// FieldError carries only schema paths and constraint names, never submitted values.
type FieldError struct {
	Field      string `json:"field"`
	Path       string `json:"path,omitempty"`
	Constraint string `json:"constraint,omitempty"`
	Expected   string `json:"expected,omitempty"`
}

func (e *FieldError) Error() string {
	constraint := e.Constraint
	if e.Expected != "" {
		constraint += ": " + e.Expected
	}
	return fmt.Sprintf("%s: validation failed at %s (%s)", e.Field, e.Path, constraint)
}
func SchemaFailure(field string, err error) error {
	var v *jsonschema.ValidationError
	if !errors.As(err, &v) {
		return &FieldError{Field: field, Path: "$", Constraint: "schema"}
	}
	for len(v.Causes) > 0 {
		v = v.Causes[0]
	}
	path := "$"
	for _, p := range v.InstanceLocation {
		b, _ := json.Marshal(p)
		path += "[" + string(b) + "]"
	}
	constraint := strings.Join(v.ErrorKind.KeywordPath(), ".")
	expected := ""
	switch k := v.ErrorKind.(type) {
	case *kind.Type:
		expected = strings.Join(k.Want, ", ")
	case *kind.Required:
		expected = strings.Join(k.Missing, ", ")
	}
	return &FieldError{Field: field, Path: path, Constraint: constraint, Expected: expected}
}

// ValidateInputContract prevents contradictory schema/mapping definitions and invalid defaults.
func ValidateInputContract(runtime *model.HTTPActionRuntime, raw json.RawMessage) error {
	if err := ValidateConfig(runtime); err != nil {
		return err
	}
	if err := ValidateSchema(raw); err != nil {
		return SchemaFailure("inputSchema", err)
	}
	var cfg RequestConfig
	_ = strictConfig(runtime.RequestConfig, &cfg)
	if cfg.SchemaVersion != 1 {
		return nil
	}
	var schema map[string]any
	if len(raw) > 0 {
		_ = jsonutil.Unmarshal(raw, &schema)
	}
	if len(schema) == 0 {
		return nil
	}
	if t, ok := schema["type"]; ok && t != "object" {
		return &FieldError{Field: "inputSchema", Path: "$", Constraint: "type", Expected: "object"}
	}
	props, _ := schema["properties"].(map[string]any)
	required := map[string]bool{}
	if arr, ok := schema["required"].([]any); ok {
		for _, v := range arr {
			if n, ok := v.(string); ok {
				required[n] = true
			}
		}
	}
	declared := map[string]Parameter{}
	for _, p := range cfg.Parameters {
		declared[p.Name] = p
	}
	for n := range props {
		if _, ok := declared[n]; !ok {
			return &FieldError{Field: "inputSchema", Path: n, Constraint: "unmapped property"}
		}
	}
	for n := range required {
		if _, ok := declared[n]; !ok {
			return &FieldError{Field: "inputSchema", Path: n, Constraint: "unmapped required property"}
		}
	}
	for _, p := range cfg.Parameters {
		property, ok := props[p.Name].(map[string]any)
		if !ok {
			return &FieldError{Field: "inputSchema", Path: p.Name, Constraint: "missing property mapping"}
		}
		t, ok := property["type"].(string)
		if !ok || (t != p.Type && !(p.Type == "integer" && t == "number")) {
			return &FieldError{Field: "inputSchema", Path: p.Name, Constraint: "type", Expected: p.Type}
		}
		if len(p.Default) == 0 && p.Required != required[p.Name] {
			return &FieldError{Field: "inputSchema", Path: p.Name, Constraint: "required mapping mismatch"}
		}
		for _, value := range []json.RawMessage{p.Default, p.Example} {
			if len(value) == 0 {
				continue
			}
			compiler := jsonschema.NewCompiler()
			loc := "https://apihub.local/parameter"
			if err := compiler.AddResource(loc, property); err != nil {
				return SchemaFailure("inputSchema", err)
			}
			compiled, err := compiler.Compile(loc)
			if err != nil {
				return SchemaFailure("inputSchema", err)
			}
			var data any
			_ = jsonutil.Unmarshal(value, &data)
			if err := compiled.Validate(data); err != nil {
				return &FieldError{Field: "requestConfig", Path: p.Name, Constraint: "default/example violates input schema"}
			}
		}
	}
	return nil
}
