package httpapi

import (
	"apihub-go/internal/authn"
	"apihub-go/internal/rediscache"
	"apihub-go/internal/secret"
	"apihub-go/internal/testutil"
	"bytes"
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRoleAuthorizationMatrix(t *testing.T) {
	for _, role := range []string{"owner", "admin", "developer", "viewer", "unknown"} {
		for _, route := range []string{"/api/actions/x/test", "/api/connections", "/api/auth-instances/x", "/api/runtime-tokens", "/api/team/members/x", "/api/settings"} {
			want := role == "owner" || role == "admin" || (role == "developer" && (route == "/api/actions/x/test" || route == "/api/connections" || route == "/api/auth-instances/x"))
			if allowsAdmin(role, "POST", route) != want {
				t.Errorf("role %s route %s", role, route)
			}
		}
	}
}
func TestViewerIsRejectedBeforeHandler(t *testing.T) {
	db := testutil.Database(t)
	ctx := context.Background()
	members, err := db.ListTeamMembers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	member := members[0]
	testutil.Exec(t, db, `UPDATE workspace_members SET role='viewer' WHERE id=$1`, member.ID)
	token := "test-viewer-session"
	if err := db.CreateAdminSession(ctx, testutil.ID("session"), member.UserID, tokenHash(token), "", "test", 1, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	a := &api{Dependencies: Dependencies{Store: db, Cache: rediscache.Unavailable(nil)}}
	called := false
	handler := a.requireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(204) }))
	request := httptest.NewRequest("POST", "http://example.test/api/actions/x/test", nil)
	request.AddCookie(&http.Cookie{Name: adminSessionCookie, Value: token})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 403 || called {
		t.Fatalf("viewer reached mutation handler: %d", response.Code)
	}
	request = httptest.NewRequest("GET", "http://example.test/api/actions", nil)
	request.AddCookie(&http.Cookie{Name: adminSessionCookie, Value: token})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 204 {
		t.Fatalf("viewer read rejected: %d", response.Code)
	}
}
func TestPartialAuthSecretsPreserveOtherFields(t *testing.T) {
	db := testutil.Database(t)
	f := testutil.Seed(t, db)
	codec, _ := secret.New(bytes.Repeat([]byte{3}, 32))
	service := authn.New(db, codec, nil, http.DefaultClient)
	blob, _ := codec.Encrypt([]byte(`{"clientId":"existing-id","clientSecret":"old"}`), []byte(db.WorkspaceID()+":auth-instance:"+f.Auth.ID))
	testutil.Exec(t, db, `UPDATE auth_instances SET secret_blob=$2 WHERE id=$1`, f.Auth.ID, blob)
	a := &api{Dependencies: Dependencies{Store: db, Codec: codec, Auth: service, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	body := map[string]any{"instanceKey": f.Auth.InstanceKey, "name": f.Auth.Name, "systemId": f.Auth.SystemID, "authTemplateId": f.Auth.AuthTemplateID, "status": "ready", "publicConfig": map[string]any{"scopes": []string{"read", "write"}}, "secrets": map[string]any{"clientSecret": "new"}, "version": f.Auth.Version}
	send := func(body map[string]any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest("PATCH", "/api/auth-instances/"+f.Auth.ID, bytes.NewReader(raw))
		route := chi.NewRouteContext()
		route.URLParams.Add("id", f.Auth.ID)
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, route))
		w := httptest.NewRecorder()
		a.saveAuthInstance(w, r)
		return w
	}
	response := send(body)
	if response.Code != 200 {
		t.Fatalf("save: %d %s", response.Code, response.Body.String())
	}
	updated, err := db.AuthInstance(context.Background(), f.Auth.ID)
	if err != nil {
		t.Fatal(err)
	}
	values, err := service.AuthInstanceSecrets(updated)
	if err != nil || values["clientId"] != "existing-id" || values["clientSecret"] != "new" {
		t.Fatalf("partial update lost secret: %v %v", values, err)
	}
	if response := send(body); response.Code != 409 {
		t.Fatalf("stale version accepted: %d", response.Code)
	}
	// Explicit null deletes the last keys and must persist an empty encrypted object.
	body["version"] = updated.Version
	body["secrets"] = map[string]any{"clientId": nil, "clientSecret": nil}
	response = send(body)
	if response.Code != 200 {
		t.Fatalf("delete: %d %s", response.Code, response.Body.String())
	}
	updated, _ = db.AuthInstance(context.Background(), f.Auth.ID)
	values, err = service.AuthInstanceSecrets(updated)
	if err != nil || len(values) != 0 {
		t.Fatalf("secret deletion ignored: %v %v", values, err)
	}
}
func TestJSONRuntimePreservesLargeIDs(t *testing.T) {
	value := providerData([]byte(`{"id":9007199254740993}`))
	raw, err := json.Marshal(value)
	if err != nil || string(raw) != `{"id":9007199254740993}` {
		t.Fatalf("rounded response: %s %v", raw, err)
	}
	request := httptest.NewRequest("POST", "/", bytes.NewBufferString(`{"input":{"id":9007199254740993}}`))
	var input actionRequest
	if !decodeJSON(httptest.NewRecorder(), request, &input, true) {
		t.Fatal("decode")
	}
	if input.Input["id"] != json.Number("9007199254740993") {
		t.Fatalf("rounded input: %v", input.Input)
	}
}
