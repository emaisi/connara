package store_test

import (
	"apihub-go/internal/model"
	"apihub-go/internal/store"
	"apihub-go/internal/testutil"
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestSystemAuthTemplatesKeepAnExplicitDefault(t *testing.T) {
	db := testutil.Database(t)
	ctx := context.Background()
	group, err := db.SaveSystemGroup(ctx, model.SystemGroup{Name: "Template test"})
	if err != nil {
		t.Fatal(err)
	}
	apiKey, err := db.AuthTemplate(ctx, "api_key")
	if err != nil {
		t.Fatal(err)
	}
	basic, err := db.AuthTemplate(ctx, "basic")
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{apiKey.ID, basic.ID}
	system, err := db.CreateSystem(ctx, model.System{
		SystemKey: "template-test", Name: "Template test", GroupID: group.ID, AuthTemplateIDs: ids,
	}, basic.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(system.AuthTemplateIDs) != 2 || system.DefaultAuthTemplateID != basic.ID {
		t.Fatalf("create lost supported/default templates: %+v", system)
	}
	lookups, err := db.CatalogLookups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var lookupRows []struct {
		SystemKey             string `json:"systemKey"`
		DefaultAuthTemplateID string `json:"defaultAuthTemplateId"`
	}
	if err := json.Unmarshal(lookups["systems"], &lookupRows); err != nil {
		t.Fatal(err)
	}
	if len(lookupRows) != 1 || lookupRows[0].SystemKey != system.SystemKey || lookupRows[0].DefaultAuthTemplateID != basic.ID {
		t.Fatalf("lookup lost default template: %+v", lookupRows)
	}
	if err := db.SetSystemAuthTemplates(ctx, system.ID, []string{apiKey.ID, basic.ID}, ""); err != nil {
		t.Fatal(err)
	}
	system, err = db.System(ctx, system.ID)
	if err != nil || system.DefaultAuthTemplateID != basic.ID {
		t.Fatalf("legacy update changed default: %+v, %v", system, err)
	}
	if err := db.SetSystemAuthTemplates(ctx, system.ID, ids, apiKey.ID); err != nil {
		t.Fatal(err)
	}
	system, err = db.System(ctx, system.ID)
	if err != nil || system.DefaultAuthTemplateID != apiKey.ID {
		t.Fatalf("explicit default was not saved: %+v, %v", system, err)
	}
	if err := db.SetSystemAuthTemplates(ctx, system.ID, ids, testutil.ID("other-template")); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("accepted a default outside supported templates: %v", err)
	}
	system, err = db.System(ctx, system.ID)
	if err != nil || system.DefaultAuthTemplateID != apiKey.ID {
		t.Fatalf("invalid update changed default: %+v, %v", system, err)
	}
}
