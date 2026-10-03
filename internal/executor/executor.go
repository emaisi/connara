package executor

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"

	"apihub-go/internal/buildinfo"
	"apihub-go/internal/model"
)

const maxProviderResponse = 4 << 20

var pathParameter = regexp.MustCompile(`\{([A-Za-z0-9_]+)\}`)

type Result struct {
	Request *Preview
	Status  int
	Header  http.Header
	Body    []byte
}

type Executor struct {
	client *http.Client
}

type RequestAuth struct {
	Headers map[string]string
	Query   map[string]string
	Cookies map[string]string
	Sign    func(context.Context, *http.Request) error
	TLS     *tls.Config
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
	req, err := BuildRequest(provider.BaseURL, action.Runtime, input)
	if err != nil {
		return Result{}, err
	}
	req = req.WithContext(ctx)
	req.Header.Set("User-Agent", buildinfo.UserAgent)
	if err := injectCredential(req, provider.Credential, credential); err != nil {
		return Result{}, err
	}
	logRequest := req.Clone(ctx)
	logRequest.Header = req.Header.Clone()
	signedQueryFields := []string{}
	client := e.client
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
		if requestAuth[0].Sign != nil {
			beforeSigning := req.Header.Clone()
			beforeQuery := req.URL.Query()
			if err := requestAuth[0].Sign(ctx, req); err != nil {
				return Result{}, fmt.Errorf("sign provider request: %w", err)
			}
			for name, values := range req.URL.Query() {
				if strings.Join(values, "\n") != strings.Join(beforeQuery[name], "\n") {
					signedQueryFields = append(signedQueryFields, name)
				}
			}
			for name, values := range req.Header {
				if strings.Join(values, "\n") != strings.Join(beforeSigning.Values(name), "\n") {
					logRequest.Header.Set(name, "[REDACTED]")
				}
			}
			copy := *client
			copy.CheckRedirect = func(*http.Request, []*http.Request) error {
				return errors.New("signed provider requests cannot redirect")
			}
			client = &copy
		}
		if requestAuth[0].TLS != nil {
			var cleanup func()
			var err error
			client, cleanup, err = ClientWithTLS(client, requestAuth[0].TLS)
			if err != nil {
				return Result{}, err
			}
			defer cleanup()
		}
	}
	client, cleanup, err := ConfiguredEndpointClient(client, provider.BaseURL)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("provider redirects are disabled") }
	// Record the final constructed request, with configured authentication fields masked.
	masked := req.Clone(ctx)
	masked.Header = req.Header.Clone()
	query := masked.URL.Query()
	if provider.Credential != nil {
		if provider.Credential.In == "header" {
			masked.Header.Set(provider.Credential.Name, "[REDACTED]")
		}
		if provider.Credential.In == "query" {
			query.Set(provider.Credential.Name, "[REDACTED]")
		}
	}
	if len(requestAuth) > 0 {
		for name := range requestAuth[0].Headers {
			masked.Header.Set(name, "[REDACTED]")
		}
		for name := range requestAuth[0].Query {
			query.Set(name, "[REDACTED]")
		}
		if len(requestAuth[0].Cookies) > 0 {
			masked.Header.Set("Cookie", "[REDACTED]")
		}
	}
	for name, values := range logRequest.Header {
		if len(values) == 1 && values[0] == "[REDACTED]" {
			masked.Header.Set(name, "[REDACTED]")
		}
	}
	for _, name := range signedQueryFields {
		query.Set(name, "[REDACTED]")
	}
	masked.URL.RawQuery = query.Encode()
	preview := RequestPreview(masked)
	result, callErr := New(client).do(req)
	result.Request = &preview
	return result, callErr
}

func (e *Executor) do(req *http.Request) (Result, error) {
	var sent atomic.Bool
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{WroteHeaders: func() { sent.Store(true) }}))
	response, err := e.client.Do(req)
	if err != nil {
		var u *url.Error
		if errors.As(err, &u) {
			err = u.Err
		}
		return Result{}, &RequestFailure{Sent: sent.Load(), Err: err}
	}
	defer response.Body.Close()
	// ponytail: action responses are buffered up to 4 MiB; add file streaming only when a real action needs it.
	body, err := io.ReadAll(io.LimitReader(response.Body, maxProviderResponse+1))
	result := Result{Status: response.StatusCode, Header: response.Header.Clone(), Body: body}
	if err != nil {
		return result, &RequestFailure{Sent: true, Err: fmt.Errorf("read provider response: %w", err)}
	}
	if len(body) > maxProviderResponse {
		result.Body = nil
		return result, errors.New("provider response exceeds 4 MiB")
	}
	return result, nil
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
	if (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, errors.New("invalid base URL")
	}
	escaped := strings.TrimRight(baseURL.EscapedPath(), "/") + reference.EscapedPath()
	baseURL.Path = strings.TrimRight(baseURL.Path, "/") + reference.Path
	baseURL.RawPath = escaped
	baseURL.RawQuery = reference.RawQuery
	return baseURL, nil
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
