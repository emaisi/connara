package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"apihub-go/internal/model"
	"apihub-go/internal/store"
	"apihub-go/internal/testutil"
	"github.com/go-chi/chi/v5"
)

func TestSystemAPITargetPreviewVersionsAndVerification(t *testing.T) {
	env := newWorkflowEnv(t)
	ctx := context.Background()
	db := env.db
	f := env.fixture
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prefix/resources" {
			t.Errorf("wrong path %s", r.URL.Path)
		}
		w.Header().Set("X-Upstream-Id", "trace-test")
		w.Header().Set("Set-Cookie", "session=private-response")
		_, _ = w.Write([]byte(`{"code":0,"data":[]}`))
	}))
	defer upstream.Close()
	target, err := db.SaveIntegration(ctx, model.Integration{IntegrationKey: "second", Name: "Second", SystemID: f.System.ID, AuthInstanceID: f.Auth.ID, BaseURL: upstream.URL + "/prefix", Status: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	definition, err := db.SaveAction(ctx, model.ActionDefinition{ActionKey: "resources", Name: "Resources", SystemID: f.System.ID, HTTPMethod: "GET", RelativePath: "/resources", Status: "active", RequiredScopes: []string{}, InputSchema: []byte(`{"type":"object"}`), RequestConfig: []byte(`{"schemaVersion":1,"bodyFormat":"none","parameters":[],"headers":[]}`), ResponseConfig: []byte(`{"statusCodes":[200],"successCondition":{"path":"$.code","operator":"equals","value":0}}`), OutputSchema: []byte(`{"type":"object","required":["data"]}`)})
	if err != nil || definition.IntegrationID != "" {
		t.Fatalf("system API save: %v %+v", err, definition)
	}
	account, err := db.SaveConnection(ctx, store.SaveConnectionInput{Connection: model.Connection{ConnectionKey: "second-account", Name: "Second", IntegrationID: target.ID, AuthInstanceID: f.Auth.ID, Status: "pending", CredentialBlob: []byte(`{}`), KeyVersion: 1, Tags: []string{}}, EndUser: model.EndUser{ExternalKey: "second"}})
	if err != nil {
		t.Fatal(err)
	}
	blob, err := env.api.Auth.SealCredentials(account.ID, account.Revision, map[string]any{"token": "test", "apiKey": "test"})
	if err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, db, `UPDATE connections SET credential_blob=$2 WHERE id=$1`, account.ID, blob)
	router := chi.NewRouter()
	router.Post("/actions/{id}/preview", env.api.previewAction)
	router.Post("/accounts/{id}/verify", env.api.verifyConnection)
	previewBody := `{"integrationId":"` + target.ID + `","connectionKey":"` + account.ID + `","input":{}}`
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("POST", "/actions/"+definition.ID+"/preview", strings.NewReader(previewBody)))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "/prefix/resources") || !strings.Contains(rec.Body.String(), `"verified":false`) {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body.String())
	}
	if current, _ := db.Connection(ctx, account.ID); current.LastVerifiedAt != nil {
		t.Fatal("preview changed verification")
	}
	verify := httptest.NewRecorder()
	router.ServeHTTP(verify, httptest.NewRequest("POST", "/accounts/"+account.ID+"/verify", strings.NewReader(`{"actionId":"`+definition.ID+`"}`)))
	if verify.Code != 200 {
		t.Fatalf("verify: %d %s", verify.Code, verify.Body.String())
	}
	for _, scenario := range []struct {
		method string
		status string
		want   int
	}{
		{"POST", "active", 200},
		{"POST", "draft", 400},
		{"POST", "disabled", 400},
	} {
		testutil.Exec(t, db, `UPDATE actions SET http_method=$2,status=$3 WHERE id=$1`, definition.ID, scenario.method, scenario.status)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("POST", "/accounts/"+account.ID+"/verify", strings.NewReader(`{"actionId":"`+definition.ID+`"}`)))
		if response.Code != scenario.want {
			t.Fatalf("verify %s/%s: %d %s", scenario.method, scenario.status, response.Code, response.Body.String())
		}
	}
	testutil.Exec(t, db, `UPDATE actions SET http_method='GET',status='active' WHERE id=$1`, definition.ID)
	account, err = db.Connection(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := actionRequest{IntegrationID: target.ID, ConnectionKey: account.ID, Input: map[string]any{}, ExpectedAPIVersion: definition.Version, ExpectedIntegrationVersion: target.Version, ExpectedAccountRevision: account.Revision}
	result, err := env.api.executeActionCore(ctx, definition.ID, request, model.RuntimeToken{}, "test-request", "manual", "")
	if err != nil || result.Status != 200 {
		t.Fatalf("execute: %v %s", err, result.Body)
	}
	events, err := db.Operation(ctx, result.Operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(events)
	if !strings.Contains(string(encoded), "/prefix/resources") {
		t.Fatal("historical target snapshot missing")
	}
	if !strings.Contains(string(encoded), "上游请求与响应") || !strings.Contains(string(encoded), "trace-test") || strings.Contains(string(encoded), "private-response") {
		t.Fatalf("missing or unsafe response trace: %s", encoded)
	}
	target.BaseURL = upstream.URL + "/new"
	updated, err := db.SaveIntegration(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	_, err = env.api.executeActionCore(ctx, definition.ID, request, model.RuntimeToken{}, "stale", "manual", "")
	status, _ := executionErrorStatus(err)
	if status != 409 {
		t.Fatalf("stale preview not rejected: %v", err)
	}
	if _, err = db.MarkConnectionVerifiedVersion(ctx, account.ID, account.Revision, target.TargetVersion); err == nil {
		t.Fatal("old target verification accepted")
	}
	if updated.TargetVersion != target.TargetVersion+1 {
		t.Fatal("target version not incremented")
	}
}

func TestSystemAPINoAuthNeedsNoAccount(t *testing.T) {
	env := newWorkflowEnv(t)
	ctx := context.Background()
	db := env.db
	f := env.fixture
	template, err := db.AuthTemplate(ctx, "no_auth")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetSystemAuthTemplates(ctx, f.System.ID, []string{f.Auth.AuthTemplateID, template.ID}, f.Auth.AuthTemplateID); err != nil {
		t.Fatal(err)
	}
	instance, err := db.SaveAuthInstance(ctx, model.AuthInstance{InstanceKey: "public", Name: "Public", SystemID: f.System.ID, AuthTemplateID: template.ID, Status: "ready", PublicConfig: []byte(`{}`), KeyVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	target, err := db.SaveIntegration(ctx, model.Integration{IntegrationKey: "public", Name: "Public", SystemID: f.System.ID, AuthInstanceID: instance.ID, BaseURL: env.upstream.URL, Status: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := env.api.executeActionCore(ctx, f.Action.ID, actionRequest{IntegrationID: target.ID, Input: map[string]any{}, ExpectedAPIVersion: f.Action.Version, ExpectedIntegrationVersion: target.Version}, model.RuntimeToken{}, "no-auth", "manual", "")
	if err != nil || result.Status != 200 {
		t.Fatalf("no-auth execution failed: %v %s", err, result.Body)
	}
	_, _, _, err = env.api.actionTarget(ctx, f.Action, actionRequest{IntegrationID: target.ID, ConnectionKey: f.Connection.ID}, true)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = env.api.actionTarget(ctx, f.Action, actionRequest{IntegrationID: f.Integration.ID, ConnectionKey: "nonexistent"}, true)
	if status, _ := executionErrorStatus(err); status != 404 {
		t.Fatalf("invalid account accepted: %v", err)
	}
}

func TestSaveAPIChecksContractAndReportsField(t *testing.T) {
	env := newWorkflowEnv(t)
	router := chi.NewRouter()
	router.Post("/actions", env.api.saveAction)
	base := map[string]any{
		"actionKey": "default_contract", "name": "Default contract", "systemId": env.fixture.System.ID,
		"httpMethod": "GET", "relativePath": "/resources", "status": "active",
		"requestConfig": map[string]any{"schemaVersion": 1, "bodyFormat": "none", "parameters": []any{map[string]any{"name": "page", "in": "query", "type": "integer", "required": true, "default": 0}}, "headers": []any{}},
		"inputSchema":   map[string]any{"type": "object", "properties": map[string]any{"page": map[string]any{"type": "integer", "minimum": 1}}, "required": []string{"page"}},
	}
	encoded, _ := json.Marshal(base)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("POST", "/actions", strings.NewReader(string(encoded))))
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), `"fieldErrors"`) || !strings.Contains(rec.Body.String(), `"path":"page"`) {
		t.Fatalf("invalid contract not localized: %d %s", rec.Code, rec.Body.String())
	}
	base["requestConfig"].(map[string]any)["parameters"].([]any)[0].(map[string]any)["default"] = 2
	encoded, _ = json.Marshal(base)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("POST", "/actions", strings.NewReader(string(encoded))))
	if rec.Code != 201 {
		t.Fatalf("valid contract rejected: %d %s", rec.Code, rec.Body.String())
	}
	var action model.ActionDefinition
	json.Unmarshal(rec.Body.Bytes(), &action)
	if err := env.api.Catalog.ValidateInput(definitionAction(action), map[string]any{}); err != nil {
		t.Fatalf("default not applied before required: %v", err)
	}
	oldVersion := action.Version
	action.InputSchema = []byte(`{"type":"object","properties":{"page":{"type":"integer","minimum":3}},"required":["page"]}`)
	action.RequestConfig = []byte(`{"schemaVersion":1,"bodyFormat":"none","parameters":[{"name":"page","in":"query","type":"integer","required":true,"default":4}],"headers":[]}`)
	updated, err := env.db.SaveAction(context.Background(), action)
	if err != nil {
		t.Fatal(err)
	}
	integration, err := env.db.Integration(context.Background(), env.fixture.Integration.ID)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := env.db.Connection(context.Background(), env.fixture.Connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = env.api.executeActionCore(context.Background(), updated.ID, actionRequest{Input: map[string]any{"page": json.Number("0")}, IntegrationID: integration.ID, ConnectionKey: connection.ID, ExpectedAPIVersion: oldVersion, ExpectedIntegrationVersion: integration.Version, ExpectedAccountRevision: connection.Revision}, model.RuntimeToken{}, "stale-contract", "manual", "")
	status, code := executionErrorStatus(err)
	if status != 409 || code != "configuration_changed" {
		t.Fatalf("stale preview must refresh before input checks: %d %s %v", status, code, err)
	}

}
