package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"apihub-go/internal/model"
	"apihub-go/internal/store"
	"apihub-go/internal/testutil"
	"apihub-go/internal/workflow"
)

func TestWorkflowLayoutCASAndFrozenV2Snapshot(t *testing.T) {
	db := testutil.Database(t)
	ctx := context.Background()
	f := testutil.Seed(t, db)
	db.ConfigureWorkflows(workflow.Features{V2Enabled: true}, nil)
	draft := draftWorkflow(f)
	var graph map[string]any
	_ = json.Unmarshal(draft.Graph, &graph)
	graph["schemaVersion"] = 2
	for _, row := range graph["steps"].([]any) {
		row.(map[string]any)["type"] = "api"
	}
	graph["variables"] = []any{map[string]any{"name": "date", "type": "string", "initial": map[string]any{"$value": map[string]any{"kind": "run", "field": "businessDate"}}}}
	draft.Graph, _ = json.Marshal(graph)
	draft.ScheduleTimezone = "Asia/Singapore"
	created, err := db.SaveWorkflow(ctx, draft, 0)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := db.ResolveWorkflowBindings(ctx, created)
	if err != nil {
		t.Fatal(err)
	}
	next := time.Now().UTC().Add(time.Hour)
	deployed, err := db.DeployWorkflow(ctx, created.ID, created.Version, resolved.Graph, &next)
	if err != nil {
		t.Fatal(err)
	}
	layout := json.RawMessage(`{"schemaVersion":1,"direction":"LR","positions":{"s1":{"x":123,"y":456}}}`)
	moved, err := db.SaveWorkflowLayout(ctx, deployed.ID, deployed.Version, deployed.LayoutVersion, layout)
	if err != nil {
		t.Fatal(err)
	}
	if moved.Version != deployed.Version || moved.Status != "deployed" || !moved.NextRunAt.Equal(*deployed.NextRunAt) || moved.LayoutVersion != deployed.LayoutVersion+1 {
		t.Fatalf("layout changed business state: %+v", moved)
	}
	if _, err := db.SaveWorkflowLayout(ctx, deployed.ID, deployed.Version, deployed.LayoutVersion, layout); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale layout accepted: %v", err)
	}
	deployed.EditorLayout = layout
	if _, err := db.SaveWorkflow(ctx, deployed, deployed.Version, deployed.LayoutVersion); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale combined layout accepted: %v", err)
	}
	codec := workflowCodec(t)
	planned := time.Date(2026, 10, 3, 15, 55, 0, 0, time.UTC)
	op, err := db.EnqueueWorkflow(ctx, store.WorkflowTriggerInput{Workflow: moved, Source: "manual", RequestID: testutil.ID("v2"), Codec: codec, ScheduledFor: &planned})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := db.WorkflowRunSnapshot(ctx, codec, op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.RunMetadata.BusinessDate != "2026-10-03" || snapshot.InitialVariables["date"] != "2026-10-03" || snapshot.RunMetadata.TriggeredAt != op.StartedAt || snapshot.WorkflowName != moved.Name || len(snapshot.EditorLayout) == 0 {
		t.Fatalf("snapshot not pinned: %+v", snapshot)
	}
	moved.Name = "later name"
	moved.EditorLayout = nil
	updated, err := db.SaveWorkflow(ctx, moved, moved.Version)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LayoutVersion != moved.LayoutVersion {
		t.Fatal("unchanged layout counter incremented")
	}
	db.ConfigureWorkflows(workflow.Features{}, nil)
	frozen, err := db.WorkflowRunSnapshot(ctx, codec, op.ID)
	if err != nil || frozen.WorkflowName == updated.Name || frozen.RunMetadata.BusinessDate != "2026-10-03" {
		t.Fatalf("accepted snapshot changed or disabled: %+v %v", frozen, err)
	}
	updated.Graph = draftWorkflow(f).Graph
	if _, err := db.SaveWorkflow(ctx, updated, updated.Version); err == nil {
		t.Fatal("v2 downgrade accepted")
	}
}

