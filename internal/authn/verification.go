package authn

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"apihub-go/internal/executor"
	"apihub-go/internal/jsonutil"
	"apihub-go/internal/model"
)

type AuthStageError struct {
	Stage string
	Err   error
}

func (e *AuthStageError) Error() string { return e.Err.Error() }
func (e *AuthStageError) Unwrap() error { return e.Err }
func safeRequestError(err error) error {
	var urlError *url.Error
	if errors.As(err, &urlError) {
		return fmt.Errorf("token request failed: %w", urlError.Err)
	}
	return fmt.Errorf("token request failed: %w", err)
}

type TestStep struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Code   string `json:"code,omitempty"`
}
type TestResult struct {
	Configured      bool              `json:"configured"`
	Authenticated   bool              `json:"authenticated"`
	APIVerified     bool              `json:"apiVerified"`
	Steps           []TestStep        `json:"steps"`
	Message         string            `json:"message"`
	EffectiveConfig map[string]string `json:"effectiveConfig,omitempty"`
}

func (s *Service) resolveTest(instance model.AuthInstance, template model.AuthTemplate, values map[string]any) (Resolved, error) {
	if instance.AuthTemplateKey == "basic" {
		values["basic_token"] = base64.StdEncoding.EncodeToString([]byte(firstString(values, "username") + ":" + firstString(values, "password")))
	}
	advanced, err := ParseAuthRequest(instance)
	if err != nil {
		return Resolved{}, err
	}
	rules := []InjectionRule{}
	if err := jsonutil.Unmarshal(template.InjectionRules, &rules); err != nil {
		return Resolved{}, err
	}
	resolved := Resolved{Credentials: values, Headers: map[string]string{}, Query: map[string]string{}, Cookies: map[string]string{}}
	if advanced.InjectionOverride != nil {
		rules = *advanced.InjectionOverride
	} else if instance.HeaderName != "" {
		value, err := renderTemplate(instance.HeaderValueTemplate, values)
		if err != nil {
			return resolved, err
		}
		resolved.Headers[instance.HeaderName] = value
		resolved.HeaderName = instance.HeaderName
		resolved.HeaderValue = value
	}
	for _, rule := range rules {
		value, err := renderTemplate(rule.Template, values)
		if err != nil {
			return resolved, err
		}
		switch rule.Target {
		case "header":
			resolved.Headers[rule.Name] = value
		case "query":
			resolved.Query[rule.Name] = value
		case "cookie":
			resolved.Cookies[rule.Name] = value
		}
	}
	if err := s.applyAdvanced(instance, values, &resolved); err != nil {
		return resolved, err
	}
	return resolved, nil
}
func (s *Service) TestConfiguration(ctx context.Context, instance model.AuthInstance, template model.AuthTemplate, credentials map[string]any, verifyAPI bool, baseURL string) (TestResult, error) {
	result := TestResult{Configured: true, Steps: []TestStep{{"configuration", "passed", ""}, {"token_request", "skipped", ""}, {"token_response", "skipped", ""}, {"api_verification", "skipped", ""}}}
	if err := ValidateInstanceConfiguration(instance); err != nil {
		result.Configured = false
		result.Steps[0].Status = "failed"
		return result, err
	}
	if err := ValidateEffectiveConfiguration(instance, template); err != nil {
		result.Configured = false
		result.Steps[0].Status = "failed"
		return result, err
	}
	if err := s.ValidateRequestSecrets(instance, template); err != nil {
		result.Configured = false
		result.Steps[0].Status = "failed"
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	effective, _ := EffectiveRequest(instance, template)
	result.EffectiveConfig = map[string]string{"method": effective.Method, "bodyType": effective.BodyType, "tokenPath": instance.TokenPath}
	values, _, err := s.advancedCredentials(instance, credentials)
	if err != nil {
		return result, err
	}
	if template.Source == "custom" || instance.AuthTemplateFlow == "password_token" || instance.AuthTemplateFlow == "static" || instance.AuthTemplateFlow == "client_credentials" {
		if err := ValidateCredentials(template, values); err != nil {
			return result, err
		}
	}
	switch instance.AuthTemplateFlow {
	case "password_token", "client_credentials", "jwt_bearer_grant", "token_exchange":
		result.Steps[1].Status = "failed"
		_, values, err = s.requestInstanceToken(ctx, model.Connection{}, instance, template, credentials, true)
		if err != nil {
			var failure *AuthStageError
			if errors.As(err, &failure) && failure.Stage == "token_response" {
				result.Steps[1].Status = "passed"
				result.Steps[2].Status = "failed"
			}
			return result, err
		}
		result.Steps[1].Status = "passed"
		result.Steps[2].Status = "passed"
		result.Authenticated = true
	case "static", "none", "mtls", "aws_sigv4", "jwt_direct", "gateway":
	default:
		return result, errors.New("this authentication flow requires browser authorization")
	}
	resolved, err := s.resolveTest(instance, template, values)
	if err != nil {
		result.Steps[2].Status = "failed"
		return result, err
	}
	if verifyAPI {
		result.Steps[3].Status = "failed"
		if baseURL == "" {
			return result, errors.New("select an integration or provide an API base URL for verification")
		}
		if err := s.verifyResolved(ctx, instance, baseURL, resolved); err != nil {
			return result, err
		}
		result.Steps[3].Status = "passed"
		result.APIVerified = true
	}
	result.Message = "测试完成；测试凭据和令牌未保存"
	return result, nil
}
func (s *Service) verifyResolved(ctx context.Context, instance model.AuthInstance, baseURL string, resolved Resolved) error {
	var public struct {
		VerificationPath string `json:"verificationPath"`
	}
	if err := jsonutil.Unmarshal(instance.PublicConfig, &public); err != nil {
		return err
	}
	advanced, err := ParseAuthRequest(instance)
	if err != nil {
		return err
	}
	if public.VerificationPath == "" || (advanced.SchemaVersion == 2 && !advanced.Verification.Enabled) {
		return errors.New("API verification is not configured")
	}
	if err := executor.ValidatePath(public.VerificationPath); err != nil {
		return err
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !httpScheme(base) || base.User != nil || base.Fragment != "" {
		return errors.New("invalid API base URL")
	}
	target, err := executor.BuildRequest(baseURL, &model.HTTPActionRuntime{Method: "GET", Path: public.VerificationPath}, nil)
	if err != nil {
		return err
	}
	endpoint := target.URL.String()
	config := RequestConfig{SchemaVersion: 2, Method: "GET", BodyType: "none", CredentialMode: "mapped"}
	if advanced.Verification.Request != nil {
		config = *advanced.Verification.Request
	}
	request, err := buildAuthRequest(ctx, endpoint, config, nil, resolved.Credentials, nil)
	if err != nil {
		return err
	}
	query := request.URL.Query()
	for key, value := range resolved.Query {
		if query.Has(key) {
			return errors.New("authentication query conflicts with verification parameter")
		}
		query.Set(key, value)
	}
	request.URL.RawQuery = query.Encode()
	for key, value := range resolved.Headers {
		if request.Header.Get(key) != "" {
			return errors.New("authentication header conflicts with verification header")
		}
		request.Header.Set(key, value)
	}
	for key, value := range resolved.Cookies {
		request.AddCookie(&http.Cookie{Name: key, Value: value})
	}
	if resolved.Sign != nil {
		if err := resolved.Sign(ctx, request); err != nil {
			return err
		}
	}
	client, cleanupTLS, err := executor.ClientWithTLS(s.client, resolved.TLS)
	if err != nil {
		return err
	}
	defer cleanupTLS()
	client, cleanup, err := executor.ConfiguredEndpointClient(client, instance.TokenURL, instance.RefreshURL, baseURL)
	if err != nil {
		return err
	}
	defer cleanup()
	response, err := client.Do(request)
	if err != nil {
		return errors.New("API verification connection failed; check the address and upstream service")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("verification endpoint returned HTTP %d", response.StatusCode)
	}
	if advanced.Verification.SuccessCondition != nil {
		raw, err := io.ReadAll(io.LimitReader(response.Body, maxTokenResponse+1))
		if err != nil {
			return err
		}
		if len(raw) > maxTokenResponse {
			return errors.New("verification response exceeds 1 MiB")
		}
		var value any
		if err := jsonutil.Unmarshal(raw, &value); err != nil {
			return errors.New("verification response must be JSON")
		}
		if !matchesCondition(value, advanced.Verification.SuccessCondition) {
			return errors.New("API verification response does not satisfy the success condition")
		}
	}
	return nil
}
