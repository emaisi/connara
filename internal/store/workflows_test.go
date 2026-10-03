package store_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"apihub-go/internal/model"
	"apihub-go/internal/secret"
	"apihub-go/internal/store"
	"apihub-go/internal/testutil"
	"apihub-go/internal/workflow"
)

func workflowCodec(t *testing.T) *secret.Codec {
	t.Helper()
	codec, err := secret.New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return codec
}

func draftWorkflow(f testutil.Fixture) model.Workflow {
	return model.Workflow{
		WorkflowKey:  "order-flow",
		Name:         "order flow",
		Graph:        []byte(`{"steps":[{"id":"s1","action":"` + f.Action.ActionKey + `","integrationId":"` + f.Integration.ID + `","connectionKey":"` + f.Connection.ID + `","input":{"id":"{{trigger.id}}"},"dependsOn":[]},{"id":"s2","action":"` + f.Action.ActionKey + `","integrationId":"` + f.Integration.IntegrationKey + `","connectionKey":"` + f.Connection.ConnectionKey + `","input":{"name":"{{s1.data.name}}"},"dependsOn":["s1"]}],"output":{"name":"{{s1.data.name}}"}}`),
		ScheduleType: "manual",
		Input:        []byte(`{"id":"C-1"}`),
	}
}

