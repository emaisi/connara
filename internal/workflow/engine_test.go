package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"apihub-go/internal/catalog"
	"apihub-go/internal/executor"
	"apihub-go/internal/jsonutil"
	"apihub-go/internal/model"
)

type fakeDeps struct {
	actions      map[string]model.ActionDefinition
	integrations map[string]model.Integration
	connections  map[string]model.Connection
	authorizer   func(principal Principal, action model.ActionDefinition, connection model.Connection) error
}

func (d *fakeDeps) Action(_ context.Context, id string) (model.ActionDefinition, error) {
	item, ok := d.actions[id]
	if !ok {
		return model.ActionDefinition{}, errors.New("action not found")
	}
	return item, nil
}

func (d *fakeDeps) Integration(_ context.Context, id string) (model.Integration, error) {
	item, ok := d.integrations[id]
	if !ok {
		return model.Integration{}, errors.New("integration not found")
	}
	return item, nil
}

func (d *fakeDeps) Connection(_ context.Context, id string) (model.Connection, error) {
	item, ok := d.connections[id]
	if !ok {
		return model.Connection{}, errors.New("connection not found")
	}
	return item, nil
}

func (d *fakeDeps) ResolveAuth(_ context.Context, _ model.Connection) (executor.RequestAuth, error) {
	return executor.RequestAuth{Headers: map[string]string{"Authorization": "Bearer test"}}, nil
}

func (d *fakeDeps) AuthorizeStep(_ context.Context, principal Principal, action model.ActionDefinition, connection model.Connection) error {
	if d.authorizer == nil {
		return nil
	}
	return d.authorizer(principal, action, connection)
}

type upstream struct {
	mu        sync.Mutex
	requests  []capturedRequest
	responses map[string]upstreamResponse
	slow      bool
}

type capturedRequest struct {
	path string
	body map[string]any
}

type upstreamResponse struct {
	status int
	body   string
	delay  time.Duration
}

func (u *upstream) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		_ = jsonutil.Unmarshal(body, &decoded)
		u.mu.Lock()
		u.requests = append(u.requests, capturedRequest{path: r.URL.Path, body: decoded})
		response, ok := u.responses[r.URL.Path]
		if !ok {
			response = upstreamResponse{status: http.StatusOK, body: `{}`}
		}
		u.mu.Unlock()
		if response.delay > 0 {
			time.Sleep(response.delay)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(response.status)
		_, _ = w.Write([]byte(response.body))
	})
}

func (u *upstream) requestCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.requests)
}

func (u *upstream) lastRequest() capturedRequest {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.requests[len(u.requests)-1]
}

func (u *upstream) order() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	var paths []string
	for _, item := range u.requests {
		paths = append(paths, item.path)
	}
	return paths
}

func newTestRunner(serverURL string, deps Dependencies, stepTimeout time.Duration) *Runner {
	return &Runner{
		Deps:        deps,
		Executor:    executor.New(serverClient(serverURL)),
		Catalog:     &catalog.Catalog{},
		StepTimeout: stepTimeout,
	}
}

func serverClient(url string) *http.Client {
	return &http.Client{Transport: &http.Transport{}, Timeout: 60 * time.Second}
}

func setBaseURL(deps *fakeDeps, id, url string) {
	integration := deps.integrations[id]
	integration.BaseURL = url
	deps.integrations[id] = integration
}

func actionDefinition(key, systemID, method, path, inputSchema string, version int64) model.ActionDefinition {
	return model.ActionDefinition{
		ID: "action-" + key, ActionKey: key, Name: key, SystemID: systemID,
		SystemKey: "sys", HTTPMethod: method, RelativePath: path,
		InputSchema: json.RawMessage(inputSchema), Executable: true, Status: "active", Version: version,
	}
}

