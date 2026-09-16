package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"apihub-go/internal/model"
)

func TestRemoteIPUsesTCPPeer(t *testing.T) {
	for _, test := range []struct{ address, want string }{
		{"192.0.2.10:43210", "192.0.2.10"},
		{"[2001:db8::10]:43210", "2001:db8::10"},
		{"2001:db8::10", "2001:db8::10"},
	} {
		request := httptest.NewRequest("GET", "http://example.test", nil)
		request.RemoteAddr = test.address
		request.Header.Set("X-Forwarded-For", "203.0.113.99")
		if got := remoteIP(request); got != test.want {
			t.Fatalf("remoteIP(%q)=%q, want %q", test.address, got, test.want)
		}
	}
}

func TestSafeReturnPath(t *testing.T) {
	for _, test := range []struct{ input, want string }{{"/connections", "/connections"}, {"/connections?tab=oauth", "/connections?tab=oauth"}, {"https://evil.example", ""}, {"//evil.example", ""}, {`/\\evil`, ""}} {
		if got := safeReturnPath(test.input); got != test.want {
			t.Fatalf("safeReturnPath(%q)=%q, want %q", test.input, got, test.want)
		}
	}
}

func TestRuntimeBytesDoesNotLeakSuccessShapeOnError(t *testing.T) {
	var value map[string]any
	if err := json.Unmarshal(runtimeBytes(nil, nil, "failed", "boom"), &value); err != nil {
		t.Fatal(err)
	}
	if value["success"] != false || value["errorCode"] != "failed" {
		t.Fatalf("unexpected runtime error: %#v", value)
	}
}

func TestWebhookPattern(t *testing.T) {
	if !matchesAny([]string{"sync.*"}, "sync.completed") || matchesAny([]string{"sync.*"}, "action.completed") {
		t.Fatal("event matching is incorrect")
	}
}

func TestValidInjectionName(t *testing.T) {
	for _, item := range [][2]string{{"header", "Authorization"}, {"header", "X-Tenant-ID"}, {"query", "api_key"}, {"cookie", "session.id"}} {
		if !validInjectionName(item[0], item[1]) {
			t.Fatalf("expected valid injection name %q/%q", item[0], item[1])
		}
	}
	for _, item := range [][2]string{{"header", "Host"}, {"header", "Content-Length"}, {"body", "token"}, {"query", "bad name"}, {"cookie", "名称"}} {
		if validInjectionName(item[0], item[1]) {
			t.Fatalf("expected invalid injection name %q/%q", item[0], item[1])
		}
	}
}

func TestValidPublicBaseURL(t *testing.T) {
	for _, value := range []string{"https://hub.example.com", "http://127.0.0.1:8080", "http://localhost:8080"} {
		if !validPublicBaseURL(value) {
			t.Fatalf("expected valid public base URL %q", value)
		}
	}
	for _, value := range []string{"http://hub.example.com", "https://user:pass@hub.example.com", "https://hub.example.com/#fragment"} {
		if validPublicBaseURL(value) {
			t.Fatalf("expected invalid public base URL %q", value)
		}
	}
}

func TestAllowedActionsFiltersCatalog(t *testing.T) {
	items := []model.ActionDefinition{
		{ActionKey: "github.list", Status: "active"},
		{ActionKey: "github.delete", Status: "active"},
		{ActionKey: "slack.send", Status: "active"},
		{ActionKey: "github.draft", Status: "draft"},
	}
	token := model.RuntimeToken{AllowedActions: []string{"github.*"}, BlockedActions: []string{"github.delete"}}
	visible := allowedActions(items, token)
	if len(visible) != 1 || visible[0].ActionKey != "github.list" {
		t.Fatalf("unexpected visible actions: %#v", visible)
	}
}

func TestAuditJSONRedactsNestedSecrets(t *testing.T) {
	encoded := auditJSON(map[string]any{
		"username": "alice",
		"password": "plain-password",
		"nested": map[string]any{
			"access_token":  "plain-token",
			"Authorization": "Bearer plain",
			"tokenCount":    3,
		},
	})
	var value map[string]any
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	if value["username"] != "alice" || value["password"] != "[REDACTED]" {
		t.Fatalf("unexpected top-level audit value: %#v", value)
	}
	nested := value["nested"].(map[string]any)
	if nested["access_token"] != "[REDACTED]" || nested["Authorization"] != "[REDACTED]" {
		t.Fatalf("nested secrets were not redacted: %#v", nested)
	}
	if nested["tokenCount"] != float64(3) {
		t.Fatalf("non-secret metadata was redacted: %#v", nested)
	}
}