func TestWorkflowCRUDVersioningAndSoftDelete(t *testing.T) {
	db := testutil.Database(t)
	ctx := context.Background()
	f := testutil.Seed(t, db)

	created, err := db.SaveWorkflow(ctx, draftWorkflow(f), 0)
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != "draft" || created.ScheduleTimezone != "UTC" || created.Version != 1 {
		t.Fatalf("unexpected draft: %+v", created)
	}
	// Optimistic lock: stale version must conflict.
	edited := created
	edited.Name = "order flow v2"
	if _, err := db.SaveWorkflow(ctx, edited, created.Version-1); err == nil {
		t.Fatal("stale version accepted")
	}
	updated, err := db.SaveWorkflow(ctx, edited, created.Version)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != created.Version+1 {
		t.Fatalf("version must increment: %d", updated.Version)
	}
	// Duplicate key conflicts while the original is alive.
	duplicate := draftWorkflow(f)
	if _, err := db.SaveWorkflow(ctx, duplicate, 0); err == nil {
		t.Fatal("duplicate workflow_key accepted")
	}
	// Soft delete frees the key for a new UUID; history keeps pointing at the
	// old row.
	if err := db.DeleteWorkflow(ctx, updated.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Workflow(ctx, updated.ID); err != store.ErrNotFound {
		t.Fatalf("soft-deleted workflow must be hidden: %v", err)
	}
	second, err := db.SaveWorkflow(ctx, duplicate, 0)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == updated.ID {
		t.Fatal("recreated workflow must use a new UUID")
	}
}

func TestWorkflowDeployResolvesBindingsAndPinsGraph(t *testing.T) {
	db := testutil.Database(t)
	ctx := context.Background()
	f := testutil.Seed(t, db)
	codec := workflowCodec(t)

	item, err := db.SaveWorkflow(ctx, draftWorkflow(f), 0)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := db.ResolveWorkflowBindings(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Graph == nil {
		t.Fatal("resolved graph missing")
	}
	next := time.Now().UTC().Add(time.Hour)
	deployed, err := db.DeployWorkflow(ctx, resolved.ID, resolved.Version, resolved.Graph, &next)
	if err != nil {
		t.Fatal(err)
	}
	if deployed.Status != "deployed" || deployed.NextRunAt == nil {
		t.Fatalf("unexpected deploy: %+v", deployed)
	}
	// Enqueue builds a pinned snapshot with canonical IDs.
	operation, err := db.EnqueueWorkflow(ctx, store.WorkflowTriggerInput{
		Workflow: deployed, Trigger: map[string]any{"id": "C-2"},
		Principal: workflow.Principal{},
		Source:    "manual", RequestID: testutil.ID("request"),
		Codec: codec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if operation.Kind != "workflow" || operation.Status != "queued" || operation.WorkflowID != deployed.ID {
		t.Fatalf("unexpected operation: %+v", operation)
	}
	if len(operation.Input) == 0 {
		t.Fatal("operation must keep a sanitized trigger preview")
	}
	snapshot, err := db.WorkflowRunSnapshot(ctx, codec, operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Steps) != 2 || snapshot.WorkflowVersion != deployed.Version {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	for _, step := range snapshot.Steps {
		if step.IntegrationID != f.Integration.ID || step.ConnectionID != f.Connection.ID {
			t.Fatalf("snapshot must pin canonical IDs: %+v", step)
		}
	}
	// Editing the definition returns to draft and blocks enqueue.
	edited := deployed
	edited.Name = "changed"
	if _, err := db.SaveWorkflow(ctx, edited, deployed.Version); err != nil {
		t.Fatal(err)
	}
	reloaded, err := db.Workflow(ctx, deployed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Status != "draft" || reloaded.NextRunAt != nil {
		t.Fatalf("editing a deployed workflow must return to draft: %+v", reloaded)
	}
	if _, err := db.EnqueueWorkflow(ctx, store.WorkflowTriggerInput{Workflow: reloaded, Principal: workflow.Principal{}, Source: "manual", RequestID: testutil.ID("request2"), Codec: codec}); err == nil {
		t.Fatal("enqueue of a draft workflow must fail")
	}
}

func TestWorkflowManualRunsAreIndependentAndScheduleDeduplicates(t *testing.T) {
	db := testutil.Database(t)
	ctx := context.Background()
	f := testutil.Seed(t, db)
	codec := workflowCodec(t)
	item, err := db.SaveWorkflow(ctx, draftWorkflow(f), 0)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := db.ResolveWorkflowBindings(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	deployed, err := db.DeployWorkflow(ctx, resolved.ID, resolved.Version, resolved.Graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Two manual triggers with different inputs queue two runs.
	first, err := db.EnqueueWorkflow(ctx, store.WorkflowTriggerInput{Workflow: deployed, Trigger: map[string]any{"id": "a"}, Principal: workflow.Principal{}, Source: "manual", RequestID: testutil.ID("r1"), Codec: codec})
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.EnqueueWorkflow(ctx, store.WorkflowTriggerInput{Workflow: deployed, Trigger: map[string]any{"id": "b"}, Principal: workflow.Principal{}, Source: "manual", RequestID: testutil.ID("r2"), Codec: codec})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatal("manual runs must stay independent")
	}
	// The same planned schedule time is deduplicated.
	planned := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second)
	testutil.Exec(t, db, `UPDATE workflows SET schedule_type='interval', cron_expression='每 15 分钟', next_run_at=$2 WHERE id=$1`, deployed.ID, planned)
	enqueued, _, err := db.EnqueueDueWorkflows(ctx, 10, codec)
	if err != nil {
		t.Fatal(err)
	}
	if enqueued != 1 {
		t.Fatalf("expected one scheduled enqueue, got %d", enqueued)
	}
	testutil.Exec(t, db, `UPDATE workflows SET next_run_at=$2 WHERE id=$1`, deployed.ID, planned)
	enqueued, _, err = db.EnqueueDueWorkflows(ctx, 10, codec)
	if err != nil {
		t.Fatal(err)
	}
	if enqueued != 0 {
		t.Fatalf("duplicate planned time must not enqueue again, got %d", enqueued)
	}
	// The scheduler advanced next_run_at into the future.
	reloaded, err := db.Workflow(ctx, deployed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.NextRunAt == nil || !reloaded.NextRunAt.After(time.Now().UTC()) {
		t.Fatalf("next_run_at must advance past now: %+v", reloaded.NextRunAt)
	}
}

func TestWorkflowActiveLimitFailsClosed(t *testing.T) {
	db := testutil.Database(t)
	ctx := context.Background()
	f := testutil.Seed(t, db)
	codec := workflowCodec(t)
	item, err := db.SaveWorkflow(ctx, draftWorkflow(f), 0)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := db.ResolveWorkflowBindings(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	deployed, err := db.DeployWorkflow(ctx, resolved.ID, resolved.Version, resolved.Graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.EnqueueWorkflow(ctx, store.WorkflowTriggerInput{Workflow: deployed, Principal: workflow.Principal{}, Source: "manual", RequestID: testutil.ID("r1"), Codec: codec, MaxActive: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.EnqueueWorkflow(ctx, store.WorkflowTriggerInput{Workflow: deployed, Principal: workflow.Principal{}, Source: "manual", RequestID: testutil.ID("r2"), Codec: codec, MaxActive: 1})
	if err != store.ErrBusy {
		t.Fatalf("expected ErrBusy, got %v", err)
	}
}

func TestFinishWorkflowRunFencesLostLeases(t *testing.T) {
	db := testutil.Database(t)
	ctx := context.Background()
	f := testutil.Seed(t, db)
	codec := workflowCodec(t)
	item, err := db.SaveWorkflow(ctx, draftWorkflow(f), 0)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := db.ResolveWorkflowBindings(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	deployed, err := db.DeployWorkflow(ctx, resolved.ID, resolved.Version, resolved.Graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := db.EnqueueWorkflow(ctx, store.WorkflowTriggerInput{Workflow: deployed, Principal: workflow.Principal{}, Source: "manual", RequestID: testutil.ID("r"), Codec: codec})
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := db.ClaimJobsByKinds(ctx, "worker", 1, []string{"workflow_run"})
	if err != nil || len(jobs) != 1 {
		t.Fatalf("claim workflow job: %v %d", err, len(jobs))
	}
	job := jobs[0]
	if err := db.MarkWorkflowOperationRunning(ctx, operation.ID); err != nil {
		t.Fatal(err)
	}
	cipher, err := codec.Encrypt([]byte(`{"name":"Alice"}`), []byte(db.WorkspaceID()+":operation-run:"+operation.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishWorkflowRun(ctx, job, operation.ID, "success", 0, []byte(`{}`), cipher, "", "", model.IdempotencyRecord{}, nil); err != nil {
		t.Fatal(err)
	}
	// A worker that lost its lease must not overwrite the durable result.
	stale := job
	stale.Attempt = job.Attempt + 1
	if err := db.FinishWorkflowRun(ctx, stale, operation.ID, "success", 0, []byte(`{"hijack":true}`), nil, "", "", model.IdempotencyRecord{}, nil); err != store.ErrConflict {
		t.Fatalf("expected fence conflict, got %v", err)
	}
	loaded, err := db.Operation(ctx, operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != "success" || loaded.Output == nil || bytes.Contains(loaded.Output, []byte("hijack")) {
		t.Fatalf("durable result was tampered: %s", loaded.Output)
	}
	result, err := db.WorkflowRunResult(ctx, codec, operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != `{"name":"Alice"}` {
		t.Fatalf("unexpected decrypted result: %s", result)
	}
	// Completing the already-finalized job again must fail like the fence.
	if err := db.CompleteJob(ctx, job, "worker"); err == nil {
		t.Fatal("double completion must conflict")
	}
}