func TestDisabledWorkflowSchedulesDoNotStarveAndRecoverySkipsCatchup(t *testing.T) {
	db := testutil.Database(t)
	ctx := context.Background()
	f := testutil.Seed(t, db)
	db.ConfigureWorkflows(workflow.Features{V2Enabled: true}, nil)
	now := time.Now().UTC().Truncate(time.Second)
	var disabled []model.Workflow
	for i := 0; i < 7; i++ {
		draft := draftWorkflow(f)
		draft.WorkflowKey = fmt.Sprintf("disabled-%d", i)
		draft.ScheduleType = "interval"
		draft.CronExpression = "每 15 分钟"
		var graph map[string]any
		_ = json.Unmarshal(draft.Graph, &graph)
		graph["schemaVersion"] = 2
		for _, row := range graph["steps"].([]any) {
			row.(map[string]any)["type"] = "api"
		}
		draft.Graph, _ = json.Marshal(graph)
		item, err := db.SaveWorkflow(ctx, draft, 0)
		if err != nil {
			t.Fatal(err)
		}
		resolved, err := db.ResolveWorkflowBindings(ctx, item)
		if err != nil {
			t.Fatal(err)
		}
		due := now.Add(time.Duration(-10+i) * time.Hour)
		item, err = db.DeployWorkflow(ctx, item.ID, item.Version, resolved.Graph, &due)
		if err != nil {
			t.Fatal(err)
		}
		disabled = append(disabled, item)
	}
	legacy := draftWorkflow(f)
	legacy.WorkflowKey = "enabled-legacy"
	legacy.ScheduleType = "interval"
	legacy.CronExpression = "每 15 分钟"
	item, err := db.SaveWorkflow(ctx, legacy, 0)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := db.ResolveWorkflowBindings(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	due := now.Add(-time.Minute)
	_, err = db.DeployWorkflow(ctx, item.ID, item.Version, resolved.Graph, &due)
	if err != nil {
		t.Fatal(err)
	}
	db.ConfigureWorkflows(workflow.Features{}, nil)
	n, skipped, err := db.EnqueueDueWorkflows(ctx, 2, workflowCodec(t))
	if err != nil || n != 1 || len(skipped) != len(disabled) {
		t.Fatalf("disabled rows starved later legacy: %d %v %v", n, skipped, err)
	}
	for _, old := range disabled {
		current, err := db.Workflow(ctx, old.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !current.NextRunAt.After(now) || current.Version != old.Version || current.Status != "deployed" {
			t.Fatal("disabled schedule did not advance cleanly")
		}
		var count int
		if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM operation_runs WHERE workflow_id=$1`, old.ID).Scan(&count); err != nil || count != 0 {
			t.Fatal("disabled schedule created operation", count, err)
		}
	}
	// Recovery fixes only due, deployed v2 schedules; leaves future and paused rows intact.
	cutoff := now.Add(20 * time.Minute)
	stale := cutoff.Add(-time.Hour)
	future := cutoff.Add(time.Hour)
	testutil.Exec(t, db, `UPDATE workflows SET next_run_at=$2 WHERE id=$1`, disabled[0].ID, stale)
	testutil.Exec(t, db, `UPDATE workflows SET next_run_at=$2 WHERE id=$1`, disabled[1].ID, future)
	testutil.Exec(t, db, `UPDATE workflows SET status='paused',next_run_at=$2 WHERE id=$1`, disabled[2].ID, stale)
	count, err := db.RescheduleWorkflowPlans(ctx, cutoff, false)
	if err != nil || count != 5 {
		t.Fatalf("recovery: %d %v", count, err)
	}
	count, err = db.RescheduleWorkflowPlans(ctx, cutoff, false)
	if err != nil || count != 0 {
		t.Fatalf("recovery not idempotent: %d %v", count, err)
	}
	preserved, _ := db.Workflow(ctx, disabled[1].ID)
	if !preserved.NextRunAt.Equal(future) {
		t.Fatal("recovery changed future cursor")
	}
	paused, _ := db.Workflow(ctx, disabled[2].ID)
	if !paused.NextRunAt.Equal(stale) {
		t.Fatal("recovery changed paused flow")
	}
}

func TestCodeDisabledStillAllowsV2DraftEditing(t *testing.T) {
	db := testutil.Database(t)
	testutil.Seed(t, db)
	db.ConfigureWorkflows(workflow.Features{V2Enabled: true}, nil)
	item, err := db.SaveWorkflow(context.Background(), model.Workflow{WorkflowKey: "code-draft", Name: "code draft", ScheduleType: "manual", Graph: json.RawMessage(`{"schemaVersion":2,"steps":[{"id":"code1","type":"code","language":"javascript","runtimeProfile":"js-v1","code":"function main( unfinished","inputSchema":{"type":"object","properties":{},"required":[]},"outputSchema":{"type":"object","properties":{},"required":[]},"input":{}}],"output":{}}`)}, 0)
	if err != nil || item.Status != "draft" {
		t.Fatal("code-disabled draft was rejected or compiled", item, err)
	}
	if !errors.Is(db.CheckWorkflowAdmission(item), workflow.ErrFeatureDisabled) {
		t.Fatal("code-disabled draft was executable")
	}
}
