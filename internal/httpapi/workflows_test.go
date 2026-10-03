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
	"apihub-go/internal/background"
	"apihub-go/internal/catalog"
	"apihub-go/internal/executor"
	"apihub-go/internal/model"
	"apihub-go/internal/rediscache"
	"apihub-go/internal/secret"
	"apihub-go/internal/store"
	"apihub-go/internal/testutil"
	"github.com/go-chi/chi/v5"
)

type workflowTestEnv struct {
	api        *api
	db         *store.Store
	codec      *secret.Codec
	upstream   *httptest.Server
	requests   *[]string
	token      model.RuntimeToken
	otherToken model.RuntimeToken
	fixture    testutil.Fixture
}

func newWorkflowEnv(t *testing.T) *workflowTestEnv {
	t.Helper()
	db := testutil.Database(t)
	ctx := context.Background()
	f := testutil.Seed(t, db)
	codec, _ := secret.New(bytes.Repeat([]byte{9}, 32))
	var requests []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		switch r.URL.Path {
		case "/records":
			var body map[string]any
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			_ = body
			_, _ = w.Write([]byte(`{"data":{"name":"Alice"}}`))
		case "/orders":
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			if body["name"] != "Alice" {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"error":"name mismatch"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"orderId":"O-1"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(upstream.Close)
	testutil.Exec(t, db, `UPDATE integrations SET base_url=$2 WHERE id=$1`, f.Integration.ID, upstream.URL)
	sealingAuth := authn.New(db, codec, nil, upstream.Client())
	credentialBlob, err := sealingAuth.SealCredentials(f.Connection.ID, 1, map[string]any{"apiKey": "test", "token": "test"})
	if err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, db, `UPDATE connections SET credential_blob=$2 WHERE id=$1`, f.Connection.ID, credentialBlob)
	// A second executable action on the same integration for the chain.
	second, err := db.SaveAction(ctx, model.ActionDefinition{ActionKey: "test.create", Name: "create", SystemID: f.System.ID, IntegrationID: f.Integration.ID, HTTPMethod: "POST", RelativePath: "/orders", Executable: true, Status: "active", InputSchema: []byte(`{"type":"object"}`), OutputSchema: []byte(`{}`), RequiredScopes: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	_ = second
	auth := authn.New(db, codec, nil, upstream.Client())
	apiInstance := &api{Dependencies: Dependencies{
		Store: db, Catalog: &catalog.Catalog{}, Codec: codec, Auth: auth,
		Executor: executor.New(upstream.Client()), Cache: rediscache.Unavailable(nil),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}}
	members, err := db.ListTeamMembers(ctx)
	if err != nil || len(members) == 0 {
		t.Fatalf("bootstrap members missing: %v", err)
	}
	mkToken := func(name string, allowAll bool) model.RuntimeToken {
		plain := "hub_rt_" + testutil.ID(name)
		token := model.RuntimeToken{
			ID: testutil.ID("token-" + name), Name: name, TokenPrefix: plain[:min(18, len(plain))],
			TokenHash: tokenHash(plain), Status: "active", CreatedBy: members[0].UserID,
		}
		if allowAll {
			token.AllowedActions = []string{"*"}
		}
		if err := db.CreateRuntimeToken(ctx, token); err != nil {
			t.Fatal(err)
		}
		return token
	}
	return &workflowTestEnv{
		api: apiInstance, db: db, codec: codec, upstream: upstream, requests: &requests,
		token: mkToken("primary", true), otherToken: mkToken("other", false),
		fixture: f,
	}
}

func (e *workflowTestEnv) deployWorkflow(t *testing.T) model.Workflow {
	t.Helper()
	ctx := context.Background()
	binding := map[string]any{
		"integrationId": e.fixture.Integration.ID,
		"connectionKey": e.fixture.Connection.ID,
	}
	getCustomer := map[string]any{"id": "get_customer", "action": "test.list", "input": map[string]any{"id": "{{trigger.customerId}}"}}
	for key, value := range binding {
		getCustomer[key] = value
	}
	createOrder := map[string]any{"id": "create_order", "action": "test.create", "input": map[string]any{"name": "{{get_customer.data.name}}"}, "dependsOn": []string{"get_customer"}}
	for key, value := range binding {
		createOrder[key] = value
	}
	graph := map[string]any{
		"steps":  []map[string]any{getCustomer, createOrder},
		"output": map[string]any{"orderId": "{{create_order.data.orderId}}", "customerName": "{{get_customer.data.name}}"},
	}
	raw, _ := json.Marshal(graph)
	item, err := e.db.SaveWorkflow(ctx, model.Workflow{
		WorkflowKey: "order-flow", Name: "order flow", Graph: raw,
		ScheduleType: "manual", Input: []byte(`{"customerId":"C-1"}`),
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := e.db.ResolveWorkflowBindings(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	deployed, err := e.db.DeployWorkflow(ctx, resolved.ID, resolved.Version, resolved.Graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	return deployed
}

func (e *workflowTestEnv) request(t *testing.T, method, target string, token model.RuntimeToken, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	request := httptest.NewRequest(method, target, reader)
	request.Header.Set("Content-Type", "application/json")
	route := chi.NewRouteContext()
	route.URLParams.Add("key", "order-flow")
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, route))
	request = request.WithContext(context.WithValue(request.Context(), runtimeTokenKey, token))
	response := httptest.NewRecorder()
	e.api.triggerWorkflow(response, request)
	var payload map[string]any
	_ = json.Unmarshal(response.Body.Bytes(), &payload)
	return response, payload
}

func (e *workflowTestEnv) runStatus(t *testing.T, token model.RuntimeToken, operationID string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	request := httptest.NewRequest("GET", "/v1/workflow-runs/"+operationID, nil)
	route := chi.NewRouteContext()
	route.URLParams.Add("id", operationID)
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, route))
	request = request.WithContext(context.WithValue(request.Context(), runtimeTokenKey, token))
	response := httptest.NewRecorder()
	e.api.runtimeWorkflowRun(response, request)
	var payload map[string]any
	_ = json.Unmarshal(response.Body.Bytes(), &payload)
	return response, payload
}

func (e *workflowTestEnv) drainWorkflowJob(t *testing.T, operationID string) {
	t.Helper()
	ctx := context.Background()
	jobs, err := e.db.ClaimJobsByKinds(ctx, "test-worker", 5, []string{"workflow_run"})
	if err != nil {
		t.Fatal(err)
	}
	auth := authn.New(e.db, e.codec, nil, e.upstream.Client())
	service := background.New(e.db, auth, executor.New(e.upstream.Client()), &catalog.Catalog{}, e.codec, e.upstream.Client(), slog.New(slog.NewTextHandler(io.Discard, nil)), "test")
	for _, job := range jobs {
		if job.OperationID != operationID {
			continue
		}
		if err := service.RunWorkflowJob(ctx, job); err != nil {
			t.Fatalf("run workflow job: %v", err)
		}
	}
}

func TestRuntimeWorkflowSyncMapsOutput(t *testing.T) {
	env := newWorkflowEnv(t)
	env.deployWorkflow(t)
	response, payload := env.request(t, "POST", "/v1/workflows/order-flow", env.token, `{"input":{"customerId":"C-9"}}`)
	if response.Code != http.StatusOK {
		t.Fatalf("sync trigger: %d %s", response.Code, response.Body.String())
	}
	if payload["data"] == nil {
		t.Fatalf("nil data: %s", response.Body.String())
	}
	data := payload["data"].(map[string]any)
	if data["orderId"] != "O-1" || data["customerName"] != "Alice" {
		t.Fatalf("unexpected mapped output: %#v", data)
	}
	meta := payload["meta"].(map[string]any)
	if meta["operationId"] == nil || meta["hasWarnings"] != false {
		t.Fatalf("unexpected meta: %#v", meta)
	}
	if strings.Join(*env.requests, ",") != "/records,/orders" {
		t.Fatalf("upstream call chain wrong: %v", *env.requests)
	}
	// The stored operation exposes only sanitized previews.
	operation, err := env.db.Operation(context.Background(), meta["operationId"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != "success" || bytes.Contains(operation.Output, []byte("Alice")) {
		t.Fatalf("operation output must be a sanitized summary: %s", operation.Output)
	}
}

func TestRuntimeWorkflowAsyncResultAndCrossTokenDenial(t *testing.T) {
	env := newWorkflowEnv(t)
	env.deployWorkflow(t)
	response, payload := env.request(t, "POST", "/v1/workflows/order-flow?async=1", env.token, "")
	if response.Code != http.StatusAccepted {
		t.Fatalf("async trigger: %d %s", response.Code, response.Body.String())
	}
	data := payload["data"].(map[string]any)
	operationID := data["operationId"].(string)
	env.drainWorkflowJob(t, operationID)
	statusResponse, statusPayload := env.runStatus(t, env.token, operationID)
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("status: %d %s", statusResponse.Code, statusResponse.Body.String())
	}
	result := statusPayload["data"].(map[string]any)
	if result["status"] != "success" {
		t.Fatalf("run status: %#v", result)
	}
	output := result["output"].(map[string]any)
	if output["orderId"] != "O-1" {
		t.Fatalf("final output not decrypted: %#v", output)
	}
	// Another valid token learns nothing about the run.
	denied, _ := env.runStatus(t, env.otherToken, operationID)
	if denied.Code != http.StatusNotFound {
		t.Fatalf("cross-token read must 404, got %d", denied.Code)
	}
}

func TestRuntimeWorkflowIdempotencyReplayAndConflict(t *testing.T) {
	env := newWorkflowEnv(t)
	env.deployWorkflow(t)
	request := func(key string) (*httptest.ResponseRecorder, map[string]any) {
		httpRequest := httptest.NewRequest("POST", "/v1/workflows/order-flow", bytes.NewReader([]byte(`{"input":{"customerId":"C-1"}}`)))
		httpRequest.Header.Set("Idempotency-Key", key)
		route := chi.NewRouteContext()
		route.URLParams.Add("key", "order-flow")
		httpRequest = httpRequest.WithContext(context.WithValue(httpRequest.Context(), chi.RouteCtxKey, route))
		httpRequest = httpRequest.WithContext(context.WithValue(httpRequest.Context(), runtimeTokenKey, env.token))
		response := httptest.NewRecorder()
		env.api.triggerWorkflow(response, httpRequest)
		var payload map[string]any
		_ = json.Unmarshal(response.Body.Bytes(), &payload)
		return response, payload
	}
	first, firstPayload := request("idem-1")
	if first.Code != http.StatusOK {
		t.Fatalf("first call: %d %s", first.Code, first.Body.String())
	}
	replay, replayPayload := request("idem-1")
	if replay.Code != http.StatusOK {
		t.Fatalf("replay: %d %s", replay.Code, replay.Body.String())
	}
	if !reflectDeepEqual(firstPayload["data"], replayPayload["data"]) {
		t.Fatalf("replay data mismatch: %#v vs %#v", firstPayload["data"], replayPayload["data"])
	}
	if replayPayload["meta"].(map[string]any)["replayed"] != true {
		t.Fatalf("replay must be flagged: %#v", replayPayload["meta"])
	}
	// Same key with a different request fingerprint conflicts.
	conflictRequest := httptest.NewRequest("POST", "/v1/workflows/order-flow", bytes.NewReader([]byte(`{"input":{"customerId":"C-2"}}`)))
	conflictRequest.Header.Set("Idempotency-Key", "idem-1")
	route := chi.NewRouteContext()
	route.URLParams.Add("key", "order-flow")
	conflictRequest = conflictRequest.WithContext(context.WithValue(conflictRequest.Context(), chi.RouteCtxKey, route))
	conflictRequest = conflictRequest.WithContext(context.WithValue(conflictRequest.Context(), runtimeTokenKey, env.token))
	conflictRecorder := httptest.NewRecorder()
	env.api.triggerWorkflow(conflictRecorder, conflictRequest)
	if conflictRecorder.Code != http.StatusConflict {
		t.Fatalf("fingerprint conflict: %d %s", conflictRecorder.Code, conflictRecorder.Body.String())
	}
}

func reflectDeepEqual(left, right any) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}

func TestWorkflowDiscoveryFiltersByPolicy(t *testing.T) {
	env := newWorkflowEnv(t)
	env.deployWorkflow(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/v1/workflows", nil)
	request = request.WithContext(context.WithValue(request.Context(), runtimeTokenKey, env.token))
	env.api.runtimeWorkflows(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("discovery: %d %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 1 || payload.Data[0]["workflowKey"] != "order-flow" {
		t.Fatalf("expected discovered workflow: %s", recorder.Body.String())
	}
	// The other token denies test.create and must not see the workflow.
	deniedRecorder := httptest.NewRecorder()
	deniedRequest := httptest.NewRequest("GET", "/v1/workflows", nil)
	deniedRequest = deniedRequest.WithContext(context.WithValue(deniedRequest.Context(), runtimeTokenKey, env.otherToken))
	env.api.runtimeWorkflows(deniedRecorder, deniedRequest)
	var denied struct {
		Data []map[string]any `json:"data"`
	}
	_ = json.Unmarshal(deniedRecorder.Body.Bytes(), &denied)
	if len(denied.Data) != 0 {
		t.Fatalf("policy must hide the workflow: %s", deniedRecorder.Body.String())
	}
}

func TestRevokedTokenStopsQueuedWorkflowRun(t *testing.T) {
	env := newWorkflowEnv(t)
	env.deployWorkflow(t)
	response, payload := env.request(t, "POST", "/v1/workflows/order-flow?async=1", env.token, "")
	if response.Code != http.StatusAccepted {
		t.Fatalf("async trigger: %d %s", response.Code, response.Body.String())
	}
	operationID := payload["data"].(map[string]any)["operationId"].(string)
	// Revoke before the worker picks the job up.
	if err := env.db.RevokeRuntimeToken(context.Background(), env.token.ID); err != nil {
		t.Fatal(err)
	}
	env.drainWorkflowJob(t, operationID)
	operation, err := env.db.Operation(context.Background(), operationID)
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != "failed" || operation.ErrorCode != "policy_denied" {
		t.Fatalf("revoked token must fail the run: %+v", operation)
	}
	if len(*env.requests) != 0 {
		t.Fatalf("no upstream call may happen after revocation: %v", *env.requests)
	}
}
