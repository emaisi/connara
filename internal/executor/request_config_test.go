package executor

import (
	"apihub-go/internal/model"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func definition(method, format string, params string) *model.HTTPActionRuntime {
	return &model.HTTPActionRuntime{Method: method, Path: "/resources", RequestConfig: json.RawMessage(`{"schemaVersion":1,"bodyFormat":"` + format + `","parameters":` + params + `,"headers":[]}`)}
}
func TestPreviewAndExecutionShareEncodedTarget(t *testing.T) {
	var actual string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actual = r.URL.RequestURI()
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer upstream.Close()
	cfg := definition("GET", "none", `[{"name":"term","in":"query","type":"string"},{"name":"enabled","in":"query","type":"boolean"},{"name":"ids","in":"query","type":"array"}]`)
	input := map[string]any{"term": "中文 空格", "enabled": false, "ids": []any{"1", "2"}}
	req, err := BuildRequest(upstream.URL+"/api-hub/", cfg, input)
	if err != nil {
		t.Fatal(err)
	}
	preview := RequestPreview(req)
	_, err = New(NewGuardedClient()).Action(context.Background(), model.Provider{BaseURL: upstream.URL + "/api-hub/"}, model.Action{Runtime: cfg}, input, nil)
	if err != nil {
		t.Fatal(err)
	}
	if actual != req.URL.RequestURI() || !strings.Contains(preview.URL, "/api-hub/resources?") || !strings.Contains(actual, "enabled=false") || !strings.Contains(actual, "ids=1&ids=2") {
		t.Fatalf("preview/actual mismatch: %s %s", preview.URL, actual)
	}
}
func TestMappedBodiesRetainTypesAndEncoding(t *testing.T) {
	for _, format := range []string{"json", "form"} {
		cfg := definition("POST", format, `[{"name":"enabled","in":"body","type":"boolean"},{"name":"count","in":"body","type":"integer"},{"name":"title","in":"body","type":"string"}]`)
		req, err := BuildRequest("https://example.test/root", cfg, map[string]any{"enabled": false, "count": json.Number("0"), "title": "a&中文"})
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(req.Body)
		if format == "json" && !strings.Contains(string(b), `"enabled":false`) {
			t.Fatal(string(b))
		}
		if format == "form" && (!strings.Contains(string(b), "title=a%26") || !strings.Contains(string(b), "count=0")) {
			t.Fatal(string(b))
		}
	}
}
func TestResponseChecksRequireBusinessAndStructureSuccess(t *testing.T) {
	cfg := definition("GET", "none", "[]")
	cfg.ResponseConfig = []byte(`{"successCondition":{"path":"$.code","operator":"equals","value":0}}`)
	schema := map[string]any{"type": "object", "required": []any{"data"}}
	for _, body := range []string{`{"code":"0","data":[]}`, `{"code":1,"data":[]}`, `{"code":0}`, `not JSON`} {
		if err := CheckResponse(cfg, 200, []byte(body), schema); err == nil {
			t.Fatalf("invalid response accepted: %s", body)
		}
	}
	if err := CheckResponse(cfg, 200, []byte(`{"code":0.0,"data":[]}`), schema); err != nil {
		t.Fatal(err)
	}
}
func TestUnsafeDefinitionsAreRejected(t *testing.T) {
	for _, params := range []string{`[{"name":"Authorization","in":"header","type":"string"}]`, `[{"name":"a","in":"body","type":"string"},{"name":"a.b","in":"body","type":"string"}]`, `[{"name":"password","in":"body","type":"string"}]`} {
		if ValidateConfig(definition("POST", "json", params)) == nil {
			t.Fatal(params)
		}
	}
	if ValidateSchema([]byte(`{"$ref":"https://other.test/schema"}`)) == nil {
		t.Fatal("external schema accepted")
	}
}
func TestNetworkFailureClassification(t *testing.T) {
	cfg := definition("GET", "none", "[]")
	_, err := New(NewGuardedClient()).Action(context.Background(), model.Provider{BaseURL: "http://127.0.0.1:1"}, model.Action{Runtime: cfg}, nil, nil)
	var failure *RequestFailure
	if !errors.As(err, &failure) || failure.Sent || OutcomeUnknown(err) {
		t.Fatalf("connection refusal cannot be unknown: %v", err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.(http.Hijacker)
		c, _, _ := h.Hijack()
		_ = c.Close()
	}))
	defer upstream.Close()
	cfg.Method = "POST"
	cfg.RequestConfig = []byte(`{"schemaVersion":1,"bodyFormat":"json","parameters":[],"headers":[]}`)
	_, err = New(upstream.Client()).Action(context.Background(), model.Provider{BaseURL: upstream.URL}, model.Action{Runtime: cfg}, nil, nil)
	if !OutcomeUnknown(err) {
		t.Fatalf("request sent without response must be unknown: %v", err)
	}
}
