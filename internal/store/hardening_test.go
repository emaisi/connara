package store_test

import (
	"apihub-go/internal/model"
	"apihub-go/internal/secret"
	"apihub-go/internal/store"
	"apihub-go/internal/testutil"
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestSchedulerAndSyncCommitAreAtomicAndFenced(t *testing.T) {
	db := testutil.Database(t)
	f := testutil.Seed(t, db)
	ctx := context.Background()
	// Deliberately collide with the scheduled operation ID: transaction must roll back next_run_at.
	req := "schedule-" + f.Task.ID + "-" + f.Task.NextRunAt.UTC().Format(time.RFC3339Nano)
	run := model.OperationRun{ID: store.StableID("operation", req), RequestID: req, Kind: "sync", Name: "collision", Status: "queued", Source: "test", StartedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	if err := db.CreateOperation(ctx, run); err != nil {
		t.Fatal(err)
	}
	if _, err := db.EnqueueDueSyncTasks(ctx, 20); err == nil {
		t.Fatal("expected duplicate operation rollback")
	}
	unchanged, _ := db.SyncTask(ctx, f.Task.ID)
	if !unchanged.NextRunAt.Equal(*f.Task.NextRunAt) {
		t.Fatal("scheduler advanced without enqueue")
	}
	testutil.Exec(t, db, `DELETE FROM operation_runs WHERE workspace_id=$1`, db.WorkspaceID())
	var wg sync.WaitGroup
	counts := make(chan int, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); n, err := db.EnqueueDueSyncTasks(ctx, 20); counts <- n; errs <- err }()
	}
	wg.Wait()
	close(counts)
	close(errs)
	total := 0
	for n := range counts {
		total += n
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if total != 1 {
		t.Fatalf("enqueued %d runs", total)
	}
	if _, err := db.EnqueueSync(ctx, f.Task, "manual", testutil.ID("request")); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("overlapping sync allowed: %v", err)
	}
	jobs, err := db.ClaimJobs(ctx, "worker-a", 1)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("claim: %v %v", jobs, err)
	}
	old := jobs[0]
	testutil.Exec(t, db, `UPDATE jobs SET lease_expires_at=now()-interval '1 second' WHERE id=$1`, old.ID)
	jobs, err = db.ClaimJobs(ctx, "worker-b", 1)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("reclaim: %v %v", jobs, err)
	}
	current := jobs[0]
	records := []model.SyncRecord{{Model: "record", ExternalID: "9007199254740992", Payload: []byte(`{"id":9007199254740992}`)}, {Model: "record", ExternalID: "9007199254740993", Payload: []byte(`{"id":9007199254740993}`)}}
	if err := db.CommitSyncPage(ctx, old, f.Task, []byte(`{"done":true}`), records, true, 2); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale worker committed: %v", err)
	}
	if err := db.RenewJob(ctx, old); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale worker renewed: %v", err)
	}
	if err := db.RenewJob(ctx, current); err != nil {
		t.Fatal(err)
	}
	if err := db.CommitSyncPage(ctx, current, f.Task, []byte(`{"done":true}`), records, true, 2); err != nil {
		t.Fatal(err)
	}
	saved, err := db.ListSyncRecords(ctx, f.Task.ID, 100)
	if err != nil || len(saved) != 2 {
		t.Fatalf("large ID collision: %v %v", saved, err)
	}
	var count int
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE workspace_id=$1`, db.WorkspaceID()).Scan(&count); err != nil || count != 1 {
		t.Fatalf("outbox missing: %d %v", count, err)
	}
	if err := db.CompleteJob(ctx, old, "worker-a"); !errors.Is(err, store.ErrConflict) {
		t.Fatal("old attempt completed job")
	}
	if err := db.CompleteJob(ctx, current, "worker-b"); err != nil {
		t.Fatal(err)
	}
}
func TestSyncInvalidRecordRollsBackCheckpoint(t *testing.T) {
	db := testutil.Database(t)
	f := testutil.Seed(t, db)
	ctx := context.Background()
	if _, err := db.EnqueueSync(ctx, f.Task, "test", testutil.ID("request")); err != nil {
		t.Fatal(err)
	}
	jobs, err := db.ClaimJobs(ctx, "test", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CommitSyncPage(ctx, jobs[0], f.Task, []byte(`{"cursor":"next"}`), []model.SyncRecord{{Model: "record", Payload: []byte(`{}`)}}, true, 1); err == nil {
		t.Fatal("missing identity accepted")
	}
	task, _ := db.SyncTask(ctx, f.Task.ID)
	if string(task.Checkpoint) != "{}" {
		t.Fatalf("checkpoint advanced on rollback: %s", task.Checkpoint)
	}
}
func TestAuthBindingCASAndRotation(t *testing.T) {
	db := testutil.Database(t)
	f := testutil.Seed(t, db)
	ctx := context.Background()
	changed := f.Integration
	changed.BaseURL = "https://other.example.test"
	if _, err := db.SaveIntegration(ctx, changed); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("rebound existing credentials: %v", err)
	}
	f.Auth.Name = "updated"
	updated, err := db.SaveAuthInstance(ctx, f.Auth)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SaveAuthInstance(ctx, f.Auth); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale auth update accepted: %v", err)
	}
	key1 := bytes.Repeat([]byte{1}, 32)
	key2 := bytes.Repeat([]byte{2}, 32)
	old, _ := secret.New(key1)
	ring, _ := secret.NewKeyring(map[int][]byte{1: key1, 2: key2}, 2)
	aad := []byte(fmt.Sprintf("%s:connection:%s:%d", db.WorkspaceID(), f.Connection.ID, f.Connection.Revision))
	blob, _ := old.Encrypt([]byte(`{"token":"original"}`), aad)
	testutil.Exec(t, db, `UPDATE connections SET credential_blob=$2 WHERE id=$1`, f.Connection.ID, blob)
	// Also exercise named secret AAD.
	authBlob, _ := old.Encrypt([]byte(`{"clientId":"id"}`), []byte(db.WorkspaceID()+":auth-instance:"+updated.ID))
	testutil.Exec(t, db, `UPDATE auth_instances SET secret_blob=$2 WHERE id=$1`, updated.ID, authBlob)
	n, err := db.RotateSecrets(ctx, ring)
	if err != nil || n != 2 {
		t.Fatalf("rotate: %d %v", n, err)
	}
	connection, _ := db.Connection(ctx, f.Connection.ID)
	plain, err := ring.Decrypt(connection.CredentialBlob, aad)
	if err != nil || string(plain) != `{"token":"original"}` || connection.Revision != f.Connection.Revision || connection.KeyVersion != 2 {
		t.Fatalf("rotation changed credential: %s %v", plain, err)
	}
	if n, err := db.RotateSecrets(ctx, ring); err != nil || n != 0 {
		t.Fatalf("rotation not resumable: %d %v", n, err)
	}
}
func TestIdempotencyCleanupPreservesUnknownAndActiveJobs(t *testing.T) {
	db := testutil.Database(t)
	ctx := context.Background()
	f := testutil.Seed(t, db)
	record := model.IdempotencyRecord{ID: testutil.ID("idem"), RuntimeTokenID: testutil.ID("token"), Scope: "test", Key: "key", Fingerprint: "fingerprint", ExpiresAt: time.Now().Add(-time.Hour)}
	if _, created, err := db.ClaimIdempotency(ctx, record); err != nil || !created {
		t.Fatal(err)
	}
	run := model.OperationRun{ID: testutil.ID("op"), RequestID: testutil.ID("request"), Name: "test", Kind: "action", Status: "running", Source: "test", StartedAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(-time.Hour)}
	if err := db.CreateOperation(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := db.StartIdempotency(ctx, record.ID, run.ID); err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, db, `UPDATE idempotency_records SET created_at=now()-interval '1 hour' WHERE id=$1`, record.ID)
	if _, err := db.EnqueueSync(ctx, f.Task, "test", testutil.ID("request")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CleanupExpired(ctx); err != nil {
		t.Fatal(err)
	}
	existing, created, err := db.ClaimIdempotency(ctx, record)
	if err != nil || created || existing.Status != "unknown" {
		t.Fatalf("unsafe replay: %v %v %v", existing, created, err)
	}
	if err := db.ReleaseIdempotency(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	jobs, err := db.ClaimJobs(ctx, "worker", 1)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("active job deleted: %v %v", jobs, err)
	}
}
func TestPagedHistoryAndBootstrapPreserveEdits(t *testing.T) {
	db := testutil.Database(t)
	ctx := context.Background()
	f := testutil.Seed(t, db)
	for i := range 105 {
		run := model.OperationRun{ID: testutil.ID("op"), RequestID: testutil.ID("request"), Kind: "action", Name: fmt.Sprintf("event-%d", i), Status: "success", Source: "test", StartedAt: time.Now().Add(time.Duration(i) * time.Second), ExpiresAt: time.Now().Add(time.Hour)}
		if err := db.CreateOperation(ctx, run); err != nil {
			t.Fatal(err)
		}
	}
	first, err := db.ListOperations(ctx, "", "", "", 100, store.ListOptions{Limit: 100})
	if err != nil || len(first) != 100 {
		t.Fatalf("first page: %d %v", len(first), err)
	}
	last := first[len(first)-1]
	second, err := db.ListOperations(ctx, "", "", "", 100, store.ListOptions{Limit: 100, Before: last.StartedAt, ID: last.ID})
	if err != nil || len(second) != 5 {
		t.Fatalf("second page: %d %v", len(second), err)
	}
	matches, err := db.ListOperations(ctx, "", "", "event-0", 100)
	if err != nil || len(matches) != 1 {
		t.Fatalf("historical search: %d %v", len(matches), err)
	}
	if _, err := db.ListSystems(ctx, store.ListOptions{Limit: 1, Query: "test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ListConnections(ctx, store.ListOptions{Limit: 1, Query: "test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ListActions(ctx, "test", "", store.ListOptions{Limit: 1}); err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, db, `UPDATE actions SET description='preserved' WHERE id=$1`, f.Action.ID)
	if err := db.Bootstrap(ctx, store.BootstrapInput{WorkspaceID: db.WorkspaceID()}); err != nil {
		t.Fatal(err)
	}
	action, _ := db.Action(ctx, f.Action.ID)
	if action.Description != "preserved" {
		t.Fatal("bootstrap overwrote action")
	}
}
