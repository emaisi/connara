package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"apihub-go/internal/authn"
	"apihub-go/internal/secret"
	"apihub-go/internal/testutil"
)

func TestIntranetHTTPURLsAccepted(t *testing.T) {
	db := testutil.Database(t)
	f := testutil.Seed(t, db)
	codec, _ := secret.New(bytes.Repeat([]byte{3}, 32))
	service := authn.New(db, codec, nil, http.DefaultClient)
	a := &api{Dependencies: Dependencies{Store: db, Codec: codec, Auth: service, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}}

	t.Run("auth instance token and refresh URLs", func(t *testing.T) {
		body := map[string]any{
			"instanceKey": f.Auth.InstanceKey, "name": f.Auth.Name,
			"systemId": f.Auth.SystemID, "authTemplateId": f.Auth.AuthTemplateID,
			"status": f.Auth.Status, "version": f.Auth.Version,
			"tokenUrl": "http://intranet.example.test/login", "refreshUrl": "http://intranet.example.test/refresh",
		}
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest("PATCH", "/api/auth-instances/"+f.Auth.ID, bytes.NewReader(raw))
		route := chi.NewRouteContext()
		route.URLParams.Add("id", f.Auth.ID)
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, route))
		w := httptest.NewRecorder()
		a.saveAuthInstance(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("save auth instance with http URLs: %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("integration base URL", func(t *testing.T) {
		body := map[string]any{
			"integrationKey": "intranet-http", "name": "intranet-http",
			"systemId": f.Integration.SystemID, "authInstanceId": f.Integration.AuthInstanceID,
			"baseUrl": "http://intranet.example.test/api", "status": "ready",
		}
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "/api/integrations", bytes.NewReader(raw))
		w := httptest.NewRecorder()
		a.saveIntegration(w, r)
		if w.Code != http.StatusOK && w.Code != http.StatusCreated {
			t.Fatalf("save integration with http baseUrl: %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("malformed URLs still rejected", func(t *testing.T) {
		body := map[string]any{
			"instanceKey": f.Auth.InstanceKey, "name": f.Auth.Name,
			"systemId": f.Auth.SystemID, "authTemplateId": f.Auth.AuthTemplateID,
			"status": f.Auth.Status, "version": f.Auth.Version,
			"tokenUrl": "ftp://intranet.example.test/login",
		}
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest("PATCH", "/api/auth-instances/"+f.Auth.ID, bytes.NewReader(raw))
		route := chi.NewRouteContext()
		route.URLParams.Add("id", f.Auth.ID)
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, route))
		w := httptest.NewRecorder()
		a.saveAuthInstance(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for non-http scheme, got %d %s", w.Code, w.Body.String())
		}
	})
}
