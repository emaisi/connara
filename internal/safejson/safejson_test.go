package safejson

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMarshalRedactsSensitiveKeys(t *testing.T) {
	value := map[string]any{
		"orderId": "O-1",
		"apiKey":  "leak",
		"nested":  map[string]any{"access_token": "leak", "name": "Alice"},
		"items":   []any{map[string]any{"clientSecret": "leak"}},
	}
	data := string(Marshal(value, 1<<16))
	if !strings.Contains(data, `"orderId":"O-1"`) {
		t.Fatalf("orderId must survive: %s", data)
	}
	if strings.Contains(data, "leak") {
		t.Fatalf("secret leaked: %s", data)
	}
	if !strings.Contains(data, "[REDACTED]") {
		t.Fatalf("expected redaction marker: %s", data)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(data), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestMarshalTruncates(t *testing.T) {
	big := map[string]any{"data": strings.Repeat("x", 4096)}
	if got := string(Marshal(big, 1024)); got != `{"truncated":true}` {
		t.Fatalf("expected truncation marker, got %q", got)
	}
}

func TestMarshalNil(t *testing.T) {
	if got := Marshal(nil, 10); got != nil {
		t.Fatalf("nil input must stay nil, got %q", got)
	}
}

func TestSensitiveKey(t *testing.T) {
	for _, key := range []string{"Authorization", "api_key", "clientSecret", "refresh-token", "userPassword", "apiKey"} {
		if !SensitiveKey(key) {
			t.Fatalf("key %q must be sensitive", key)
		}
	}
	for _, key := range []string{"orderId", "name", "tokens", "tokenized"} {
		if SensitiveKey(key) {
			t.Fatalf("key %q must not be sensitive", key)
		}
	}
}
