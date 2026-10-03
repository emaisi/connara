package authn

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"apihub-go/internal/model"
)

func TestConfiguredLoginRequestAndResponse(t *testing.T) {
	for _, method := range []string{"GET", "POST-json", "POST-form"} {
		t.Run(method, func(t *testing.T) {
			config := RequestConfig{SchemaVersion: 2, Method: "POST", BodyType: "json", CredentialMode: "all"}
			if method == "GET" {
				config.Method = "GET"
				config.BodyType = "none"
			}
			if method == "POST-form" {
				config.BodyType = "form"
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				switch method {
				case "GET":
					if r.Method != "GET" || r.URL.Query().Get("username") != "张 三&" || len(raw) != 0 {
						t.Errorf("incorrect GET request")
					}
				case "POST-form":
					if r.Method != "POST" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || strings.Contains(string(raw), "grant_type") {
						t.Errorf("ordinary form became OAuth")
					}
				case "POST-json":
					var body map[string]any
					_ = json.Unmarshal(raw, &body)
					if body["username"] != "张 三&" {
						t.Errorf("incorrect JSON")
					}
				}
				_, _ = w.Write([]byte(`{"code":0,"data":{"token":"ok","ttl":120}}`))
			}))
			defer server.Close()
			requestJSON, _ := json.Marshal(config)
			template := model.AuthTemplate{FlowType: "password_token", CredentialSchema: json.RawMessage(`{"type":"object","fields":[{"name":"username"},{"name":"password","secret":true}]}`), TokenRequest: requestJSON, InjectionRules: json.RawMessage(`[{"target":"header","name":"Authorization","template":"Bearer {{token}}"}]`)}
			instance := model.AuthInstance{AuthTemplateFlow: "password_token", TokenURL: server.URL, TokenPath: "$.data.token", ExpiryPath: "$.data.ttl", PublicConfig: json.RawMessage(`{"authRequest":{"schemaVersion":2,"response":{"successCondition":{"path":"$.code","operator":"equals","value":0},"expiry":{"mode":"field","format":"duration_seconds"}},"refresh":{"mode":"relogin"},"verification":{"enabled":false}}}`)}
			service := New(nil, nil, nil, server.Client())
			result, err := service.TestConfiguration(context.Background(), instance, template, map[string]any{"username": "张 三&", "password": "secret"}, false, "")
			if err != nil || !result.Authenticated || result.APIVerified {
				t.Fatalf("test result %+v: %v", result, err)
			}
		})
	}
}
func TestMappedRequestRejectsConflictsAndPreservesTypes(t *testing.T) {
	fields := []CredentialField{{Name: "username"}}
	config := RequestConfig{SchemaVersion: 2, Method: "POST", BodyType: "json", CredentialMode: "mapped", Parameters: []RequestParameter{{Name: "login.account", Target: "body", Value: RequestValue{Source: "credential", Name: "username"}}, {Name: "enabled", Target: "body", Value: RequestValue{Source: "literal", Value: false}}}}
	if err := validateRequest(config, fields, "login"); err != nil {
		t.Fatal(err)
	}
	req, err := buildAuthRequest(context.Background(), "https://example.com/login", config, fields, map[string]any{"username": "user"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(req.Body)
	if !strings.Contains(string(raw), `"enabled":false`) || !strings.Contains(string(raw), `"login":{"account":"user"}`) {
		t.Fatal(string(raw))
	}
	config.Parameters = append(config.Parameters, RequestParameter{Name: "login", Target: "body", Value: RequestValue{Source: "literal", Value: "bad"}})
	if err := validateRequest(config, fields, "login"); err == nil {
		t.Fatal("path conflict accepted")
	}
	config.Parameters = nil
	config.Headers = []RequestHeader{{Name: "Host", Value: RequestValue{Source: "literal", Value: "other"}}}
	if err := validateRequest(config, fields, "login"); err == nil {
		t.Fatal("Host accepted")
	}
	config.Headers = nil
	config.Method = "GET"
	config.BodyType = "none"
	config.Parameters = []RequestParameter{{Name: "user", Target: "query", Value: RequestValue{Source: "literal", Value: "new"}}}
	if _, err := buildAuthRequest(context.Background(), "https://example.com/?user=old", config, fields, nil, nil); err == nil {
		t.Fatal("query conflict accepted")
	}
}
func TestExpiryFormatsAndConditions(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		format string
		value  any
	}{{"duration_seconds", json.Number("120")}, {"duration_milliseconds", "120000"}, {"unix_seconds", now.Add(time.Hour).Unix()}, {"unix_milliseconds", now.Add(time.Hour).UnixMilli()}, {"rfc3339", now.Add(time.Hour).Format(time.RFC3339)}} {
		result, err := responseExpiry(map[string]any{"ttl": tc.value}, "$.ttl", ExpiryConfig{Mode: "field", Format: tc.format}, now)
		if err != nil || result == nil || !result.After(now) {
			t.Fatalf("%s: %v", tc.format, err)
		}
	}
	for _, value := range []any{nil, "", -1, 1.5, "1e3", "9223372036854775808"} {
		if _, err := responseExpiry(map[string]any{"ttl": value}, "$.ttl", ExpiryConfig{Mode: "field", Format: "duration_seconds"}, now); err == nil {
			t.Fatalf("invalid expiry accepted: %v", value)
		}
	}
	if matchesCondition(map[string]any{"code": "0"}, &SuccessCondition{Path: "$.code", Operator: "equals", Value: json.Number("0")}) {
		t.Fatal("condition coerces types")
	}
}
func TestConfigurationStrictAndVerificationFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing token")
		}
		_, _ = w.Write([]byte(`{"code":401}`))
	}))
	defer server.Close()
	service := New(nil, nil, nil, server.Client())
	instance := model.AuthInstance{PublicConfig: json.RawMessage(`{"verificationPath":"/me","authRequest":{"schemaVersion":2,"response":{"expiry":{"mode":"none"}},"refresh":{"mode":"relogin"},"verification":{"enabled":true,"request":{"schemaVersion":2,"method":"POST","bodyType":"json","credentialMode":"mapped"},"successCondition":{"path":"$.code","operator":"equals","value":0}}}}`)}
	err := service.verifyResolved(context.Background(), instance, server.URL, Resolved{Credentials: map[string]any{}, Headers: map[string]string{"Authorization": "Bearer test-token"}})
	if err == nil || !strings.Contains(err.Error(), "success condition") {
		t.Fatalf("business failure accepted: %v", err)
	}
	instance.PublicConfig = json.RawMessage(`{"authRequest":{"schemaVersion":99}}`)
	if _, err := ParseAuthRequest(instance); err == nil {
		t.Fatal("unknown version accepted")
	}
	instance.PublicConfig = json.RawMessage(`{"authRequest":{"schemaVersion":2,"unknown":1}}`)
	if _, err := ParseAuthRequest(instance); err == nil {
		t.Fatal("unknown field accepted")
	}
}

