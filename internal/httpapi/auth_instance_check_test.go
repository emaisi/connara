package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"apihub-go/internal/authn"
	"apihub-go/internal/executor"
	"apihub-go/internal/secret"
	"apihub-go/internal/testutil"
)

func TestCheckUnsavedAuthInstance(t *testing.T) {
	db := testutil.Database(t)
	f := testutil.Seed(t, db)
	template, err := db.AuthTemplate(context.Background(), "username-password-token")
	if err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, db, `INSERT INTO system_auth_templates(id,workspace_id,system_id,auth_template_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, testutil.ID("binding"), db.WorkspaceID(), f.System.ID, template.ID)
	calls := 0
	response := `{"data":{"token":"do-not-return-this-token","ttl":3600}}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; _, _ = w.Write([]byte(response)) }))
	defer upstream.Close()
	codec, _ := secret.New(bytes.Repeat([]byte{3}, 32))
	a := &api{Dependencies: Dependencies{Store: db, Codec: codec, Auth: authn.New(db, codec, nil, executor.NewGuardedClient()), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	before, _ := db.ListAuthInstances(context.Background(), "")
	body := map[string]any{"instanceKey": "unsaved-test", "name": "Unsaved", "systemId": f.System.ID, "authTemplateId": template.ID, "status": "ready", "tokenUrl": upstream.URL, "tokenPath": "$.data.token", "expiryPath": "$.data.ttl", "publicConfig": map[string]any{}, "testCredentials": map[string]any{"username": "test", "password": "secret"}}
	check := func(status int, want string) {
		t.Helper()
		raw, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		a.checkAuthInstance(w, httptest.NewRequest("POST", "/api/auth-instances/check", bytes.NewReader(raw)))
		if w.Code != status || !strings.Contains(w.Body.String(), want) {
			t.Fatalf("got %d %s; want %d %s", w.Code, w.Body.String(), status, want)
		}
		if strings.Contains(w.Body.String(), "do-not-return-this-token") {
			t.Fatal("returned token in test response")
		}
	}
	check(200, `"configured":true`)
	if calls != 0 {
		t.Fatal("configuration-only check made a request")
	}
	body["authenticate"] = true
	body["tokenPath"] = "$.data["
	check(400, "invalid_auth_config")
	if calls != 0 {
		t.Fatal("invalid configuration made a request")
	}
	body["tokenPath"] = "$.data.token"
	check(200, `"authenticated":true`)
	response = `{"access_token":"do-not-return-this-token"}`
	check(502, `"authenticated":false`)
	body["testCredentials"] = map[string]any{}
	check(400, "credential field")
	if calls != 2 {
		t.Fatalf("expected 2 upstream requests, got %d", calls)
	}
	after, _ := db.ListAuthInstances(context.Background(), "")
	if len(before) != len(after) {
		t.Fatal("test saved an auth instance")
	}
}
