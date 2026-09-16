package authn

import (
	"strings"
	"testing"
)

func TestLookupJSONPathAndTemplate(t *testing.T) {
	value := map[string]any{"data": map[string]any{"token": "abc"}}
	token, ok := lookupJSONPath(value, "$.data.token")
	if !ok || token != "abc" {
		t.Fatalf("unexpected token: %v %v", token, ok)
	}
	header, err := renderTemplate("Bearer {{token}}", map[string]any{"access_token": "abc"})
	if err != nil || header != "Bearer abc" {
		t.Fatalf("unexpected header: %q %v", header, err)
	}
}

func TestSupportedAuthenticationFlows(t *testing.T) {
	for _, flow := range []string{"none", "static", "password_token", "client_credentials", "oauth2_code", "gateway"} {
		if !SupportsFlow(flow) {
			t.Fatalf("expected %q to be supported", flow)
		}
	}
	for _, flow := range []string{"", "oauth1", "saml", "mtls", "jwt_signing"} {
		if SupportsFlow(flow) {
			t.Fatalf("expected %q to require an extension", flow)
		}
	}
}

func TestRenderTemplateRequiresEveryCredential(t *testing.T) {
	value, err := renderTemplate("Bearer {{token}} / {{tenant_id}}", map[string]any{
		"access_token": "abc",
		"tenant_id":    "north",
	})
	if err != nil || value != "Bearer abc / north" {
		t.Fatalf("unexpected rendered value %q: %v", value, err)
	}
	_, err = renderTemplate("{{missing}}", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected missing credential error, got %v", err)
	}
}
