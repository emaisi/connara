package background

import (
	"apihub-go/internal/authn"
	"apihub-go/internal/executor"
	"apihub-go/internal/secret"
	"apihub-go/internal/testutil"
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSyncRecordIdentityAndResponsePaths(t *testing.T) {
	records, err := syncRecords("task", []byte(`[{"id":9007199254740992},{"id":9007199254740993}]`))
	if err != nil || len(records) != 2 || records[0].ExternalID == records[1].ExternalID {
		t.Fatalf("lost integer ID: %v %v", records, err)
	}
	for _, raw := range []string{`[{}]`, `[{"id":null}]`, `["text"]`, `[{"id":1},{"id":1}]`} {
		if _, err := syncRecords("task", []byte(raw)); err == nil {
			t.Fatalf("accepted unstable identity %s", raw)
		}
	}
	config, _ := ParseSyncConfig([]byte(`{"recordsPath":"$.response.items","idPath":"key","cursorPath":"$.response.next","cursorParam":"after"}`))
	records, next, err := extractSyncPage("task", []byte(`{"response":{"items":[{"key":"x"}],"next":"abc"}}`), config)
	if err != nil || len(records) != 1 || next != "abc" {
		t.Fatalf("nested page: %v %s %v", records, next, err)
	}
}
func TestSyncResumeSkipsPersistedPage(t *testing.T) {
	db := testutil.Database(t)
	f := testutil.Seed(t, db)
	ctx := context.Background()
	codec, _ := secret.New(bytes.Repeat([]byte{1}, 32))
	count := 0
	fail := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if r.URL.Query().Get("after") == "second" {
			if fail {
				w.WriteHeader(503)
				return
			}
			w.Write([]byte(`{"data":[{"id":9007199254740993}],"next":null}`))
			return
		}
		w.Write([]byte(`{"data":[{"id":9007199254740992}],"next":"second"}`))
	}))
	defer server.Close()
	auth := authn.New(db, codec, nil, server.Client())
	blob, _ := auth.SealCredentials(f.Connection.ID, 1, map[string]any{"apiKey": "test", "token": "test"})
	testutil.Exec(t, db, `UPDATE connections SET credential_blob=$2 WHERE id=$1`, f.Connection.ID, blob)
	testutil.Exec(t, db, `UPDATE integrations SET base_url=$2 WHERE id=$1`, f.Integration.ID, server.URL)
	testutil.Exec(t, db, `UPDATE sync_tasks SET sync_config='{"cursorPath":"next","cursorParam":"after"}' WHERE id=$1`, f.Task.ID)
	if _, err := db.EnqueueSync(ctx, f.Task, "test", testutil.ID("request")); err != nil {
		t.Fatal(err)
	}
	jobs, err := db.ClaimJobs(ctx, "worker", 1)
	if err != nil {
		t.Fatal(err)
	}
	service := New(db, auth, executor.New(server.Client()), codec, server.Client(), slog.New(slog.NewTextHandler(io.Discard, nil)), "test")
	if err := service.runSync(ctx, jobs[0]); err == nil {
		t.Fatal("expected second-page failure")
	}
	records, _ := db.ListSyncRecords(ctx, f.Task.ID, 100)
	if len(records) != 1 {
		t.Fatal("first page not committed")
	}
	fail = false
	if err := service.runSync(ctx, jobs[0]); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("persisted first page repeated: %d calls", count)
	}
	if err := service.runSync(ctx, jobs[0]); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatal("completed operation repeated")
	}
	records, _ = db.ListSyncRecords(ctx, f.Task.ID, 100)
	if len(records) != 2 {
		t.Fatal("second page missing")
	}
}
