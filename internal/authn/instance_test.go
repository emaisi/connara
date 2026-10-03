package authn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"apihub-go/internal/executor"
	"apihub-go/internal/model"
)

func TestInstanceTokenChecksResponseWithoutPersistence(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{
		{"valid", `{"data":{"token":"secret-token","ttl":3600}}`, "", 200},
		{"wrong path", `{"access_token":"secret-token","expires_in":3600}`, "token path", 200},
		{"not JSON", `<html>login</html>`, "did not return JSON", 200},
		{"rejected", `{"error":"bad credentials"}`, "HTTP 401", 401},
		{"object token", `{"data":{"token":{"value":"secret-token"},"ttl":3600}}`, "token path", 200},
		{"null token", `{"data":{"token":null,"ttl":3600}}`, "token path", 200},
		{"blank token", `{"data":{"token":" ","ttl":3600}}`, "token path", 200},
		{"missing expiry", `{"data":{"token":"secret-token"}}`, "expiry path", 200},
		{"invalid expiry", `{"data":{"token":"secret-token","ttl":"tomorrow"}}`, "expiry path", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["username"] != "tester" || body["password"] != "test-password" {
					t.Errorf("incorrect test request")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			// A nil store/cache proves the test path never persists credentials or takes a connection lock.
			service := New(nil, nil, nil, executor.NewGuardedClient())
			credentials := map[string]any{"username": "tester", "password": "test-password"}
			err := service.TestInstanceToken(context.Background(), model.AuthInstance{AuthTemplateFlow: "password_token", TokenURL: server.URL, TokenPath: "$.data.token", ExpiryPath: "$.data.ttl", PublicConfig: json.RawMessage(`{}`)}, model.AuthTemplate{TokenRequest: json.RawMessage(`{"method":"POST","bodyType":"json"}`)}, credentials)
			if tc.want == "" && err != nil {
				t.Fatal(err)
			}
			if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
			if _, ok := credentials["access_token"]; ok {
				t.Fatal("test mutated supplied credentials")
			}
		})
	}
}

func TestInstanceConfigurationRejectsMalformedPaths(t *testing.T) {
	for _, path := range []string{"$.data.", "$.data[", "$.data[-1]"} {
		err := ValidateInstanceConfiguration(model.AuthInstance{AuthTemplateFlow: "password_token", TokenURL: "http://internal.example/login", TokenPath: path, PublicConfig: json.RawMessage(`{}`)})
		if err == nil {
			t.Fatalf("accepted malformed path %q", path)
		}
	}
}
