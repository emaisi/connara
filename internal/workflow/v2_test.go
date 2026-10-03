package workflow

import (
	"apihub-go/internal/executor"
	"apihub-go/internal/jsonutil"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestV2BranchJoinAndAtomicVariables(t *testing.T) {
	raw := []byte(`{"schemaVersion":2,"variables":[{"name":"rate","type":"integer","initial":0}],"steps":[{"id":"choose","type":"condition","branches":[{"id":"vip","condition":{"path":"trigger.vip","op":"eq","value":true},"assign":[{"variable":"rate","value":10}]},{"id":"otherwise","default":true,"assign":[{"variable":"rate","value":3}]}]},{"id":"vip","type":"transform","dependsOn":["choose"],"scope":[{"conditionId":"choose","branchId":"vip"}],"source":"{{trigger.items}}","operations":[{"op":"slice","limit":10}]},{"id":"normal","type":"transform","dependsOn":["choose"],"scope":[{"conditionId":"choose","branchId":"otherwise"}],"source":"{{missing.fields}}","operations":[{"op":"count"}]},{"id":"joined","type":"condition","dependsOn":["vip","normal"],"branches":[{"id":"high","condition":{"path":"vars.rate","op":"gte","value":10}},{"id":"otherwise","default":true}]}],"output":{"branch":"{{joined.branchId}}","rate":"{{vars.rate}}","normalStatus":"{{status.normal}}","optionalNormal":"{{?normal.result}}"}}`)
	// Replace the inactive test source with a valid declared upstream reference.
	raw = []byte(strings.Replace(string(raw), "{{missing.fields}}", "{{choose.nonexistent}}", 1))
	def, err := ParseDefinition(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDefinition(def, false, nil); err != nil {
		t.Fatal(err)
	}
	metadata, _ := NewRunMetadata(time.Date(2026, 10, 3, 16, 5, 0, 0, time.UTC), nil, "Asia/Singapore")
	trigger := map[string]any{"vip": true, "items": []any{json.Number("1"), json.Number("2")}}
	vars, err := InitialVariables(def, trigger, metadata)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := Snapshot{SchemaVersion: 2, Variables: def.Variables, InitialVariables: vars, RunMetadata: metadata, Trigger: trigger, Output: def.Output}
	for _, step := range def.Steps {
		snapshot.Steps = append(snapshot.Steps, StepToSnapshot(step))
	}
	runner := Runner{Deps: &fakeDeps{}}
	result, err := runner.Run(context.Background(), snapshot, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(result.Final["rate"]) != "10" || result.Final["branch"] != "high" || result.Outcomes[2].Status != StatusSkipped || result.Final["normalStatus"] != "skipped" || result.Final["optionalNormal"] != nil {
		t.Fatalf("unexpected join: %+v", result)
	}
	assignments := []Assignment{{Variable: "rate", Value: json.RawMessage(`5`)}, {Variable: "other", Value: json.RawMessage(`"invalid"`)}}
	if _, err := PrepareAssignments(def.Variables, assignments, map[string]any{"vars": vars}, metadata); err == nil {
		t.Fatal("invalid assignment should fail")
	}
	if fmt.Sprint(vars["rate"]) != "0" {
		t.Fatal("candidate assignment mutated original variables")
	}
}
func TestRunMetadataDateAndLiteralCompatibility(t *testing.T) {
	planned := time.Date(2026, 10, 3, 15, 55, 0, 0, time.UTC)
	actual := planned.Add(10 * time.Minute)
	schedule, _ := NewRunMetadata(actual, &planned, "Asia/Singapore")
	manual, _ := NewRunMetadata(actual, nil, "Asia/Singapore")
	if schedule.BusinessDate != "2026-10-03" || manual.BusinessDate != "2026-10-04" {
		t.Fatal("business dates use wrong basis")
	}
	var marker any
	_ = jsonutil.Unmarshal([]byte(`{"$value":{"kind":"literal","value":"{{unavailable.secret}}"}}`), &marker)
	value, err := ResolveV2(nil, marker, manual, false)
	if err != nil || value != "{{unavailable.secret}}" {
		t.Fatal(value, err)
	}
	value, err = Resolve(nil, marker)
	if err == nil {
		t.Fatal("legacy marker must retain normal template semantics")
	}
}
func TestV2RejectsUnknownAndUnorderedVariables(t *testing.T) {
	for _, raw := range []string{`{"schemaVersion":3,"steps":[],"output":{}}`, `{"schemaVersion":2,"steps":[{"id":"x","type":"python"}],"output":{}}`, `{"schemaVersion":2,"steps":[{"id":"vars","type":"condition"}],"output":{}}`} {
		def, err := ParseDefinition([]byte(raw))
		if err == nil {
			err = ValidateDefinition(def, false, nil)
		}
		if err == nil {
			t.Fatal("unsupported definition accepted")
		}
	}
}
func TestCodeAdmissionReservesFormalCapacity(t *testing.T) {
	runner := NewCodeRunner("", "", 2)
	release, err := runner.acquire(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := runner.acquire(context.Background(), false); CodeErrorCode(err) != "code_capacity_busy" {
		t.Fatal("preview consumed formal reserve")
	}
	formal, err := runner.acquire(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	formal()
	single := NewCodeRunner("", "", 1)
	active, err := single.BeginFormal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := single.acquire(context.Background(), false); CodeErrorCode(err) != "code_capacity_busy" {
		t.Fatal("preview entered active formal run")
	}
	active()
	inter, err := single.acquire(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := single.BeginFormal(ctx); err == nil {
		t.Fatal("pre-existing interactive slot not drained")
	}
	inter()
	again, err := single.acquire(context.Background(), false)
	if err != nil {
		t.Fatal("failed formal registration leaked")
	}
	again()
}
func TestRealRestrictedWorker(t *testing.T) {
	worker := os.Getenv("APIHUB_TEST_CODE_WORKER")
	launcher := os.Getenv("APIHUB_TEST_CODE_LAUNCHER")
	if worker == "" || launcher == "" {
		t.Skip("set APIHUB_TEST_CODE_WORKER and APIHUB_TEST_CODE_LAUNCHER for real sandbox checks")
	}
	runner := NewCodeRunner(worker, launcher, 2)
	if err := runner.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, code, want string }{
		{"empty_object", `function main(){return {};}`, ""},
		{"stack", `function main(){return main();}`, "code_stack_limit"},
		{"iterator_tampering", `Array.prototype[Symbol.iterator]=function*(){};function main(){return {bad:undefined};}`, "code_output_invalid"},
		{"toJSON_tampering", `Object.prototype.toJSON=function(){return {wrong:1};};function main(input){return {total:1900};}`, ""},
		{"native_timeout", `function main(){return {text:"a".repeat(2147483647)};}`, "code_worker_failed"},
		{"calculation", `function main(input){return {total:input.price*input.quantity-input.discount};}`, ""},
		{"compile_only", `throw new Error('must not execute');function main(input){return {};}`, ""},
		{"generator_constructor", `function main(){return {value:(function*(){}).constructor("yield 5")().next().value};}`, "code_execution_failed"},
		{"unsupported_clock", `function main(){return {now:Date.now()};}`, "code_execution_failed"},
		{"accessor", `function main(){return {get secret(){return 1}};}`, "code_output_invalid"},
		{"undefined", `function main(){return {value:undefined};}`, "code_output_invalid"},
		{"unsafe_integer", `function main(){return {value:9007199254740992};}`, "code_output_invalid"},
		{"async", `async function main(){return {};}`, "code_compile_error"},
		{"timeout", `function main(){for(;;){}}`, "code_timeout"},
	} {
		t.Run(test.name, func(t *testing.T) {
			step := Step{ID: test.name, Type: "code", RuntimeProfile: "js-v1", Code: test.code}
			if test.name == "compile_only" {
				if err := runner.Compile(context.Background(), step, false); err != nil {
					t.Fatal(err)
				}
				return
			}
			pinned := StepToSnapshot(step)
			response, err := runner.Execute(context.Background(), pinned, map[string]any{"price": json.Number("1000"), "quantity": json.Number("2"), "discount": json.Number("100")}, false)
			if test.want == "" {
				if err != nil || response.Output == nil || test.name != "empty_object" && fmt.Sprint(response.Output["total"]) != "1900" {
					t.Fatal(response, err)
				}
			} else if CodeErrorCode(err) != test.want {
				t.Fatalf("want %s, got %v", test.want, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := runner.Execute(ctx, StepToSnapshot(Step{ID: "cancel", Type: "code", RuntimeProfile: "js-v1", Code: `function main(){for(;;){}}`}), map[string]any{}, false)
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled worker succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not clean up worker")
	}
	if err := runner.Probe(context.Background()); err != nil {
		t.Fatalf("worker not reusable after failure: %v", err)
	}
}

func TestTransformPipelineStableDateOrderAndCancellation(t *testing.T) {
	var source any
	_ = jsonutil.Unmarshal([]byte(`[{"id":"later","paid":true,"at":"2026-10-03T09:00:00Z"},{"id":"first","paid":true,"at":"2026-10-03T16:00:00+08:00"},{"id":"equal","paid":true,"at":"2026-10-03T08:00:00Z"},{"id":"skip","paid":false,"at":null},{"id":"missing","paid":true}]`), &source)
	before, _ := json.Marshal(source)
	var operations []map[string]any
	_ = jsonutil.Unmarshal([]byte(`[{"op":"filter","condition":{"itemPath":"paid","op":"eq","value":true}},{"op":"sort","path":"at","valueType":"datetime","direction":"asc"},{"op":"slice","offset":0,"limit":3},{"op":"select","fields":[{"path":"id","as":"orderId"}]}]`), &operations)
	output, _, err := Transform(context.Background(), source, operations, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(output)
	if string(raw) != `{"result":[{"orderId":"first"},{"orderId":"equal"},{"orderId":"later"}]}` {
		t.Fatal(string(raw))
	}
	after, _ := json.Marshal(source)
	if string(before) != string(after) {
		t.Fatal("transform mutated input")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := Transform(ctx, source, operations, nil, nil); err == nil {
		t.Fatal("cancelled transform returned output")
	}
	match, err := EvaluateV2(map[string]any{"trigger": map[string]any{"n": json.Number("9007199254740993")}}, &Condition{Path: "trigger.n", Op: "gt", Value: json.Number("9007199254740992")}, nil)
	if err != nil || !match {
		t.Fatal("numeric comparison lost precision", match, err)
	}
	if _, err := exactNumber(json.Number("1e100000000")); err == nil {
		t.Fatal("unbounded numeric expansion accepted")
	}
}

func TestV2ValidationRejectsUnsafeScopesAndPartialCodeInputs(t *testing.T) {
	for _, raw := range []string{
		`{"schemaVersion":2,"variables":[{"name":"x","type":"integer","initial":0}],"steps":[{"id":"c","type":"transform","source":"{{?vars.x}}","operations":[{"op":"count"}]}],"output":{}}`,
		`{"schemaVersion":2,"steps":[{"id":"c","type":"code","language":"javascript","runtimeProfile":"js-v1","code":"function main(){return {}}","inputSchema":{"type":"object","properties":{"x":{"type":"string"}},"required":[]},"outputSchema":{"type":"object"}}],"output":{}}`,
		`{"schemaVersion":2,"steps":[{"id":"c","type":"transform","source":[],"operations":[{"op":"count"},{"op":"slice","limit":1}]}],"output":{}}`,
	} {
		def, err := ParseDefinition([]byte(raw))
		if err == nil {
			err = ValidateDefinition(def, false, nil)
		}
		if err == nil {
			t.Fatal("invalid v2 accepted", raw)
		}
	}
}

func TestV2ConditionTraceAndExactEquality(t *testing.T) {
	condition := &Condition{Any: []*Condition{{Path: "trigger.flag", Op: "eq", Value: true}, {Path: "trigger.missing", Op: "gt", Value: json.Number("1")}}}
	matched, trace, err := EvaluateV2Traced(map[string]any{"trigger": map[string]any{"flag": true}}, condition, nil, "/branches/0/condition")
	if err != nil || !matched || len(trace) != 3 || trace[2]["status"] != "not_evaluated" {
		t.Fatal(matched, trace, err)
	}
	for _, pair := range [][2]string{{"9223372036854775808", "9223372036854775809"}, {"9007199254740993.0", "9007199254740992.0"}} {
		matched, err = EvaluateV2(map[string]any{"trigger": map[string]any{"n": json.Number(pair[0])}}, &Condition{Path: "trigger.n", Op: "eq", Value: json.Number(pair[1])}, nil)
		if err != nil || matched {
			t.Fatal("JSON equality rounded large numbers", pair, err)
		}
	}
	diagnostic := FieldDiagnostic(&executor.FieldError{Path: `$["customer.id/a~b"][0]`, Constraint: "type"}, "api1", "input", "/input")
	if diagnostic[0].FieldPath != "/input/customer.id~1a~0b/0" {
		t.Fatal(diagnostic)
	}
}
func TestSingleConcurrencyDrainsBeforeAnyWorkflowStep(t *testing.T) {
	code := NewCodeRunner("", "", 1)
	release, err := code.acquire(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	runner := Runner{Deps: &fakeDeps{}, CodeRunner: code}
	result, err := runner.Run(context.Background(), Snapshot{SchemaVersion: 2, Steps: []StepSnapshot{{ID: "api1", Type: "api"}, {ID: "code1", Type: "code", RunnerBuild: CodeBuild, RuntimeProfile: "js-v1", DependsOn: []string{"api1"}}}}, Options{})
	var failure *Error
	if !errors.As(err, &failure) || failure.Code != "code_capacity_busy" || len(result.Outcomes) != 0 {
		t.Fatal("formal admission did not precede API", result, err)
	}
}

func TestRealCodeDataScenarios(t *testing.T) {
	worker, launcher := os.Getenv("APIHUB_TEST_CODE_WORKER"), os.Getenv("APIHUB_TEST_CODE_LAUNCHER")
	if worker == "" || launcher == "" {
		t.Skip("real sandbox paths required")
	}
	runner := NewCodeRunner(worker, launcher, 2)
	for _, test := range []struct{ name, code, input, output string }{
		{"id_join", `function main(input){const right=new Map(input.right.map(row=>[row.id,row.name]));return {rows:input.left.map(row=>({id:row.id,name:right.get(row.id)||null}))};}`, `{"left":[{"id":"2"},{"id":"1"}],"right":[{"id":"1","name":"one"},{"id":"2","name":"two"}]}`, `{"rows":[{"id":"2","name":"two"},{"id":"1","name":"one"}]}`},
		{"group_sum", `function main(input){const sums={};for(const row of input.rows)sums[row.customer]=(sums[row.customer]||0)+row.cents;return {sums};}`, `{"rows":[{"customer":"A","cents":100},{"customer":"A","cents":200}]}`, `{"sums":{"A":300}}`},
		{"shape_and_frozen_date", `function main(input){return {rows:input.rows.map(row=>({id:row.id,active:row.amount>0})),date:input.date};}`, `{"rows":[{"id":"A","amount":0},{"id":"B","amount":20}],"date":"2026-10-03"}`, `{"date":"2026-10-03","rows":[{"active":false,"id":"A"},{"active":true,"id":"B"}]}`},
		{"input_mutation", `function main(input){input.rows.push(2);return {rows:input.rows};}`, `{"rows":[1]}`, `{"rows":[1,2]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var input map[string]any
			_ = jsonutil.Unmarshal([]byte(test.input), &input)
			before, _ := json.Marshal(input)
			response, err := runner.Execute(context.Background(), StepToSnapshot(Step{ID: test.name, Type: "code", RuntimeProfile: "js-v1", Code: test.code}), input, false)
			if err != nil {
				t.Fatal(err)
			}
			actual, _ := json.Marshal(response.Output)
			if string(actual) != test.output {
				t.Fatalf("%s != %s", actual, test.output)
			}
			after, _ := json.Marshal(input)
			if string(before) != string(after) {
				t.Fatal("code mutated caller input")
			}
		})
	}
	for _, number := range []string{"9007199254740992", "9007199254740992.0", "9.007199254740992e15"} {
		if CheckJSNumber(json.Number(number)) == nil {
			t.Fatal("unsafe integer accepted", number)
		}
	}
}
