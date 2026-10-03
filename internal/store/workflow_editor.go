package store

import (
	"apihub-go/internal/model"
	"apihub-go/internal/workflow"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"time"
)

func (s *Store) ConfigureWorkflows(features workflow.Features, runner *workflow.CodeRunner) {
	s.workflowFeatures = features
	s.codeRunner = runner
}
func (s *Store) WorkflowFeatures() workflow.Features      { return s.workflowFeatures }
func (s *Store) WorkflowCodeRunner() *workflow.CodeRunner { return s.codeRunner }
func (s *Store) CheckWorkflowAdmission(item model.Workflow) error {
	return s.workflowFeatures.CheckNew(item.Graph)
}

func (s *Store) SaveWorkflowLayout(ctx context.Context, id string, expectedVersion, layoutVersion int64, raw json.RawMessage) (model.Workflow, error) {
	item, err := s.Workflow(ctx, id)
	if err != nil {
		return item, err
	}
	if expectedVersion <= 0 || layoutVersion <= 0 {
		return item, ErrConflict
	}
	def, err := workflow.ParseDefinition(item.Graph)
	if err != nil {
		return item, err
	}
	layout, err := workflow.NormalizeLayout(raw, def, false)
	if err != nil {
		return item, err
	}
	command, err := s.database(ctx).Exec(ctx, `UPDATE workflows SET editor_layout=$5,layout_version=layout_version+CASE WHEN editor_layout IS DISTINCT FROM $5::jsonb THEN 1 ELSE 0 END WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL AND version=$3 AND layout_version=$4`, s.workspaceID, id, expectedVersion, layoutVersion, layout)
	if err != nil {
		return item, err
	}
	if command.RowsAffected() != 1 {
		return item, ErrConflict
	}
	return s.Workflow(ctx, id)
}

