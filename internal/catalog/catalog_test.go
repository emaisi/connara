package catalog

import (
	"os"
	"path/filepath"
	"testing"

	"apihub-go/internal/model"
)

func TestLoadOpenConnectorMetadataAsCatalogOnly(t *testing.T) {
	directory := t.TempDir()
	provider := `{
  "service":"example",
  "displayName":"Example",
  "categories":["Developer Tools"],
  "authTypes":["no_auth"],
  "auth":[{"type":"no_auth"}],
  "actions":[{
    "id":"example.echo",
    "service":"example",
    "name":"echo",
    "description":"Echo a value",
    "requiredScopes":[],
    "providerPermissions":[],
    "inputSchema":{"type":"object","required":["value"],"properties":{"value":{"type":"string"}}},
    "outputSchema":{"type":"object"}
  }]
}`
	if err := os.WriteFile(filepath.Join(directory, "example.json"), []byte(provider), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	action, ok := catalog.Action("example.echo")
	if !ok {
		t.Fatal("external action was not loaded")
	}
	if action.Executable {
		t.Fatal("metadata-only action must not be advertised as executable")
	}
	if err := catalog.ValidateInput(action, map[string]any{"value": "ok"}); err != nil {
		t.Fatalf("valid input failed: %v", err)
	}
	if err := catalog.ValidateInput(action, map[string]any{}); err == nil {
		t.Fatal("required input should fail schema validation")
	}
}

func TestValidateInputCacheTracksSchemaChanges(t *testing.T) {
	catalog, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	action := model.Action{
		ID:          "custom.changed",
		InputSchema: map[string]any{"type": "object", "required": []any{"first"}},
	}
	if err := catalog.ValidateInput(action, map[string]any{"first": "ok"}); err != nil {
		t.Fatalf("first schema should accept input: %v", err)
	}
	action.InputSchema = map[string]any{"type": "object", "required": []any{"second"}}
	if err := catalog.ValidateInput(action, map[string]any{"first": "stale"}); err == nil {
		t.Fatal("updated schema must not reuse the previous compiled schema")
	}
}
