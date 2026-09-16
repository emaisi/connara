package store_test

import (
	"apihub-go/internal/store"
	"apihub-go/internal/testutil"
	"context"
	"errors"
	"testing"
	"time"
)

func TestInvitationExpiresRevokesAndConsumesOnce(t *testing.T) {
	db := testutil.Database(t)
	ctx := context.Background()
	email := testutil.ID("invite") + "@example.test"
	members, err := db.ListTeamMembers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	owner := members[0].UserID
	if _, err = db.InviteMember(ctx, email, "viewer", owner, "old-hash", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.InviteMember(ctx, email, "developer", owner, "new-hash", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.AcceptInvitation(ctx, "old-hash", "password-hash", "Name"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("old invitation remained valid: %v", err)
	}
	member, err := db.AcceptInvitation(ctx, "new-hash", "password-hash", "Name")
	if err != nil {
		t.Fatal(err)
	}
	if member.Status != "active" || member.Role != "developer" || member.DisplayName != "Name" {
		t.Fatalf("incorrect activation: %+v", member)
	}
	if _, err = db.AcceptInvitation(ctx, "new-hash", "replacement-password", "Name"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("token reused: %v", err)
	}
	if _, err = db.InviteMember(ctx, email, "admin", owner, "reset-hash", time.Now().Add(time.Hour)); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("active account could be reset: %v", err)
	}
	expired := testutil.ID("expired") + "@example.test"
	if _, err = db.InviteMember(ctx, expired, "viewer", owner, "expired-hash", time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.AcceptInvitation(ctx, "expired-hash", "password", "Name"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expired invitation accepted: %v", err)
	}
	other := testutil.Database(t)
	if _, err = other.AcceptInvitation(ctx, "new-hash", "password", "Name"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross workspace activation: %v", err)
	}
}

func TestUIListsUseServerSearchFiltersAndCursors(t *testing.T) {
	db := testutil.Database(t)
	f := testutil.Seed(t, db)
	ctx := context.Background()
	if _, err := db.CatalogLookups(ctx); err != nil {
		t.Fatal(err)
	}
	actions, err := db.ListActions(ctx, "/records", "", store.ListOptions{Limit: 1})
	if err != nil || len(actions) != 1 {
		t.Fatalf("path search: %v %v", actions, err)
	}
	systems, err := db.ListSystems(ctx, store.ListOptions{Limit: 1, Query: f.Auth.Name, Scope: "connected"})
	if err != nil || len(systems) != 1 {
		t.Fatalf("auth search: %v %v", systems, err)
	}
	if _, err = db.ListOperations(ctx, "", "", "", 1, store.ListOptions{Limit: 1, IntegrationID: f.Integration.ID, ConnectionID: f.Connection.ID, SyncTaskID: f.Task.ID, From: time.Now().Add(-time.Hour), To: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ListAudit(ctx, "", 1, store.ListOptions{Limit: 1, From: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ListWebhookDeliveries(ctx, 1, store.ListOptions{Limit: 1, Query: "event", Status: "retrying"}); err != nil {
		t.Fatal(err)
	}
	for index, external := range []string{"a", "b", "c"} {
		testutil.Exec(t, db, `INSERT INTO sync_records(id,workspace_id,sync_task_id,model,external_id,payload,payload_hash,first_seen_at) VALUES($1,$2,$3,'record',$4,'{}','hash',$5)`, testutil.ID(external), db.WorkspaceID(), f.Task.ID, external, time.Now().Add(time.Duration(index)*time.Second))
	}
	first, err := db.ListSyncRecords(ctx, f.Task.ID, 2, store.ListOptions{Limit: 2})
	if err != nil || len(first) != 2 {
		t.Fatalf("records page: %v %v", first, err)
	}
	last := first[1]
	second, err := db.ListSyncRecords(ctx, f.Task.ID, 2, store.ListOptions{Limit: 2, Before: last.FirstSeenAt, ID: last.ID})
	if err != nil || len(second) != 1 || second[0].ID == first[0].ID || second[0].ID == first[1].ID {
		t.Fatalf("records cursor: %v %v", second, err)
	}
}