func TestTokenResponseFailureStepAndNoCredentialMutation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"token":"new-token","ttl":"bad"}`))
	}))
	defer server.Close()
	template := model.AuthTemplate{FlowType: "password_token", CredentialSchema: json.RawMessage(`{"type":"object","fields":[{"name":"username"}]}`), TokenRequest: json.RawMessage(`{"schemaVersion":2,"method":"POST","bodyType":"json","credentialMode":"all"}`), InjectionRules: json.RawMessage(`[]`)}
	instance := model.AuthInstance{AuthTemplateFlow: "password_token", TokenURL: server.URL, TokenPath: "$.token", ExpiryPath: "$.ttl", PublicConfig: json.RawMessage(`{"authRequest":{"schemaVersion":2,"response":{"expiry":{"mode":"field","format":"duration_seconds"}},"refresh":{"mode":"relogin"},"verification":{"enabled":false}}}`)}
	credentials := map[string]any{"username": "user"}
	service := New(nil, nil, nil, server.Client())
	result, err := service.TestConfiguration(context.Background(), instance, template, credentials, false, "")
	if err == nil || result.Steps[1].Status != "passed" || result.Steps[2].Status != "failed" || result.Authenticated {
		t.Fatalf("incorrect failure stage: %+v %v", result, err)
	}
	if credentials["token"] != nil {
		t.Fatal("failed test modified input")
	}
	_, _, runtimeErr := service.requestInstanceToken(context.Background(), model.Connection{}, instance, template, credentials, false)
	if runtimeErr == nil || runtimeErr.Error() != err.Error() {
		t.Fatalf("test and runtime differ: %v, %v", err, runtimeErr)
	}
}

func TestDraftAllowsMissingValuesButNotInvalidRequestStructure(t *testing.T) {
	template := model.AuthTemplate{FlowType: "password_token", CredentialSchema: json.RawMessage(`{"type":"object","fields":[]}`), TokenRequest: json.RawMessage(`{"schemaVersion":2,"method":"POST","bodyType":"json","credentialMode":"all"}`), InjectionRules: json.RawMessage(`[]`)}
	instance := model.AuthInstance{AuthTemplateFlow: "password_token", PublicConfig: json.RawMessage(`{"authRequest":{"schemaVersion":2,"response":{"expiry":{"mode":"field","format":"duration_seconds"}},"refresh":{"mode":"refresh_token"},"verification":{"enabled":true}}}`)}
	if err := ValidateDraftConfiguration(instance, template); err != nil {
		t.Fatalf("incomplete draft rejected: %v", err)
	}
	if err := ValidateEffectiveConfiguration(instance, template); err == nil {
		t.Fatal("incomplete ready config accepted")
	}
	instance.PublicConfig = json.RawMessage(`{"authRequest":{"schemaVersion":2,"requestOverride":{"schemaVersion":2,"method":"DELETE","bodyType":"none","credentialMode":"all"},"response":{"expiry":{"mode":"none"}},"refresh":{"mode":"relogin"},"verification":{"enabled":false}}}`)
	if err := ValidateDraftConfiguration(instance, template); err == nil {
		t.Fatal("invalid method allowed in draft")
	}
}

func TestResponseArrayPathsAndSecretHeaders(t *testing.T) {
	response := map[string]any{"tokens": []any{map[string]any{"access.token": "value", "ttl": json.Number("120")}}}
	value, ok := lookupJSONPath(response, `$.tokens[0]["access.token"]`)
	if !ok || value != "value" {
		t.Fatal("response array or quoted path not resolved")
	}
	if _, err := responseExpiry(response, "$.tokens[0].ttl", ExpiryConfig{Mode: "field", Format: "duration_seconds"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	config := RequestConfig{SchemaVersion: 2, Method: "POST", BodyType: "json", CredentialMode: "mapped", Headers: []RequestHeader{{Name: "Authorization", Value: RequestValue{Source: "literal", Value: "secret"}}}}
	if err := validateRequest(config, nil, "login"); err == nil {
		t.Fatal("plaintext secret header accepted")
	}
	config.Headers[0].Value = RequestValue{Source: "instance_secret", Name: "api_key"}
	if err := validateRequest(config, nil, "login"); err != nil {
		t.Fatal(err)
	}
}