func twoStepSnapshot() Snapshot {
	return Snapshot{
		WorkflowID: "wf-1", WorkflowVersion: 3,
		Trigger: map[string]any{"customerId": "C-1"},
		Steps: []StepSnapshot{
			{ID: "get_customer", ActionKey: "crm.get", ActionID: "action-crm.get", ActionVersion: 1,
				IntegrationID: "int-crm", ConnectionID: "conn-crm",
				Input: map[string]any{"id": "{{trigger.customerId}}"}},
			{ID: "create_order", ActionKey: "erp.create", ActionID: "action-erp.create", ActionVersion: 1,
				IntegrationID: "int-erp", ConnectionID: "conn-erp", DependsOn: []string{"get_customer"},
				Input: map[string]any{"name": "{{get_customer.data.name}}", "big": "{{get_customer.data.big}}"}},
		},
		Output: map[string]any{
			"orderId":      "{{create_order.data.orderId}}",
			"customerName": "{{get_customer.data.name}}",
		},
	}
}

func twoStepDeps() *fakeDeps {
	return &fakeDeps{
		actions: map[string]model.ActionDefinition{
			"action-crm.get":    actionDefinition("crm.get", "sys-1", "GET", "/customer", `{"type":"object"}`, 1),
			"action-erp.create": actionDefinition("erp.create", "sys-2", "POST", "/order", `{"type":"object"}`, 1),
		},
		integrations: map[string]model.Integration{
			"int-crm": {ID: "int-crm", SystemID: "sys-1", Status: "ready", BaseURL: "-"},
			"int-erp": {ID: "int-erp", SystemID: "sys-2", Status: "ready", BaseURL: "-"},
		},
		connections: map[string]model.Connection{
			"conn-crm": {ID: "conn-crm", IntegrationID: "int-crm", Status: "active"},
			"conn-erp": {ID: "conn-erp", IntegrationID: "int-erp", Status: "active"},
		},
	}
}

