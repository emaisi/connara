package executor_test

import (
	"apihub-go/internal/catalog"
	"apihub-go/internal/executor"
	"apihub-go/internal/model"
	"encoding/json"
	"strings"
	"testing"
)

func TestDefaultsAreValidatedBeforeEveryRequest(t *testing.T) {
	c, err := catalog.Load("")
	if err != nil {
		t.Fatal(err)
	}
	runtime := &model.HTTPActionRuntime{Method: "GET", Path: "/items", RequestConfig: []byte(`{"schemaVersion":1,"bodyFormat":"none","parameters":[{"name":"page","in":"query","type":"integer","required":true,"default":0}],"headers":[]}`)}
	raw := json.RawMessage(`{"type":"object","properties":{"page":{"type":"integer","minimum":1}},"required":["page"],"additionalProperties":false}`)
	if executor.ValidateInputContract(runtime, raw) == nil {
		t.Fatal("invalid default accepted at save")
	}
	var schema map[string]any
	json.Unmarshal(raw, &schema)
	if c.ValidateInput(model.Action{Runtime: runtime, InputSchema: schema}, map[string]any{}) == nil {
		t.Fatal("default bypassed minimum")
	}
	runtime.RequestConfig = []byte(strings.ReplaceAll(string(runtime.RequestConfig), `"default":0`, `"default":2`))
	if err := executor.ValidateInputContract(runtime, raw); err != nil {
		t.Fatal(err)
	}
	input := map[string]any{}
	if err := c.ValidateInput(model.Action{Runtime: runtime, InputSchema: schema}, input); err != nil {
		t.Fatal("required default rejected:", err)
	}
	req, err := executor.BuildRequest("https://example.test/prefix", runtime, input)
	if err != nil || req.URL.Query().Get("page") != "2" || len(input) != 0 {
		t.Fatalf("default request mismatch: %v %v", req, err)
	}
	mismatch := json.RawMessage(strings.ReplaceAll(string(raw), `"type":"integer"`, `"type":"string"`))
	if executor.ValidateInputContract(runtime, mismatch) == nil {
		t.Fatal("mapping type mismatch accepted")
	}
}
func TestArraySchemaMatchesMappedScalarQuery(t *testing.T) {
	runtime := &model.HTTPActionRuntime{Method: "GET", Path: "/items", RequestConfig: []byte(`{"schemaVersion":1,"bodyFormat":"none","parameters":[{"name":"ids","in":"query","type":"array"}],"headers":[]}`)}
	raw := json.RawMessage(`{"type":"object","properties":{"ids":{"type":"array","items":{"type":["string","number","boolean"]}}},"additionalProperties":false}`)
	if err := executor.ValidateInputContract(runtime, raw); err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	json.Unmarshal(raw, &schema)
	c, _ := catalog.Load("")
	input := map[string]any{"ids": []any{json.Number("0"), false, "中文"}}
	if err := c.ValidateInput(model.Action{Runtime: runtime, InputSchema: schema}, input); err != nil {
		t.Fatal(err)
	}
	req, err := executor.BuildRequest("https://example.test", runtime, input)
	if err != nil || len(req.URL.Query()["ids"]) != 3 {
		t.Fatalf("array lost: %v %v", req, err)
	}
}
func TestSensitiveAndCaseDuplicateHeadersCannotBeDefined(t *testing.T) {
	for _, name := range []string{"ApiKey", "API_KEY", "api-key", "Authorization", "Cookie", "X-Api-Key"} {
		for _, cfg := range []string{`{"schemaVersion":1,"bodyFormat":"none","parameters":[],"headers":[{"name":"` + name + `","value":"dummy"}]}`, `{"schemaVersion":1,"bodyFormat":"none","parameters":[{"name":"` + name + `","in":"header","type":"string"}],"headers":[]}`} {
			if executor.ValidateConfig(&model.HTTPActionRuntime{Method: "GET", Path: "/", RequestConfig: []byte(cfg)}) == nil {
				t.Fatalf("secret header %s accepted", name)
			}
		}
	}
	cfg := []byte(`{"schemaVersion":1,"bodyFormat":"none","parameters":[{"name":"X-Id","in":"header","type":"string"},{"name":"x-id","in":"header","type":"string"}],"headers":[]}`)
	if executor.ValidateConfig(&model.HTTPActionRuntime{Method: "GET", Path: "/", RequestConfig: cfg}) == nil {
		t.Fatal("case duplicate accepted")
	}
}
func TestResponseSchemaDiagnosticsDoNotExposeValues(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "integer"}}}
	_, err := executor.CheckResponseDetailed(&model.HTTPActionRuntime{}, 200, []byte(`{"id":"private-provider-value"}`), schema)
	if err == nil || !strings.Contains(err.Error(), `$["id"]`) || !strings.Contains(err.Error(), "integer") || strings.Contains(err.Error(), "private-provider-value") {
		t.Fatalf("unsafe or missing diagnostic: %v", err)
	}
}
