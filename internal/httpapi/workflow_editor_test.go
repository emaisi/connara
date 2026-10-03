package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"apihub-go/internal/model"
	"apihub-go/internal/testutil"
	"apihub-go/internal/workflow"
	"github.com/go-chi/chi/v5"
)

func (e *workflowTestEnv) editorRequest(t *testing.T, id, role, body string, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "/api/workflows/"+id, strings.NewReader(body))
	route := chi.NewRouteContext()
	route.URLParams.Add("id", id)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, route))
	members, err := e.db.ListTeamMembers(r.Context())
	if err != nil {
		t.Fatal(err)
	}
	r = r.WithContext(withAdminIdentity(r.Context(), adminIdentity{UserID: members[0].UserID, Role: role}))
	w := httptest.NewRecorder()
	handler(w, r)
	return w
}

func (e *workflowTestEnv) deployCodeWorkflow(t *testing.T) model.Workflow {
	t.Helper()
	worker, launcher := os.Getenv("APIHUB_TEST_CODE_WORKER"), os.Getenv("APIHUB_TEST_CODE_LAUNCHER")
	if worker == "" || launcher == "" {
		t.Skip("real sandbox paths are required")
	}
	e.db.ConfigureWorkflows(workflow.Features{V2Enabled: true, CodeEnabled: true}, workflow.NewCodeRunner(worker, launcher, 2))
	raw := fmt.Sprintf(`{"schemaVersion":2,"inputSchema":{"type":"object","properties":{"customerId":{"type":"string"},"vip":{"type":"boolean"}},"required":["customerId","vip"]},"variables":[{"name":"discount","type":"integer","initial":0},{"name":"total","type":"integer","initial":0}],"steps":[{"id":"get_customer","type":"api","action":"test.list","integrationId":%q,"connectionKey":%q,"input":{"id":"{{trigger.customerId}}"}},{"id":"route","type":"condition","dependsOn":["get_customer"],"branches":[{"id":"vip","condition":{"path":"trigger.vip","op":"eq","value":true},"assign":[{"variable":"discount","value":100}]},{"id":"otherwise","default":true}]},{"id":"calculate","type":"code","dependsOn":["route"],"scope":[{"conditionId":"route","branchId":"vip"}],"language":"javascript","runtimeProfile":"js-v1","code":"function main(input){/* private-source-marker */ return {total:input.price*input.quantity-input.discount};}","inputSchema":{"type":"object","properties":{"price":{"type":"integer"},"quantity":{"type":"integer"},"discount":{"type":"integer"}},"required":["price","quantity","discount"]},"input":{"price":1000,"quantity":2,"discount":"{{vars.discount}}"},"outputSchema":{"type":"object","properties":{"total":{"type":"integer"}},"required":["total"]},"assign":[{"variable":"total","value":"{{calculate.total}}"}]},{"id":"normal","type":"transform","dependsOn":["route"],"scope":[{"conditionId":"route","branchId":"otherwise"}],"source":[1,2,3],"operations":[{"op":"count"}],"assign":[{"variable":"total","value":"{{normal.result}}"}]},{"id":"create_order","type":"api","action":"test.create","integrationId":%q,"connectionKey":%q,"dependsOn":["calculate","normal"],"input":{"name":"{{get_customer.data.name}}","total":"{{vars.total}}"}}],"output":{"orderId":"{{create_order.data.orderId}}","total":"{{vars.total}}","chosen":{"$value":{"kind":"branch","conditionId":"route","cases":{"vip":"{{calculate.total}}","otherwise":"{{normal.result}}"}}},"date":{"$value":{"kind":"run","field":"businessDate"}}}}`, e.fixture.Integration.ID, e.fixture.Connection.ID, e.fixture.Integration.ID, e.fixture.Connection.ID)
	item, err := e.db.SaveWorkflow(context.Background(), model.Workflow{WorkflowKey: "order-flow", Name: "v2 code order flow", Graph: []byte(raw), ScheduleType: "manual", ScheduleTimezone: "Asia/Singapore", Input: []byte(`{"customerId":"C-1","vip":true}`)}, 0)
	if err != nil {
		t.Fatal(err)
	}
	response := e.editorRequest(t, item.ID, "owner", fmt.Sprintf(`{"expectedVersion":%d}`, item.Version), e.api.deployWorkflow)
	if response.Code != 200 {
		t.Fatalf("real code deployment: %d %s", response.Code, response.Body.String())
	}
	item, err = e.db.Workflow(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func TestV2RealCodeWorkflowSyncAsyncScheduledAndFrozenHistory(t *testing.T) {
	e := newWorkflowEnv(t)
	item := e.deployCodeWorkflow(t)
	response, payload := e.request(t, "POST", "/v1/workflows/order-flow", e.token, `{"input":{"customerId":"C-1","vip":true}}`)
	if response.Code != 200 {
		t.Fatalf("sync: %d %s", response.Code, response.Body.String())
	}
	output := payload["data"].(map[string]any)
	if output["total"] != float64(1900) || output["chosen"] != float64(1900) || output["orderId"] != "O-1" {
		t.Fatalf("unexpected real code output: %#v", output)
	}
	syncID := payload["meta"].(map[string]any)["operationId"].(string)
	operation, err := e.db.Operation(context.Background(), syncID)
	if err != nil {
		t.Fatal(err)
	}
	starts, finishes := 0, 0
	for _, event := range operation.Events {
		var attrs map[string]any
		_ = json.Unmarshal(event.Attributes, &attrs)
		if attrs["eventType"] == "workflow.step.started" {
			starts++
		}
		if attrs["eventType"] == "workflow.step.finished" {
			finishes++
			if attrs["stepId"] == "calculate" && attrs["assignmentStatus"] != "committed" {
				t.Fatal("assignment event missing", attrs)
			}
		}
	}
	if starts != 5 || finishes != 5 {
		t.Fatalf("both event phases required: %d %d", starts, finishes)
	}
	view := e.editorRequest(t, syncID, "developer", "", e.api.workflowRunView)
	if view.Code != 200 || bytes.Contains(view.Body.Bytes(), []byte("private-source-marker")) || !bytes.Contains(view.Body.Bytes(), []byte("runMetadata")) {
		t.Fatalf("unsafe or missing run view: %d %s", view.Code, view.Body.String())
	}
	viewer := e.editorRequest(t, syncID, "viewer", "", e.api.workflowRunView)
	if viewer.Code != 403 {
		t.Fatal("viewer read sensitive snapshot")
	}
	audit := workflowAuditSummary(item)
	if bytes.Contains(audit, []byte("private-source-marker")) || !bytes.Contains(audit, []byte("sourceSHA256")) {
		t.Fatal("code audit contains source or lacks hash", string(audit))
	}
	// Reject invalid trigger before any additional provider call.
	calls := len(*e.requests)
	invalid, _ := e.request(t, "POST", "/v1/workflows/order-flow", e.token, `{"input":{"customerId":"C-1"}}`)
	if invalid.Code == 200 || len(*e.requests) != calls {
		t.Fatal("invalid trigger reached provider", invalid.Body.String())
	}
	response, payload = e.request(t, "POST", "/v1/workflows/order-flow?async=1", e.token, `{"input":{"customerId":"C-1","vip":false}}`)
	if response.Code != 202 {
		t.Fatal(response.Code, response.Body.String())
	}
	asyncID := payload["data"].(map[string]any)["operationId"].(string)
	// Admission flags do not invalidate a previously accepted immutable snapshot.
	shared := e.db.WorkflowCodeRunner()
	e.db.ConfigureWorkflows(workflow.Features{}, shared)
	blocked, _ := e.request(t, "POST", "/v1/workflows/order-flow?async=1", e.token, "")
	if blocked.Code != 409 {
		t.Fatal("new disabled run accepted", blocked.Code)
	}
	e.drainWorkflowJob(t, asyncID)
	_, status := e.runStatus(t, e.token, asyncID)
	final := status["data"].(map[string]any)
	if final["status"] != "success" || final["output"].(map[string]any)["total"] != float64(3) {
		t.Fatalf("frozen accepted run: %#v", final)
	}
	e.db.ConfigureWorkflows(workflow.Features{V2Enabled: true, CodeEnabled: true}, shared)
	planned := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	testutil.Exec(t, e.db, `UPDATE workflows SET schedule_type='interval',cron_expression='每 15 分钟',next_run_at=$2 WHERE id=$1`, item.ID, planned)
	n, _, err := e.db.EnqueueDueWorkflows(context.Background(), 10, e.codec)
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	var scheduleID string
	err = e.db.Pool().QueryRow(context.Background(), `SELECT id::text FROM operation_runs WHERE workflow_id=$1 AND source='schedule'`, item.ID).Scan(&scheduleID)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := e.db.WorkflowRunSnapshot(context.Background(), e.codec, scheduleID)
	if err != nil || !frozen.RunMetadata.ScheduledFor.Equal(planned) {
		t.Fatal("scheduled metadata", err)
	}
	e.drainWorkflowJob(t, scheduleID)
	scheduled, err := e.db.Operation(context.Background(), scheduleID)
	if err != nil || scheduled.Status != "success" {
		t.Fatal("scheduled real worker run", scheduled, err)
	}
}

func TestV2PreviewAndVersionGuards(t *testing.T) {
	e := newWorkflowEnv(t)
	item := e.deployCodeWorkflow(t)
	stale := e.editorRequest(t, item.ID, "owner", `{"expectedVersion":0}`, e.api.pauseWorkflow)
	if stale.Code != 409 {
		t.Fatal("invalid expected version accepted")
	}
	def, err := workflow.ParseDefinition(item.Graph)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"definition": def, "stepId": "calculate", "previewMode": "code_inputs", "sampleInput": map[string]any{"price": 1000, "quantity": 2, "discount": 100}})
	before := len(*e.requests)
	preview := e.editorRequest(t, "", "developer", string(body), e.api.previewWorkflowStep)
	if preview.Code != 200 || !bytes.Contains(preview.Body.Bytes(), []byte("1900")) || len(*e.requests) != before {
		t.Fatalf("preview must run only sandbox: %d %s", preview.Code, preview.Body.String())
	}
	var operations int
	if err := e.db.Pool().QueryRow(context.Background(), `SELECT count(*) FROM operation_runs WHERE workflow_id=$1`, item.ID).Scan(&operations); err != nil || operations != 0 {
		t.Fatal("preview created operation", operations, err)
	}
	denied := e.editorRequest(t, "", "viewer", string(body), e.api.previewWorkflowStep)
	if denied.Code != 403 {
		t.Fatal("viewer posted samples")
	}
	body, _ = json.Marshal(map[string]any{"definition": def, "stepId": "calculate", "previewMode": "workflow", "trigger": map[string]any{"vip": true}, "sampleOutputs": map[string]any{"trigger": map[string]any{"vip": false}}})
	conflict := e.editorRequest(t, "", "developer", string(body), e.api.previewWorkflowStep)
	if conflict.Code != 400 {
		t.Fatal("sample namespace collision accepted", conflict.Body.String())
	}
	for _, samples := range []map[string]any{
		{"sampleOutputs": map[string]any{"create_order": map[string]any{"data": "unrelated"}}},
		{"sampleStatuses": map[string]any{"create_order": "success"}},
		{"sampleOutputs": map[string]any{"route": map[string]any{"branchId": "unknown"}}},
		{"stepId": "create_order", "sampleOutputs": map[string]any{"route": map[string]any{"branchId": "vip"}}, "sampleStatuses": map[string]any{"normal": "success"}},
	} {
		samples["definition"], samples["previewMode"] = def, "workflow"
		if samples["stepId"] == nil {
			samples["stepId"] = "calculate"
		}
		body, _ = json.Marshal(samples)
		rejected := e.editorRequest(t, "", "developer", string(body), e.api.previewWorkflowStep)
		if rejected.Code != 400 {
			t.Fatal("out-of-scope or inconsistent sample accepted", rejected.Code, rejected.Body.String())
		}
	}
	e.db.ConfigureWorkflows(workflow.Features{V2Enabled: true}, e.db.WorkflowCodeRunner())
	body, _ = json.Marshal(map[string]any{"definition": def, "stepId": "calculate", "previewMode": "code_inputs", "sampleInput": map[string]any{}})
	disabled := e.editorRequest(t, "", "developer", string(body), e.api.previewWorkflowStep)
	if disabled.Code != 409 {
		t.Fatal("disabled code preview accepted", disabled.Code)
	}
	time.Sleep(220 * time.Millisecond)
	body, _ = json.Marshal(map[string]any{"definition": def, "stepId": "normal", "previewMode": "workflow", "sampleOutputs": map[string]any{"route": map[string]any{"branchId": "otherwise"}}, "sampleVariables": map[string]any{"discount": 0, "total": 0}})
	local := e.editorRequest(t, "", "developer", string(body), e.api.previewWorkflowStep)
	if local.Code != 200 || !bytes.Contains(local.Body.Bytes(), []byte(`"result":3`)) {
		t.Fatal("code gate blocked a pure preview", local.Code, local.Body.String())
	}
}

