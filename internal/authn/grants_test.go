package authn

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"apihub-go/internal/rediscache"
	"apihub-go/internal/secret"
	"apihub-go/internal/testutil"
)

func TestNewTokenGrantsRefreshAndPersist(t *testing.T) {
	address := os.Getenv("APIHUB_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("isolated Redis required")
	}
	for _, tc := range []struct {
		name, template string
		config         string
		credentials    func(*testing.T) map[string]any
		check          func(*testing.T, url.Values)
	}{
		{
			name: "JWT Bearer grant", template: "jwt-bearer-grant", config: `{"issuer":"service@example.com","audience":"https://id.example.com/token","scope":"read"}`,
			credentials: func(t *testing.T) map[string]any { _, key := testRSAPEM(t); return map[string]any{"private_key": key} },
			check: func(t *testing.T, form url.Values) {
				if form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" || form.Get("assertion") == "" || form.Get("scope") != "read" {
					t.Fatal("incorrect JWT grant fields")
				}
			},
		},
		{
			name: "Token Exchange", template: "oauth2-token-exchange", config: `{"subjectTokenType":"urn:ietf:params:oauth:token-type:jwt","requestedTokenType":"urn:ietf:params:oauth:token-type:access_token","audience":"orders","resource":"https://orders.example.com"}`,
			credentials: func(*testing.T) map[string]any { return map[string]any{"subject_token": "subject-1"} },
			check: func(t *testing.T, form url.Values) {
				if form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:token-exchange" || form.Get("subject_token") != "subject-1" || form.Get("subject_token_type") != "urn:ietf:params:oauth:token-type:jwt" || form.Get("requested_token_type") != "urn:ietf:params:oauth:token-type:access_token" || form.Get("audience") != "orders" || form.Get("resource") != "https://orders.example.com" {
					t.Fatal("incorrect token exchange fields")
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db := testutil.Database(t)
			fixture := testutil.Seed(t, db)
			cache, err := rediscache.Open(ctx, address, "", 0)
			if err != nil {
				t.Fatal(err)
			}
			defer cache.Close()
			codec, err := secret.New(bytes.Repeat([]byte{8}, 32))
			if err != nil {
				t.Fatal(err)
			}
			var requests int
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				requests++
				if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
					t.Fatalf("incorrect token request: %s %s", request.Method, request.Header.Get("Content-Type"))
				}
				if err := request.ParseForm(); err != nil {
					t.Fatal(err)
				}
				tc.check(t, request.PostForm)
				w.Header().Set("Content-Type", "application/json")
				if tc.template == "oauth2-token-exchange" {
					_, _ = w.Write([]byte(`{"access_token":"issued-token","issued_token_type":"urn:ietf:params:oauth:token-type:access_token","expires_in":3600}`))
				} else {
					_, _ = w.Write([]byte(`{"access_token":"issued-token","expires_in":3600}`))
				}
			}))
			defer upstream.Close()
			template, err := db.AuthTemplate(ctx, tc.template)
			if err != nil {
				t.Fatal(err)
			}
			testutil.Exec(t, db, `UPDATE auth_instances SET auth_template_id=$2,token_url=$3,token_path='$.access_token',public_config=$4 WHERE id=$1`, fixture.Auth.ID, template.ID, upstream.URL, json.RawMessage(tc.config))
			service := New(db, codec, cache, upstream.Client())
			blob, err := service.SealCredentials(fixture.Connection.ID, 1, tc.credentials(t))
			if err != nil {
				t.Fatal(err)
			}
			testutil.Exec(t, db, `UPDATE connections SET credential_blob=$2,revision=1,status='active' WHERE id=$1`, fixture.Connection.ID, blob)
			resolved, err := service.Resolve(ctx, fixture.Connection.ID)
			if err != nil {
				t.Fatal(err)
			}
			if resolved.Credentials["access_token"] != "issued-token" || requests != 1 {
				t.Fatalf("token was not issued exactly once: calls=%d", requests)
			}
			stored, err := db.Connection(ctx, fixture.Connection.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.TokenExpiresAt == nil || stored.Revision != 2 {
				t.Fatalf("token was not persisted with expiry: %+v", stored)
			}
		})
	}
}
