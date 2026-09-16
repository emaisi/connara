package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"apihub-go/internal/model"
)

const maxProviderResponse = 4 << 20

var pathParameter = regexp.MustCompile(`\{([A-Za-z0-9_]+)\}`)

type Result struct {
	Status int
	Header http.Header
	Body   []byte
}

type Executor struct {
	client *http.Client
}

type RequestAuth struct {
	Headers map[string]string
	Query   map[string]string
	Cookies map[string]string
}

func New(client *http.Client) *Executor { return &Executor{client: client} }

func (e *Executor) Action(
	ctx context.Context,
	provider model.Provider,
	action model.Action,
	input map[string]any,
	credential map[string]any,
	requestAuth ...RequestAuth,
) (Result, error) {
	if action.Runtime == nil || provider.BaseURL == "" {
		return Result{}, errors.New("action is catalog-only")
	}
	method := strings.ToUpper(action.Runtime.Method)
	if method == "" {
		method = http.MethodPost
	}
	path, remaining, err := expandPath(action.Runtime.Path, input)
	if err != nil {
		return Result{}, err
	}
	target, err := joinTarget(provider.BaseURL, path)
	if err != nil {
		return Result{}, err
	}

	var body io.Reader
	if method == http.MethodGet || method == http.MethodHead {
		addQuery(target, remaining)
	} else if len(remaining) > 0 {
		data, err := json.Marshal(remaining)
		if err != nil {
			return Result{}, fmt.Errorf("encode provider request: %w", err)
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "apihub-go/0.1")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if err := injectCredential(req, provider.Credential, credential); err != nil {
		return Result{}, err
	}
	if len(requestAuth) > 0 {
		for name, value := range requestAuth[0].Headers {
			req.Header.Set(name, value)
		}
		query := req.URL.Query()
		for name, value := range requestAuth[0].Query {
			query.Set(name, value)
		}
		req.URL.RawQuery = query.Encode()
		for name, value := range requestAuth[0].Cookies {
			req.AddCookie(&http.Cookie{Name: name, Value: value, Secure: true, HttpOnly: true})
		}
	}
	return e.do(req)
}

func (e *Executor) do(req *http.Request) (Result, error) {
	response, err := e.client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("provider request failed: %w", err)
	}
	defer response.Body.Close()
	// ponytail: action responses are buffered up to 4 MiB; add file streaming only when a real action needs it.
	body, err := io.ReadAll(io.LimitReader(response.Body, maxProviderResponse+1))
	if err != nil {
		return Result{}, fmt.Errorf("read provider response: %w", err)
	}
	if len(body) > maxProviderResponse {
		return Result{}, errors.New("provider response exceeds 4 MiB")
	}
	return Result{Status: response.StatusCode, Header: response.Header.Clone(), Body: body}, nil
}

func expandPath(pattern string, input map[string]any) (string, map[string]any, error) {
	if err := ValidatePath(pattern); err != nil {
		return "", nil, err
	}
	remaining := make(map[string]any, len(input))
	for key, value := range input {
		remaining[key] = value
	}
	var invalid string
	path := pathParameter.ReplaceAllStringFunc(pattern, func(match string) string {
		key := pathParameter.FindStringSubmatch(match)[1]
		value, ok := input[key]
		if !ok {
			invalid = key
			return match
		}
		switch value.(type) {
		case string, json.Number, int, int64, float64, bool:
		default:
			invalid = key
			return match
		}
		text := fmt.Sprint(value)
		if text == "" || text == "." || text == ".." || strings.ContainsAny(text, "/\\%?#\x00\r\n") {
			invalid = key
			return match
		}
		delete(remaining, key)
		return url.PathEscape(text)
	})
	if invalid != "" {
		return "", nil, fmt.Errorf("missing or unsafe path input %q", invalid)
	}
	if err := ValidatePath(path); err != nil {
		return "", nil, err
	}
	return path, remaining, nil
}

func ValidatePath(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") ||
		parsed.IsAbs() || parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
		strings.ContainsAny(parsed.Path, "\\\x00\r\n") {
		return errors.New("action path must be an absolute path on the configured host")
	}
	for _, part := range strings.Split(parsed.Path, "/") {
		if part == "." || part == ".." || strings.Contains(part, "%") {
			return errors.New("action path must not contain encoded or literal dot segments")
		}
	}
	return nil
}

func joinTarget(base, endpoint string) (*url.URL, error) {
	if err := ValidatePath(endpoint); err != nil {
		return nil, err
	}
	baseURL, err := url.Parse(base)
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, errors.New("provider base URL is invalid")
	}
	reference, err := url.Parse(endpoint)
	if err != nil {
		return nil, errors.New("provider endpoint is invalid")
	}
	return baseURL.ResolveReference(reference), nil
}

func addQuery(target *url.URL, values map[string]any) {
	query := target.Query()
	for key, value := range values {
		switch typed := value.(type) {
		case []any:
			for _, item := range typed {
				query.Add(key, fmt.Sprint(item))
			}
		case nil:
		default:
			query.Add(key, fmt.Sprint(value))
		}
	}
	target.RawQuery = query.Encode()
}

func injectCredential(req *http.Request, spec *model.CredentialSpec, credential map[string]any) error {
	if spec == nil {
		return nil
	}
	field := spec.Field
	if field == "" {
		field = "apiKey"
	}
	value, ok := credential[field].(string)
	if !ok || strings.TrimSpace(value) == "" {
		if accessToken, exists := credential["accessToken"].(string); exists {
			value, ok = accessToken, true
		}
	}
	if !ok || strings.TrimSpace(value) == "" {
		return fmt.Errorf("connection credential %q is required", field)
	}
	switch spec.In {
	case "header":
		req.Header.Set(spec.Name, spec.Prefix+value)
	case "query":
		query := req.URL.Query()
		query.Set(spec.Name, spec.Prefix+value)
		req.URL.RawQuery = query.Encode()
	default:
		return fmt.Errorf("unsupported credential placement %q", spec.In)
	}
	return nil
}