func TestV2APISuccessAssignmentFailureStopsWithResponsePreserved(t *testing.T) {
	e := newWorkflowEnv(t)
	item := e.deployCodeWorkflow(t)
	def, err := workflow.ParseDefinition(item.Graph)
	if err != nil {
		t.Fatal(err)
	}
	for i := range def.Steps {
		if def.Steps[i].ID == "create_order" {
			def.Steps[i].OnError = "continue"
			def.Steps[i].Assign = []workflow.Assignment{{Variable: "total", Value: json.RawMessage(`"wrong type"`)}}
		}
	}
	def.Steps = append(def.Steps, workflow.Step{ID: "must_not_call", Type: "api", Action: "test.create", IntegrationID: e.fixture.Integration.ID, ConnectionKey: e.fixture.Connection.ID, DependsOn: []string{"create_order"}, Input: map[string]any{"name": "Alice", "total": 1}})
	item.Graph, _ = json.Marshal(def)
	item, err = e.db.SaveWorkflow(context.Background(), item, item.Version)
	if err != nil {
		t.Fatal(err)
	}
	deployed := e.editorRequest(t, item.ID, "owner", fmt.Sprintf(`{"expectedVersion":%d}`, item.Version), e.api.deployWorkflow)
	if deployed.Code != 200 {
		t.Fatal(deployed.Code, deployed.Body.String())
	}
	before := len(*e.requests)
	response, payload := e.request(t, "POST", "/v1/workflows/order-flow", e.token, `{"input":{"customerId":"C-1","vip":true}}`)
	if response.Code == 200 || len(*e.requests)-before != 2 {
		t.Fatal("assignment failure reached downstream", response.Code, len(*e.requests)-before, response.Body.String())
	}
	id := payload["meta"].(map[string]any)["operationId"].(string)
	operation, err := e.db.Operation(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range operation.Events {
		var attrs map[string]any
		_ = json.Unmarshal(event.Attributes, &attrs)
		if attrs["stepId"] == "create_order" && attrs["eventType"] == "workflow.step.finished" {
			found = true
			if attrs["apiCallStatus"] != "success" || attrs["assignmentStatus"] != "failed" || attrs["phase"] != "variables" || attrs["outputPreview"] != nil || attrs["responsePreview"] == nil {
				t.Fatal(attrs)
			}
		}
	}
	if !found {
		t.Fatal("missing API failure event")
	}
}

func TestV2MultipleAPIInputsAndFrozenBusinessDateToCode(t *testing.T) {
	e := newWorkflowEnv(t)
	worker, launcher := os.Getenv("APIHUB_TEST_CODE_WORKER"), os.Getenv("APIHUB_TEST_CODE_LAUNCHER")
	if worker == "" || launcher == "" {
		t.Skip("real sandbox paths required")
	}
	e.db.ConfigureWorkflows(workflow.Features{V2Enabled: true, CodeEnabled: true}, workflow.NewCodeRunner(worker, launcher, 2))
	var calls atomic.Int32
	orders := make(chan map[string]any, 3)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/price":
			_, _ = io.WriteString(w, `{"data":{"price":1000}}`)
		case "/quantity":
			_, _ = io.WriteString(w, `{"data":{"quantity":2}}`)
		case "/discount":
			_, _ = io.WriteString(w, `{"data":{"discount":100}}`)
		case "/orders":
			var input map[string]any
			_ = json.NewDecoder(r.Body).Decode(&input)
			orders <- input
			_, _ = io.WriteString(w, `{"data":{"orderId":"O-multiple"}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(provider.Close)
	testutil.Exec(t, e.db, `UPDATE integrations SET base_url=$2 WHERE id=$1`, e.fixture.Integration.ID, provider.URL)
	steps := []workflow.Step{}
	for _, name := range []string{"price", "quantity", "discount"} {
		action, err := e.db.SaveAction(context.Background(), model.ActionDefinition{ActionKey: "multi." + name, Name: name, RequiredScopes: []string{}, SystemID: e.fixture.System.ID, IntegrationID: e.fixture.Integration.ID, HTTPMethod: "GET", RelativePath: "/" + name, Executable: true, Status: "active", InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{}`)})
		if err != nil {
			t.Fatal(err)
		}
		steps = append(steps, workflow.Step{ID: name, Type: "api", Action: action.ID, IntegrationID: e.fixture.Integration.ID, ConnectionKey: e.fixture.Connection.ID, Input: map[string]any{}})
	}
	var code workflow.Step
	_ = json.Unmarshal([]byte(`{"id":"calculate","type":"code","dependsOn":["price","quantity","discount"],"language":"javascript","runtimeProfile":"js-v1","code":"function main(input){return {total:input.price*input.quantity-input.discount,query:{date:input.date}};}","inputSchema":{"type":"object","properties":{"price":{"type":"integer"},"quantity":{"type":"integer"},"discount":{"type":"integer"},"date":{"type":"string"}},"required":["price","quantity","discount","date"]},"input":{"price":"{{price.data.price}}","quantity":"{{quantity.data.quantity}}","discount":"{{discount.data.discount}}","date":{"$value":{"kind":"run","field":"businessDate"}}},"outputSchema":{"type":"object","properties":{"total":{"type":"integer"},"query":{"type":"object","properties":{"date":{"type":"string"}},"required":["date"]}},"required":["total","query"]}}`), &code)
	steps = append(steps, code, workflow.Step{ID: "order", Type: "api", Action: "test.create", IntegrationID: e.fixture.Integration.ID, ConnectionKey: e.fixture.Connection.ID, DependsOn: []string{"calculate"}, Input: map[string]any{"total": "{{calculate.total}}", "query": "{{calculate.query}}"}})
	graph, _ := json.Marshal(workflow.Definition{SchemaVersion: 2, Steps: steps, Output: map[string]any{"orderId": "{{order.data.orderId}}", "query": "{{calculate.query}}", "total": "{{calculate.total}}"}})
	item, err := e.db.SaveWorkflow(context.Background(), model.Workflow{WorkflowKey: "multi-api-code", Name: "multiple sources", Graph: graph, ScheduleType: "manual", ScheduleTimezone: "Asia/Singapore", Input: json.RawMessage(`{}`)}, 0)
	if err != nil {
		t.Fatal(err)
	}
	deployed := e.editorRequest(t, item.ID, "developer", fmt.Sprintf(`{"expectedVersion":%d}`, item.Version), e.api.deployWorkflow)
	if deployed.Code != 200 || calls.Load() != 0 {
		t.Fatal("deployment executed provider", deployed.Code, deployed.Body.String(), calls.Load())
	}
	item, _ = e.db.Workflow(context.Background(), item.ID)
	response, payload := e.request(t, "POST", "/v1/workflows/multi-api-code", e.token, "")
	if response.Code != 200 || payload["data"].(map[string]any)["total"] != float64(1900) {
		t.Fatal(response.Code, response.Body.String())
	}
	syncID := payload["meta"].(map[string]any)["operationId"].(string)
	frozen, _ := e.db.WorkflowRunSnapshot(context.Background(), e.codec, syncID)
	checkOrder := func(date string) {
		t.Helper()
		select {
		case input := <-orders:
			if input["total"] != float64(1900) || input["query"].(map[string]any)["date"] != date {
				t.Fatal("downstream lost named values", input, date)
			}
		default:
			t.Fatal("downstream did not execute")
		}
	}
	checkOrder(frozen.RunMetadata.BusinessDate)
	response, payload = e.request(t, "POST", "/v1/workflows/multi-api-code?async=1", e.token, "")
	if response.Code != 202 {
		t.Fatal(response.Code, response.Body.String())
	}
	asyncID := payload["data"].(map[string]any)["operationId"].(string)
	e.drainWorkflowJob(t, asyncID)
	frozen, _ = e.db.WorkflowRunSnapshot(context.Background(), e.codec, asyncID)
	checkOrder(frozen.RunMetadata.BusinessDate)
	planned := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Second)
	testutil.Exec(t, e.db, `UPDATE workflows SET schedule_type='interval',cron_expression='每 15 分钟',next_run_at=$2 WHERE id=$1`, item.ID, planned)
	n, _, err := e.db.EnqueueDueWorkflows(context.Background(), 10, e.codec)
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	var scheduledID string
	err = e.db.Pool().QueryRow(context.Background(), `SELECT id::text FROM operation_runs WHERE workflow_id=$1 AND source='schedule'`, item.ID).Scan(&scheduledID)
	if err != nil {
		t.Fatal(err)
	}
	e.drainWorkflowJob(t, scheduledID)
	metadata, _ := workflow.NewRunMetadata(time.Now().UTC(), &planned, "Asia/Singapore")
	checkOrder(metadata.BusinessDate)
	if calls.Load() != 12 {
		t.Fatal("each run must call each API exactly once", calls.Load())
	}
}