// Recovery is a narrow maintenance operation. Call with one fixed resumeAt
// after stopping all schedulers; keyset pagination and row locks make it
// repeatable without reviving drafts/paused flows or changing accepted runs.
func (s *Store) RescheduleWorkflowPlans(ctx context.Context, resumeAt time.Time, codeOnly bool) (int, error) {
	if resumeAt.IsZero() {
		return 0, fmt.Errorf("resumeAt is required")
	}
	count := 0
	cursor := ""
	for {
		batch := 0
		changed := 0
		nextCursor := cursor
		err := s.withTx(ctx, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `SELECT id::text FROM workflows WHERE workspace_id=$1 AND id::text>$2 AND status='deployed' AND deleted_at IS NULL AND schedule_type IN ('cron','interval') AND next_run_at IS NOT NULL AND graph->>'schemaVersion'='2' ORDER BY id::text LIMIT 100`, s.workspaceID, cursor)
			if err != nil {
				return err
			}
			var ids []string
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return err
				}
				ids = append(ids, id)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			for _, id := range ids {
				batch++
				nextCursor = id
				item, err := scanWorkflow(tx.QueryRow(ctx, `SELECT id::text,workflow_key,name,description,status,graph,schedule_type,COALESCE(cron_expression,''),schedule_timezone,next_run_at,retry_policy,input,version,editor_layout,layout_version,created_at,updated_at FROM workflows WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, s.workspaceID, id))
				if err != nil {
					return err
				}
				if item.Status != "deployed" || item.NextRunAt == nil || item.NextRunAt.After(resumeAt) {
					continue
				}
				def, err := workflow.ParseDefinition(item.Graph)
				if err != nil {
					return err
				}
				if def.SchemaVersion != 2 {
					continue
				}
				hasCode := false
				for _, step := range def.Steps {
					hasCode = hasCode || step.Type == "code"
				}
				if codeOnly && !hasCode {
					continue
				}
				next, err := advanceWorkflowSchedule(item, *item.NextRunAt, resumeAt)
				if err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `UPDATE workflows SET next_run_at=$3 WHERE workspace_id=$1 AND id=$2`, s.workspaceID, id, next); err != nil {
					return err
				}
				changed++
			}
			return nil
		})
		if err != nil {
			return count, err
		}
		count += changed
		cursor = nextCursor
		if batch < 100 {
			return count, nil
		}
	}
}

func (s *Store) saveWorkflowDefinition(ctx context.Context, item model.Workflow, expectedVersion int64, expectedLayout []int64) (model.Workflow, error) {
	if len(item.RetryPolicy) == 0 || string(item.RetryPolicy) == "{}" {
		item.RetryPolicy = json.RawMessage(`{"maxAttempts":1}`)
	}
	def, err := workflow.ParseDefinition(item.Graph)
	if err != nil {
		return item, err
	}
	if err := workflow.ValidateDefinition(def, false, nil); err != nil {
		return item, err
	}
	if err := s.workflowFeatures.CheckEditingDefinition(def); err != nil {
		return item, err
	}
	if item.ID == "" {
		layout, err := workflow.NormalizeLayout(item.EditorLayout, def, false)
		if err != nil {
			return item, err
		}
		item.ID = randomID()
		item.Status = "draft"
		item.ScheduleTimezone = defaultText(item.ScheduleTimezone, "UTC")
		_, err = s.database(ctx).Exec(ctx, `INSERT INTO workflows(id,workspace_id,workflow_key,name,description,status,graph,schedule_type,cron_expression,schedule_timezone,retry_policy,input,editor_layout) VALUES($1,$2,$3,$4,$5,'draft',$6,$7,NULLIF($8,''),$9,$10,$11,$12)`, item.ID, s.workspaceID, item.WorkflowKey, item.Name, item.Description, jsonOrObject(item.Graph), item.ScheduleType, item.CronExpression, item.ScheduleTimezone, jsonOrObject(item.RetryPolicy), jsonOrObject(item.Input), layout)
		if err != nil {
			return item, err
		}
		return s.Workflow(ctx, item.ID)
	}
	current, err := s.Workflow(ctx, item.ID)
	if err != nil {
		return item, err
	}
	if expectedVersion <= 0 || current.Version != expectedVersion {
		return item, ErrConflict
	}
	old, err := workflow.ParseDefinition(current.Graph)
	if err != nil {
		return item, err
	}
	if old.SchemaVersion == 2 && def.SchemaVersion != 2 {
		return item, fmt.Errorf("%w: workflow schema downgrade is not supported", ErrConflict)
	}
	layout := current.EditorLayout
	layoutExpected := current.LayoutVersion
	if len(item.EditorLayout) > 0 {
		if (len(expectedLayout) > 0 && (len(expectedLayout) != 1 || expectedLayout[0] != layoutExpected)) || (len(expectedLayout) == 0 && !bytes.Equal(item.EditorLayout, current.EditorLayout)) {
			return item, ErrConflict
		}
		layout, err = workflow.NormalizeLayout(item.EditorLayout, def, false)
	} else {
		layout, err = workflow.NormalizeLayout(layout, def, true)
	}
	if err != nil {
		return item, err
	}
	var oldLayout, newLayout any
	_ = json.Unmarshal(current.EditorLayout, &oldLayout)
	_ = json.Unmarshal(layout, &newLayout)
	oldCanonical, _ := json.Marshal(oldLayout)
	newCanonical, _ := json.Marshal(newLayout)
	changed := !bytes.Equal(newCanonical, oldCanonical)
	command, err := s.database(ctx).Exec(ctx, `UPDATE workflows SET workflow_key=$3,name=$4,description=$5,graph=$6,schedule_type=$7,cron_expression=NULLIF($8,''),schedule_timezone=$9,retry_policy=$10,input=$11,status='draft',next_run_at=NULL,version=version+1,updated_at=now(),editor_layout=$13,layout_version=layout_version+CASE WHEN $14 THEN 1 ELSE 0 END WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL AND version=$12 AND layout_version=$15`, s.workspaceID, item.ID, item.WorkflowKey, item.Name, item.Description, jsonOrObject(item.Graph), item.ScheduleType, item.CronExpression, item.ScheduleTimezone, jsonOrObject(item.RetryPolicy), jsonOrObject(item.Input), expectedVersion, layout, changed, layoutExpected)
	if err != nil {
		return item, err
	}
	if command.RowsAffected() != 1 {
		return item, ErrConflict
	}
	return s.Workflow(ctx, item.ID)
}
