package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"apihub-go/internal/model"
	"apihub-go/internal/store"
	"apihub-go/internal/testutil"
)

func TestAuthTemplateConfigurationRoundTripAndVersion(t *testing.T) {
	db := testutil.Database(t)
	ctx := context.Background()
	original := model.AuthTemplate{TemplateKey: "request-roundtrip", Name: "Request", FlowType: "password_token", CredentialSchema: json.RawMessage(`{"type":"object","fields":[{"name":"username","required":false}]}`), TokenRequest: json.RawMessage(`{"schemaVersion":2,"method":"GET","bodyType":"none","credentialMode":"mapped","parameters":[{"name":"account","target":"query","value":{"source":"credential","name":"username"}}]}`), InjectionRules: json.RawMessage(`[{"target":"header","name":"Authorization","template":"Bearer {{token}}"}]`)}
	saved, err := db.SaveAuthTemplate(ctx, original)
	if err != nil {
		t.Fatal(err)
	}
	var request map[string]any
	if err := json.Unmarshal(saved.TokenRequest, &request); err != nil || request["method"] != "GET" {
		t.Fatalf("request lost: %v", err)
	}
	stale := saved
	saved.Name = "Updated"
	updated, err := db.SaveAuthTemplate(ctx, saved)
	if err != nil || updated.Version != saved.Version+1 {
		t.Fatalf("update: %v %+v", err, updated)
	}
	stale.Name = "Stale"
	if _, err := db.SaveAuthTemplate(ctx, stale); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale accepted: %v", err)
	}
	loaded, err := db.AuthTemplate(ctx, updated.ID)
	if err != nil || loaded.Name != "Updated" {
		t.Fatalf("stale modified template: %v", err)
	}
}