func TestRunnerChainsStepsAndPreservesJSONTypes(t *testing.T) {
	server := &upstream{responses: map[string]upstreamResponse{
		"/customer": {status: 200, body: `{"data":{"name":"Alice","big":1234567890123456789}}`},
		"/order":    {status: 200, body: `{"data":{"orderId":"O-1"}}`},
	}}
	httpServer := httptest.NewServer(server.handler())
	defer httpServer.Close()
	deps := twoStepDeps()
	setBaseURL(deps, "int-crm", httpServer.URL)
	setBaseURL(deps, "int-erp", httpServer.URL)
	runner := newTestRunner(httpServer.URL, deps, 5*time.Second)

	result, err := runner.Run(context.Background(), twoStepSnapshot(), Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Final["orderId"] != "O-1" || result.Final["customerName"] != "Alice" {
		t.Fatalf("final mapping wrong: %#v", result.Final)
	}
	order := server.lastRequest()
	if order.path != "/order" {
		t.Fatalf("expected order request last, got %q", order.path)
	}
	if order.body["name"] != "Alice" {
		t.Fatalf("chained name wrong: %#v", order.body)
	}
	// The 19-digit id must arrive at the second API as the same integer, not
	// a float64 with precision loss.
	encoded, _ := json.Marshal(order.body["big"])
	if string(encoded) != "1234567890123456789" {
		t.Fatalf("number lost precision: %s", encoded)
	}
	if result.HasWarnings {
		t.Fatal("clean run must not carry warnings")
	}
}

func TestRunnerSkipsOnRunIfAndRecordsStatus(t *testing.T) {
	server := &upstream{responses: map[string]upstreamResponse{
		"/customer": {status: 200, body: `{"data":{"vip":false,"name":"Bob"}}`},
		"/order":    {status: 200, body: `{"data":{"orderId":"O-1"}}`},
	}}
	httpServer := httptest.NewServer(server.handler())
	defer httpServer.Close()
	deps := twoStepDeps()
	setBaseURL(deps, "int-crm", httpServer.URL)
	setBaseURL(deps, "int-erp", httpServer.URL)
	snapshot := twoStepSnapshot()
	snapshot.Steps[1].RunIf = &Condition{Path: "get_customer.data.vip", Op: "eq", Value: true}
	snapshot.Output = map[string]any{"customerName": "{{get_customer.data.name}}", "maybe": "{{?create_order.data.orderId}}"}
	runner := newTestRunner(httpServer.URL, deps, 5*time.Second)

	var statuses []StepOutcome
	result, err := runner.Run(context.Background(), snapshot, Options{OnStep: func(outcome StepOutcome) {
		statuses = append(statuses, outcome)
	}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(statuses) != 2 || statuses[0].Status != StatusSuccess || statuses[1].Status != StatusSkipped {
		t.Fatalf("unexpected outcomes: %#v", statuses)
	}
	if server.requestCount() != 1 {
		t.Fatalf("skipped step must not call upstream, calls=%d", server.requestCount())
	}
	if result.Final["maybe"] != nil {
		t.Fatalf("optional output from skipped step must be null: %#v", result.Final)
	}
	if result.Final["customerName"] != "Bob" {
		t.Fatalf("output mapping failed: %#v", result.Final)
	}
}

func TestRunnerContinuesAfterToleratedFailure(t *testing.T) {
	server := &upstream{responses: map[string]upstreamResponse{
		"/customer": {status: http.StatusBadGateway, body: `{"error":"down"}`},
		"/order":    {status: 200, body: `{"data":{"orderId":"O-9"}}`},
	}}
	httpServer := httptest.NewServer(server.handler())
	defer httpServer.Close()
	deps := twoStepDeps()
	setBaseURL(deps, "int-crm", httpServer.URL)
	setBaseURL(deps, "int-erp", httpServer.URL)
	snapshot := twoStepSnapshot()
	snapshot.Steps[0].OnError = "continue"
	snapshot.Steps[1].DependsOn = nil
	snapshot.Steps[1].Input = map[string]any{"name": "static"}
	snapshot.Output = map[string]any{"orderId": "{{create_order.data.orderId}}"}
	runner := newTestRunner(httpServer.URL, deps, 5*time.Second)

	result, err := runner.Run(context.Background(), snapshot, Options{})
	if err != nil {
		t.Fatalf("tolerated failure must finish: %v", err)
	}
	if !result.HasWarnings || len(result.FailedSteps) != 1 || result.FailedSteps[0] != "get_customer" {
		t.Fatalf("warnings missing: %#v", result)
	}
	if result.Final["orderId"] != "O-9" {
		t.Fatalf("downstream output missing: %#v", result.Final)
	}
}

func TestRunnerFailsGraphOnProviderError(t *testing.T) {
	server := &upstream{responses: map[string]upstreamResponse{
		"/customer": {status: http.StatusNotFound, body: `{"error":"missing"}`},
	}}
	httpServer := httptest.NewServer(server.handler())
	defer httpServer.Close()
	deps := twoStepDeps()
	setBaseURL(deps, "int-crm", httpServer.URL)
	setBaseURL(deps, "int-erp", httpServer.URL)
	runner := newTestRunner(httpServer.URL, deps, 5*time.Second)

	_, err := runner.Run(context.Background(), twoStepSnapshot(), Options{})
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != "workflow_step_failed" || typed.Status != StatusFailed {
		t.Fatalf("expected step failure, got %v", err)
	}
	if typed.StepID != "get_customer" {
		t.Fatalf("error must name the failing step, got %q", typed.StepID)
	}
	if server.requestCount() != 1 {
		t.Fatalf("downstream must not run after failure, calls=%d", server.requestCount())
	}
}

func TestRunnerMarksTimeoutUnknownEvenWithContinue(t *testing.T) {
	server := &upstream{responses: map[string]upstreamResponse{
		"/customer": {status: 200, body: `{}`, delay: 500 * time.Millisecond},
	}}
	httpServer := httptest.NewServer(server.handler())
	defer httpServer.Close()
	deps := twoStepDeps()
	setBaseURL(deps, "int-crm", httpServer.URL)
	setBaseURL(deps, "int-erp", httpServer.URL)
	snapshot := twoStepSnapshot()
	snapshot.Steps[0].OnError = "continue"
	runner := newTestRunner(httpServer.URL, deps, 50*time.Millisecond)

	var outcomes []StepOutcome
	_, err := runner.Run(context.Background(), snapshot, Options{OnStep: func(outcome StepOutcome) {
		outcomes = append(outcomes, outcome)
	}})
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != "workflow_result_unknown" {
		t.Fatalf("expected unknown outcome, got %v", err)
	}
	if !IsUnknown(err) {
		t.Fatal("IsUnknown must recognize the error")
	}
	if len(outcomes) == 0 || outcomes[0].Status != StatusUnknown {
		t.Fatalf("step outcome must be unknown: %#v", outcomes)
	}
}

func TestRunnerEnforcesAccumulatedResponseBudget(t *testing.T) {
	chunk := strings.Repeat("a", 3<<20)
	server := &upstream{responses: map[string]upstreamResponse{
		"/customer": {status: 200, body: `{"data":{"pad":"` + chunk[:1024] + `"}}`},
		"/order":    {status: 200, body: `{"data":{"pad":"` + chunk + `","pad2":"` + chunk + `","pad3":"` + chunk + `"}}`},
	}}
	httpServer := httptest.NewServer(server.handler())
	defer httpServer.Close()
	deps := twoStepDeps()
	setBaseURL(deps, "int-crm", httpServer.URL)
	setBaseURL(deps, "int-erp", httpServer.URL)
	snapshot := twoStepSnapshot()
	snapshot.Steps[1].DependsOn = nil
	snapshot.Steps[1].Input = map[string]any{"name": "static"}
	snapshot.Output = map[string]any{"x": 1}
	runner := newTestRunner(httpServer.URL, deps, 10*time.Second)

	_, err := runner.Run(context.Background(), snapshot, Options{})
	if err == nil || !strings.Contains(err.Error(), "exceed") {
		t.Fatalf("expected accumulated budget failure, got %v", err)
	}
}

func TestRunnerDeterministicDagOrder(t *testing.T) {
	server := &upstream{responses: map[string]upstreamResponse{}}
	httpServer := httptest.NewServer(server.handler())
	defer httpServer.Close()
	builder := twoStepDeps()
	builder.actions["action-a"] = actionDefinition("a", "sys-1", "GET", "/a", `{"type":"object"}`, 1)
	builder.actions["action-b"] = actionDefinition("b", "sys-1", "GET", "/b", `{"type":"object"}`, 1)
	builder.actions["action-c"] = actionDefinition("c", "sys-2", "GET", "/c", `{"type":"object"}`, 1)
	setBaseURL(builder, "int-crm", httpServer.URL)
	setBaseURL(builder, "int-erp", httpServer.URL)
	snapshot := Snapshot{
		WorkflowID: "wf", Trigger: map[string]any{},
		Steps: []StepSnapshot{
			{ID: "c", ActionKey: "c", ActionID: "action-c", ActionVersion: 1, IntegrationID: "int-erp", ConnectionID: "conn-erp", DependsOn: []string{"a", "b"}, Input: map[string]any{}},
			{ID: "a", ActionKey: "a", ActionID: "action-a", ActionVersion: 1, IntegrationID: "int-crm", ConnectionID: "conn-crm", Input: map[string]any{}},
			{ID: "b", ActionKey: "b", ActionID: "action-b", ActionVersion: 1, IntegrationID: "int-crm", ConnectionID: "conn-crm", DependsOn: []string{"a"}, Input: map[string]any{}},
		},
		Output: map[string]any{"ok": true},
	}
	order, err := OrderSteps(stepsFromSnapshot(snapshot))
	if err != nil {
		t.Fatal(err)
	}
	var sequence []string
	for _, index := range order {
		sequence = append(sequence, []string{"c", "a", "b"}[index])
	}
	if strings.Join(sequence, ",") != "a,b,c" {
		t.Fatalf("expected stable order a,b,c got %v", sequence)
	}
	runner := newTestRunner(httpServer.URL, builder, 5*time.Second)
	if _, err := runner.Run(context.Background(), snapshot, Options{}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := strings.Join(server.order(), ","); got != "/a,/b,/c" {
		t.Fatalf("execution order must be deterministic, got %q", got)
	}
}

func stepsFromSnapshot(snapshot Snapshot) []Step {
	steps := make([]Step, 0, len(snapshot.Steps))
	for _, item := range snapshot.Steps {
		steps = append(steps, Step{ID: item.ID, Action: item.ActionKey, DependsOn: item.DependsOn, Input: item.Input})
	}
	return steps
}

func TestRunnerRejectsStaleActionVersionAndPolicy(t *testing.T) {
	server := &upstream{responses: map[string]upstreamResponse{
		"/customer": {status: 200, body: `{}`},
	}}
	httpServer := httptest.NewServer(server.handler())
	defer httpServer.Close()
	deps := twoStepDeps()
	setBaseURL(deps, "int-crm", httpServer.URL)
	snapshot := Snapshot{
		WorkflowID: "wf", Trigger: map[string]any{},
		Steps: []StepSnapshot{{ID: "s1", ActionKey: "crm.get", ActionID: "action-crm.get",
			ActionVersion: 7, IntegrationID: "int-crm", ConnectionID: "conn-crm", Input: map[string]any{}}},
		Output: map[string]any{"ok": true},
	}
	runner := newTestRunner(httpServer.URL, deps, time.Second)
	if _, err := runner.Run(context.Background(), snapshot, Options{}); err == nil || !strings.Contains(err.Error(), "changed version") {
		t.Fatalf("expected version pinning failure, got %v", err)
	}

	deps.authorizer = func(Principal, model.ActionDefinition, model.Connection) error {
		return errors.New("denied")
	}
	snapshot.Steps[0].ActionVersion = 1
	if _, err := runner.Run(context.Background(), snapshot, Options{}); err == nil || !strings.Contains(err.Error(), "policy denied") {
		t.Fatalf("expected policy failure, got %v", err)
	}
}

func TestRunnerNonJSONBodyReadableAsString(t *testing.T) {
	server := &upstream{responses: map[string]upstreamResponse{
		"/customer": {status: 200, body: `plain-text-not-json`},
	}}
	httpServer := httptest.NewServer(server.handler())
	defer httpServer.Close()
	deps := twoStepDeps()
	setBaseURL(deps, "int-crm", httpServer.URL)
	snapshot := Snapshot{
		WorkflowID: "wf", Trigger: map[string]any{},
		Steps: []StepSnapshot{{ID: "s1", ActionKey: "crm.get", ActionID: "action-crm.get",
			ActionVersion: 1, IntegrationID: "int-crm", ConnectionID: "conn-crm", Input: map[string]any{}}},
		Output: map[string]any{"raw": "{{s1}}"},
	}
	runner := newTestRunner(httpServer.URL, deps, time.Second)
	result, err := runner.Run(context.Background(), snapshot, Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Final["raw"] != "plain-text-not-json" {
		t.Fatalf("non-JSON body must be readable whole: %#v", result.Final)
	}
	// Sub-paths into a string result must fail loudly.
	snapshot.Output = map[string]any{"raw": "{{s1.data}}"}
	if _, err := runner.Run(context.Background(), snapshot, Options{}); err == nil {
		t.Fatal("sub-path into non-JSON response expected failure")
	}
}
