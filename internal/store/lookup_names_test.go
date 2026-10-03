package store_test

import (
	"context"
	"encoding/json"
	"testing"

	"apihub-go/internal/testutil"
)

func TestCatalogLookupsIncludeConnectionDisplayName(t *testing.T) {
	db := testutil.Database(t)
	fixture := testutil.Seed(t, db)
	lookups, err := db.CatalogLookups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var connections []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(lookups["connections"], &connections); err != nil {
		t.Fatal(err)
	}
	for _, connection := range connections {
		if connection.ID == fixture.Connection.ID {
			if connection.Name != fixture.Connection.Name {
				t.Fatalf("expected account name %q, got %q", fixture.Connection.Name, connection.Name)
			}
			return
		}
	}
	t.Fatal("fixture connection missing from lookups")
}
