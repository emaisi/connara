package store

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"apihub-go/internal/cronx"
	"apihub-go/internal/jsonutil"
	"apihub-go/internal/model"
	"apihub-go/internal/safejson"
	"apihub-go/internal/secret"
	"apihub-go/internal/workflow"
	"github.com/jackc/pgx/v5"
)

// ErrBusy marks a workspace at its concurrent workflow run limit.
var ErrBusy = errors.New("workflow busy")

// DefaultWorkflowActiveLimit caps queued+running workflow jobs per workspace.
const DefaultWorkflowActiveLimit = 50

func randomID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
}

func (s *Store) ListWorkflows(ctx context.Context) ([]model.Workflow, error) {
	rows, err := s.database(ctx).Query(ctx, `
		SELECT id::text, workflow_key, name, description, status, graph, schedule_type,
		       COALESCE(cron_expression, ''), schedule_timezone, next_run_at,
		       retry_policy, input, version, created_at, updated_at
		FROM workflows
		WHERE workspace_id = $1 AND deleted_at IS NULL
		ORDER BY updated_at DESC, id DESC`, s.workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.Workflow, 0)
	for rows.Next() {
		item, err := scanWorkflow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Workflow(ctx context.Context, idOrKey string) (model.Workflow, error) {
	item, err := scanWorkflow(s.database(ctx).QueryRow(ctx, `
		SELECT id::text, workflow_key, name, description, status, graph, schedule_type,
		       COALESCE(cron_expression, ''), schedule_timezone, next_run_at,
		       retry_policy, input, version, created_at, updated_at
		FROM workflows
		WHERE workspace_id = $1 AND deleted_at IS NULL
		  AND (id::text = $2 OR workflow_key = $2)`, s.workspaceID, idOrKey))
	return item, mapNotFound(err)
}

func scanWorkflow(row rowScanner) (model.Workflow, error) {
	var item model.Workflow
	err := row.Scan(
		&item.ID, &item.WorkflowKey, &item.Name, &item.Description, &item.Status,
		&item.Graph, &item.ScheduleType, &item.CronExpression, &item.ScheduleTimezone,
		&item.NextRunAt, &item.RetryPolicy, &item.Input,
		&item.Version, &item.CreatedAt, &item.UpdatedAt,
	)
	return item, err
}

// SaveWorkflow creates drafts (new UUID, UTC timezone) and updates existing
// definitions under an optimistic version lock. Editing a deployed workflow
// returns it to draft and clears next_run_at so it must be redeployed; the
// caller cannot set status directly.
func (s *Store) SaveWorkflow(ctx context.Context, item model.Workflow, expectedVersion int64) (model.Workflow, error) {
	if item.ID == "" {
		item.ID = randomID()
		item.Status = "draft"
		item.ScheduleTimezone = defaultText(item.ScheduleTimezone, "UTC")
		if _, err := s.database(ctx).Exec(ctx, `
			INSERT INTO workflows(
				id, workspace_id, workflow_key, name, description, status, graph,
				schedule_type, cron_expression, schedule_timezone, retry_policy, input
			) VALUES($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, ''), $10, $11, $12)`,
			item.ID, s.workspaceID, item.WorkflowKey, item.Name, item.Description,
			item.Status, jsonOrObject(item.Graph), item.ScheduleType, item.CronExpression,
			item.ScheduleTimezone, jsonOrObject(item.RetryPolicy), jsonOrObject(item.Input)); err != nil {
			return model.Workflow{}, err
		}
		return s.Workflow(ctx, item.ID)
	}
	command, err := s.database(ctx).Exec(ctx, `
		UPDATE workflows SET
			workflow_key = $3, name = $4, description = $5,
			graph = $6, schedule_type = $7, cron_expression = NULLIF($8, ''),
			schedule_timezone = $9, retry_policy = $10, input = $11,
			status = 'draft', next_run_at = NULL,
			version = version + 1, updated_at = now()
		WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL
		  AND version = $12`,
		s.workspaceID, item.ID, item.WorkflowKey, item.Name, item.Description,
		jsonOrObject(item.Graph), item.ScheduleType, item.CronExpression,
		item.ScheduleTimezone, jsonOrObject(item.RetryPolicy), jsonOrObject(item.Input), expectedVersion)
	if err != nil {
		return model.Workflow{}, err
	}
	if command.RowsAffected() == 0 {
		return model.Workflow{}, ErrConflict
	}
	return s.Workflow(ctx, item.ID)
}

// DeployWorkflow marks a workflow deployed with a normalized graph whose
// integration/connection references have been rewritten to canonical IDs.
func (s *Store) DeployWorkflow(ctx context.Context, id string, expectedVersion int64, graph json.RawMessage, nextRunAt *time.Time) (model.Workflow, error) {
	command, err := s.database(ctx).Exec(ctx, `
		UPDATE workflows SET status = 'deployed', graph = $3, next_run_at = $4,
		       version = version + 1, updated_at = now()
		WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL
		  AND status IN ('draft', 'paused', 'deployed') AND version = $5`,
		s.workspaceID, id, jsonOrObject(graph), nullTime(nextRunAt), expectedVersion)
	if err != nil {
		return model.Workflow{}, err
	}
	if command.RowsAffected() == 0 {
		if _, loadErr := s.Workflow(ctx, id); loadErr != nil {
			return model.Workflow{}, loadErr
		}
		return model.Workflow{}, ErrConflict
	}
	return s.Workflow(ctx, id)
}

// PauseWorkflow stops new scheduled triggers; accepted runs continue on their
// pinned snapshots.
func (s *Store) PauseWorkflow(ctx context.Context, id string, expectedVersion int64) (model.Workflow, error) {
	command, err := s.database(ctx).Exec(ctx, `
		UPDATE workflows SET status = 'paused', next_run_at = NULL,
		       version = version + 1, updated_at = now()
		WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL
		  AND status = 'deployed' AND version = $3`,
		s.workspaceID, id, expectedVersion)
	if err != nil {
		return model.Workflow{}, err
	}
	if command.RowsAffected() == 0 {
		if _, loadErr := s.Workflow(ctx, id); loadErr != nil {
			return model.Workflow{}, loadErr
		}
		return model.Workflow{}, ErrConflict
	}
	return s.Workflow(ctx, id)
}

func (s *Store) DeleteWorkflow(ctx context.Context, id string) error {
	command, err := s.database(ctx).Exec(ctx, `
		UPDATE workflows SET status = 'disabled', deleted_at = now(), next_run_at = NULL, updated_at = now()
		WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL`, s.workspaceID, id)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func defaultText(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// WorkflowTriggerInput pins everything a run needs at trigger time.
type WorkflowTriggerInput struct {
	Workflow      model.Workflow
	Trigger       map[string]any // effective trigger; nil means use workflows.input
	Principal     workflow.Principal
	Source        string // manual / runtime / schedule
	RequestID     string
	Codec         *secret.Codec
	MaxActive     int // workspace concurrency cap; 0 = DefaultWorkflowActiveLimit
	ScheduledFor  *time.Time
	IdempotencyID string
	Async         bool // true queues a job; false runs inline (sync runtime path)
}

func workflowAAD(workspaceID, operationID string) []byte {
	return []byte(workspaceID + ":operation-run:" + operationID)
}

// buildWorkflowSnapshot resolves a stored graph into an encrypted-run-ready
// snapshot: every step pins action ID/version, integration ID and connection
// ID and the graph re-validates in deploy mode.
func (s *Store) buildWorkflowSnapshot(ctx context.Context, db database, item model.Workflow, trigger map[string]any) (workflow.Snapshot, error) {
	definition, err := workflow.ParseDefinition(item.Graph)
	if err != nil {
		return workflow.Snapshot{}, fmt.Errorf("%w: %v", ErrConflict, err)
	}
	if trigger == nil {
		var defaults map[string]any
		if err := jsonutil.Unmarshal(item.Input, &defaults); err != nil || defaults == nil {
			return workflow.Snapshot{}, fmt.Errorf("%w: workflow default input must be a JSON object", ErrConflict)
		}
		trigger = defaults
	}
	if err := workflow.ValidateDefinition(definition, true, func(string) bool { return true }); err != nil {
		return workflow.Snapshot{}, fmt.Errorf("%w: %v", ErrConflict, err)
	}
	order, err := workflow.OrderSteps(definition.Steps)
	if err != nil {
		return workflow.Snapshot{}, fmt.Errorf("%w: %v", ErrConflict, err)
	}
	snapshot := workflow.Snapshot{
		WorkflowID:      item.ID,
		WorkflowVersion: item.Version,
		Trigger:         trigger,
		Output:          definition.Output,
	}
	for _, index := range order {
		step := definition.Steps[index]
		action, err := s.actionByKey(ctx, db, step.Action)
		if err != nil {
			return workflow.Snapshot{}, fmt.Errorf("%w: step %q action %q is not available: %v", ErrConflict, step.ID, step.Action, err)
		}
		if action.Status != "active" || !action.Executable {
			return workflow.Snapshot{}, fmt.Errorf("%w: step %q action %q is not active and executable", ErrConflict, step.ID, step.Action)
		}
		integration, err := s.integrationByIDOrKey(ctx, db, step.IntegrationID)
		if err != nil || integration.Status != "ready" {
			return workflow.Snapshot{}, fmt.Errorf("%w: step %q integration is not ready", ErrConflict, step.ID)
		}
		if integration.SystemID != action.SystemID {
			return workflow.Snapshot{}, fmt.Errorf("%w: step %q action and integration belong to different systems", ErrConflict, step.ID)
		}
		connection := model.Connection{}
		if integration.AuthFlow != "none" {
			connection, err = s.connectionForIntegration(ctx, db, integration.ID, step.ConnectionKey)
		}
		if err != nil || (integration.AuthFlow != "none" && (!connection.Enabled || connection.Status != "active" || connection.LastVerifiedAt == nil || connection.VerifiedTargetVersion != integration.TargetVersion || connection.VerifiedRevision != connection.Revision)) {
			return workflow.Snapshot{}, fmt.Errorf("%w: step %q connection is not active", ErrConflict, step.ID)
		}
		snapshot.Steps = append(snapshot.Steps, workflow.StepSnapshot{
			ID: step.ID, Title: step.Title, ActionKey: action.ActionKey,
			ActionID: action.ID, ActionVersion: action.Version,
			IntegrationID: integration.ID, ConnectionID: connection.ID,
			Input: step.Input, DependsOn: step.DependsOn, RunIf: step.RunIf,
			OnError: step.OnError,
		})
	}
	return snapshot, nil
}

func (s *Store) actionByKey(ctx context.Context, db database, key string) (model.ActionDefinition, error) {
	var item model.ActionDefinition
	err := db.QueryRow(ctx, `
		SELECT a.id::text, a.action_key, a.name, a.description, a.source,
		       a.system_id::text, s.system_key, COALESCE(a.integration_id::text, ''),
		       a.http_method, a.relative_path, a.required_scopes,
		       a.input_schema, a.output_schema, a.example_input,
		       a.executable, a.status, a.version, a.created_at, a.updated_at,a.request_config,a.response_config,a.execution_config
		FROM actions a
		JOIN systems s ON s.id = a.system_id AND s.workspace_id = a.workspace_id
		WHERE a.workspace_id = $1 AND a.deleted_at IS NULL AND (a.id::text = $2 OR s.system_key||'.'||a.action_key=$2 OR (a.action_key=$2 AND (SELECT count(*) FROM actions x WHERE x.workspace_id=$1 AND x.action_key=$2 AND x.deleted_at IS NULL)=1))`,
		s.workspaceID, key).Scan(
		&item.ID, &item.ActionKey, &item.Name, &item.Description, &item.Source,
		&item.SystemID, &item.SystemKey, &item.IntegrationID, &item.HTTPMethod,
		&item.RelativePath, &item.RequiredScopes, &item.InputSchema, &item.OutputSchema,
		&item.ExampleInput, &item.Executable, &item.Status, &item.Version,
		&item.CreatedAt, &item.UpdatedAt, &item.RequestConfig, &item.ResponseConfig, &item.ExecutionConfig)
	return item, mapNotFound(err)
}

func (s *Store) integrationByIDOrKey(ctx context.Context, db database, idOrKey string) (model.Integration, error) {
	var item model.Integration
	err := db.QueryRow(ctx, `
		SELECT i.id::text, i.workspace_id::text, i.integration_key, i.name,
		       i.system_id::text, s.system_key, s.name,
		       i.auth_instance_id::text, ai.name, i.base_url, i.status,
		       i.settings, i.version, i.created_at, i.updated_at, i.target_version,(SELECT flow_type FROM auth_templates WHERE id=ai.auth_template_id)
		FROM integrations i
		JOIN systems s ON s.id = i.system_id AND s.workspace_id = i.workspace_id
		JOIN auth_instances ai ON ai.id = i.auth_instance_id AND ai.workspace_id = i.workspace_id
		WHERE i.workspace_id = $1 AND i.deleted_at IS NULL
		  AND (i.id::text = $2 OR i.integration_key = $2)`,
		s.workspaceID, idOrKey).Scan(
		&item.ID, &item.WorkspaceID, &item.IntegrationKey, &item.Name,
		&item.SystemID, &item.SystemKey, &item.SystemName,
		&item.AuthInstanceID, &item.AuthName, &item.BaseURL, &item.Status,
		&item.Settings, &item.Version, &item.CreatedAt, &item.UpdatedAt, &item.TargetVersion, &item.AuthFlow)
	return item, mapNotFound(err)
}

func (s *Store) connectionForIntegration(ctx context.Context, db database, integrationID, idOrKey string) (model.Connection, error) {
	var item model.Connection
	err := db.QueryRow(ctx, `
		SELECT c.id::text, c.workspace_id::text, c.connection_key, c.name,
		       c.integration_id::text, i.integration_key, s.system_key,
		       c.auth_instance_id::text, c.end_user_id::text, eu.external_key, COALESCE(eu.display_name,''), COALESCE(eu.email,''), eu.metadata,
		       c.status, c.credential_blob, c.key_version, c.revision,
		       c.token_expires_at, c.last_verified_at, c.last_used_at,
		       COALESCE(c.last_error_code, ''), COALESCE(c.last_error_message, ''),
		       c.tags, c.created_at, c.updated_at,c.verified_target_version,c.verified_revision,c.enabled
		FROM connections c
		JOIN integrations i ON i.id = c.integration_id AND i.workspace_id = c.workspace_id
		JOIN systems s ON s.id = i.system_id AND s.workspace_id = c.workspace_id
		JOIN end_users eu ON eu.id = c.end_user_id AND eu.workspace_id = c.workspace_id
		WHERE c.workspace_id = $1 AND c.deleted_at IS NULL
		  AND c.integration_id = $2 AND (c.id::text = $3 OR c.connection_key = $3)`,
		s.workspaceID, integrationID, idOrKey).Scan(
		&item.ID, &item.WorkspaceID, &item.ConnectionKey, &item.Name,
		&item.IntegrationID, &item.IntegrationKey, &item.SystemKey,
		&item.AuthInstanceID, &item.EndUserID, &item.EndUserKey,
		&item.EndUserName, &item.EndUserEmail, &item.Metadata, &item.Status,
		&item.CredentialBlob, &item.KeyVersion, &item.Revision,
		&item.TokenExpiresAt, &item.LastVerifiedAt, &item.LastUsedAt,
		&item.LastErrorCode, &item.LastErrorMessage, &item.Tags,
		&item.CreatedAt, &item.UpdatedAt, &item.VerifiedTargetVersion, &item.VerifiedRevision, &item.Enabled)
	return item, mapNotFound(err)
}

// startWorkflowRun is the shared transaction: lock the workflow version,
// rebuild and validate the snapshot, encrypt it, persist the operation run
// and (when async) the job row.
func (s *Store) startWorkflowRun(ctx context.Context, input WorkflowTriggerInput, initialStatus string) (model.OperationRun, workflow.Snapshot, error) {
	var operation model.OperationRun
	var snapshot workflow.Snapshot
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		item, err := scanWorkflow(tx.QueryRow(ctx, `
			SELECT id::text, workflow_key, name, description, status, graph, schedule_type,
			       COALESCE(cron_expression, ''), schedule_timezone, next_run_at,
			       retry_policy, input, version, created_at, updated_at
			FROM workflows
			WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL
			FOR UPDATE`, s.workspaceID, input.Workflow.ID))
		if err != nil {
			return mapNotFound(err)
		}
		if item.Status != "deployed" {
			return fmt.Errorf("%w: workflow is not deployed", ErrConflict)
		}
		if item.Version != input.Workflow.Version {
			return fmt.Errorf("%w: workflow changed; retry the request", ErrConflict)
		}
		if input.Async {
			if err := s.checkWorkflowCapacity(ctx, tx, input.MaxActive); err != nil {
				return err
			}
		}
		built, err := s.buildWorkflowSnapshot(ctx, tx, item, input.Trigger)
		if err != nil {
			return err
		}
		operationID := newOperationID(input.RequestID)
		cipher, err := s.sealSnapshot(input.Codec, built, operationID)
		if err != nil {
			return err
		}
		now := utcNow()
		expiresAt, err := s.OperationExpiresAt(ctx, now)
		if err != nil {
			return err
		}
		principal := input.Principal
		operation = model.OperationRun{
			ID: operationID, RequestID: input.RequestID, Kind: "workflow", Name: item.Name,
			Status: initialStatus, WorkflowID: item.ID, WorkflowVersion: item.Version,
			ScheduledFor: input.ScheduledFor, RuntimeTokenID: principal.TokenID,
			Source: input.Source, Input: safejson.Marshal(built.Trigger, 4<<10),
			StartedAt: now, ExpiresAt: expiresAt,
			WorkflowPayloadCipher: cipher,
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO operation_runs(
				id, workspace_id, request_id, kind, name, status,
				workflow_id, workflow_version, scheduled_for, workflow_payload_ciphertext,
				runtime_token_id, source, input, started_at, expires_at
			) VALUES($1, $2, $3, 'workflow', $4, $5, $6, $7, $8, $9,
			         NULLIF($10, '')::uuid, $11, $12, $13, $14)`,
			operation.ID, s.workspaceID, operation.RequestID, operation.Name, operation.Status,
			operation.WorkflowID, operation.WorkflowVersion, nullTime(operation.ScheduledFor), cipher,
			operation.RuntimeTokenID, operation.Source, nilIfEmptyJSON(operation.Input),
			operation.StartedAt, operation.ExpiresAt); err != nil {
			if IsUniqueViolation(err) && input.Source == "schedule" {
				// The unique (workspace, workflow, scheduled_for) index for
				// schedule runs already recorded this planned trigger.
				return ErrConflict
			}
			return err
		}
		if input.IdempotencyID != "" {
			command, err := tx.Exec(ctx, `UPDATE idempotency_records SET status='running',operation_id=$3 WHERE workspace_id=$1 AND id=$2 AND status='claimed'`, s.workspaceID, input.IdempotencyID, operation.ID)
			if err != nil {
				return err
			}
			if command.RowsAffected() != 1 {
				return ErrConflict
			}
		}
		if input.Async {
			payload, _ := json.Marshal(map[string]string{"operationId": operation.ID})
			if _, err := tx.Exec(ctx, `
				INSERT INTO jobs(id, workspace_id, kind, resource_id, operation_id, payload, status, max_attempts, run_after)
				VALUES($1, $2, 'workflow_run', $3, $4, $5, 'queued', 1, now())`,
				StableID("workflow-job", operation.ID), s.workspaceID, operation.ID, operation.ID, payload); err != nil {
				return err
			}
		}
		snapshot = built
		operation.WorkflowPayloadCipher = nil
		return nil
	})
	if err != nil {
		return model.OperationRun{}, workflow.Snapshot{}, err
	}
	return operation, snapshot, nil
}

func newOperationID(requestID string) string {
	if requestID == "" {
		return randomID()
	}
	return StableID("operation", requestID)
}

// EnqueueWorkflow queues an async or manual run; the workspace-level active
// workflow job cap fails closed with ErrBusy.
func (s *Store) EnqueueWorkflow(ctx context.Context, input WorkflowTriggerInput) (model.OperationRun, error) {
	input.Async = true
	operation, _, err := s.startWorkflowRun(ctx, input, "queued")
	return operation, err
}

// CreateWorkflowRun starts an inline (sync runtime) run with status running
// and no job row.
func (s *Store) CreateWorkflowRun(ctx context.Context, input WorkflowTriggerInput) (model.OperationRun, workflow.Snapshot, error) {
	input.Async = false
	return s.startWorkflowRun(ctx, input, "running")
}

// WorkflowRunSnapshot decrypts the pinned run definition for a worker.
func (s *Store) WorkflowRunSnapshot(ctx context.Context, codec *secret.Codec, operationID string) (workflow.Snapshot, error) {
	var cipher []byte
	err := s.database(ctx).QueryRow(ctx, `
		SELECT workflow_payload_ciphertext FROM operation_runs
		WHERE workspace_id = $1 AND id = $2 AND kind = 'workflow'`,
		s.workspaceID, operationID).Scan(&cipher)
	if err != nil {
		return workflow.Snapshot{}, mapNotFound(err)
	}
	plain, err := codec.Decrypt(cipher, workflowAAD(s.workspaceID, operationID))
	if err != nil {
		return workflow.Snapshot{}, err
	}
	var snapshot workflow.Snapshot
	if err := jsonutil.Unmarshal(plain, &snapshot); err != nil {
		return workflow.Snapshot{}, fmt.Errorf("decode workflow snapshot: %w", err)
	}
	return snapshot, nil
}

// WorkflowRunResult decrypts the final output of a finished run for callers
// authorized to read it.
func (s *Store) WorkflowRunResult(ctx context.Context, codec *secret.Codec, operationID string) (json.RawMessage, error) {
	var cipher []byte
	var status string
	err := s.database(ctx).QueryRow(ctx, `
		SELECT status, workflow_result_ciphertext FROM operation_runs
		WHERE workspace_id = $1 AND id = $2 AND kind = 'workflow'`,
		s.workspaceID, operationID).Scan(&status, &cipher)
	if err != nil {
		return nil, mapNotFound(err)
	}
	if status != "success" || len(cipher) == 0 {
		return nil, ErrNotFound
	}
	plain, err := codec.Decrypt(cipher, workflowAAD(s.workspaceID, operationID))
	if err != nil {
		return nil, err
	}
	return json.RawMessage(plain), nil
}

// FinishWorkflowRun atomically commits the job outcome, the operation run,
// its sanitized preview, the encrypted final output and the idempotency
// record. job may be zero-valued for the inline sync path.
func (s *Store) FinishWorkflowRun(ctx context.Context, job model.Job, runID, status string, httpStatus int, preview []byte, resultCipher []byte, code, message string, record model.IdempotencyRecord, envelope []byte) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		if job.ID != "" {
			if err := s.fenceJob(ctx, tx, job); err != nil {
				return err
			}
		}
		command, err := tx.Exec(ctx, `
			UPDATE operation_runs SET status = $3, http_status = NULLIF($4, 0),
			       output = $5, workflow_result_ciphertext = NULLIF($6, ''::bytea),
			       error_code = NULLIF($7, ''), error_message = NULLIF($8, ''),
			       completed_at = now()
			WHERE workspace_id = $1 AND id = $2 AND status IN ('queued', 'running')`,
			s.workspaceID, runID, status, httpStatus, nilIfEmptyJSON(preview),
			resultCipher, code, message)
		if err != nil {
			return err
		}
		if command.RowsAffected() == 0 {
			return ErrConflict
		}
		if job.ID != "" {
			if _, err := tx.Exec(ctx, `
				UPDATE jobs SET status = 'succeeded', completed_at = now(),
				       lease_owner = NULL, lease_expires_at = NULL, updated_at = now()
				WHERE workspace_id = $1 AND id = $2 AND status = 'running'`,
				s.workspaceID, job.ID); err != nil {
				return err
			}
		}
		if record.ID == "" {
			return nil
		}
		tag, err := tx.Exec(ctx, `UPDATE idempotency_records SET status='completed',http_status=$3,response=$4 WHERE workspace_id=$1 AND id=$2 AND status='running' AND operation_id=$5`,
			s.workspaceID, record.ID, httpStatus, envelope, runID)
		if err == nil && tag.RowsAffected() != 1 {
			return ErrConflict
		}
		return err
	})
}

// FailWorkflowRun records a final failed or unknown outcome without ever
// replaying the run; the job dies with its single attempt.
func (s *Store) FailWorkflowRun(ctx context.Context, job model.Job, runID, status string, code, message string, record model.IdempotencyRecord, envelope []byte) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		if job.ID != "" {
			if err := s.fenceJob(ctx, tx, job); err != nil {
				return err
			}
		}
		command, err := tx.Exec(ctx, `
			UPDATE operation_runs SET status = $3, error_code = NULLIF($4, ''),
			       error_message = NULLIF($5, ''), completed_at = now()
			WHERE workspace_id = $1 AND id = $2 AND status IN ('queued', 'running')`,
			s.workspaceID, runID, status, code, message)
		if err != nil {
			return err
		}
		if command.RowsAffected() == 0 {
			return ErrConflict
		}
		if job.ID != "" {
			if _, err := tx.Exec(ctx, `
				UPDATE jobs SET status = 'dead', completed_at = now(), last_error = $3,
				       lease_owner = NULL, lease_expires_at = NULL, updated_at = now()
				WHERE workspace_id = $1 AND id = $2 AND status = 'running'`,
				s.workspaceID, job.ID, message); err != nil {
				return err
			}
		}
		if record.ID == "" {
			return nil
		}
		tag, err := tx.Exec(ctx, `UPDATE idempotency_records SET status='completed',http_status=$3,response=$4 WHERE workspace_id=$1 AND id=$2 AND status='running' AND operation_id=$5`,
			s.workspaceID, record.ID, record.HTTPStatus, envelope, runID)
		if err == nil && tag.RowsAffected() != 1 {
			return ErrConflict
		}
		return err
	})
}

// MarkWorkflowOperationRunning flags the queued run as running and fails when
// another worker or a lost lease already moved it on.
func (s *Store) MarkWorkflowOperationRunning(ctx context.Context, id string) error {
	command, err := s.database(ctx).Exec(ctx, `
		UPDATE operation_runs SET status = 'running'
		WHERE workspace_id = $1 AND id = $2 AND status = 'queued'`, s.workspaceID, id)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

// EnqueueDueWorkflows selects deployed workflows whose next_run_at is due,
// enqueues one pinned run per planned time and advances the schedule from
// the planned time (never replaying missed triggers). Workflows whose
// previous scheduled run is still active are skipped with their schedule
// advanced; the returned list reports their keys for structured logging.
func (s *Store) EnqueueDueWorkflows(ctx context.Context, limit int, codec *secret.Codec) (int, []string, error) {
	enqueued := 0
	var skipped []string
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id::text FROM workflows
			WHERE workspace_id = $1 AND status = 'deployed' AND deleted_at IS NULL
			  AND schedule_type IN ('interval', 'cron') AND next_run_at <= now()
			ORDER BY next_run_at, id
			FOR UPDATE SKIP LOCKED LIMIT $2`, s.workspaceID, limit)
		if err != nil {
			return err
		}
		ids := []string{}
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
			item, err := scanWorkflow(tx.QueryRow(ctx, `
				SELECT id::text, workflow_key, name, description, status, graph, schedule_type,
				       COALESCE(cron_expression, ''), schedule_timezone, next_run_at,
				       retry_policy, input, version, created_at, updated_at
				FROM workflows WHERE workspace_id = $1 AND id = $2 FOR UPDATE`,
				s.workspaceID, id))
			if err != nil {
				return err
			}
			if item.NextRunAt == nil {
				continue
			}
			planned := *item.NextRunAt
			next, advanceErr := advanceWorkflowSchedule(item, planned, utcNow())
			if advanceErr != nil {
				// A broken schedule must not block the batch or fire every
				// tick: stop scheduling it and surface the reason.
				if _, err := tx.Exec(ctx, `
					UPDATE workflows SET next_run_at = NULL, updated_at = now()
					WHERE workspace_id = $1 AND id = $2`, s.workspaceID, id); err != nil {
					return err
				}
				skipped = append(skipped, item.WorkflowKey+" (invalid schedule: "+advanceErr.Error()+")")
				continue
			}
			var active bool
			if err := tx.QueryRow(ctx, `
				SELECT EXISTS(
					SELECT 1 FROM jobs j
					JOIN operation_runs o ON o.id = j.operation_id AND o.workspace_id = j.workspace_id
					WHERE j.workspace_id = $1 AND j.kind = 'workflow_run'
					  AND j.status IN ('queued', 'running')
					  AND o.workflow_id = $2 AND o.source = 'schedule')`,
				s.workspaceID, id).Scan(&active); err != nil {
				return err
			}
			if active {
				if _, err := tx.Exec(ctx, `
					UPDATE workflows SET next_run_at = $3, updated_at = now()
					WHERE workspace_id = $1 AND id = $2`, s.workspaceID, id, next); err != nil {
					return err
				}
				skipped = append(skipped, item.WorkflowKey)
				continue
			}
			requestID := "workflow-schedule-" + id + "-" + planned.UTC().Format(time.RFC3339Nano)
			input := WorkflowTriggerInput{
				Workflow: item, Trigger: nil,
				Principal: workflow.Principal{Kind: "schedule"},
				Source:    "schedule", RequestID: requestID,
				ScheduledFor: &planned,
			}
			if err := s.enqueueDueRun(ctx, tx, input, codec); err != nil {
				if errors.Is(err, ErrBusy) {
					skipped = append(skipped, item.WorkflowKey+" (workspace capacity reached)")
					break
				}
				return err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE workflows SET next_run_at = $3, updated_at = now()
				WHERE workspace_id = $1 AND id = $2`, s.workspaceID, id, next); err != nil {
				return err
			}
			enqueued++
		}
		return nil
	})
	return enqueued, skipped, err
}

// enqueueDueRun duplicates the enqueue half of startWorkflowRun inside the
// scheduler transaction with the schedule source; a unique violation on the
// (workflow, scheduled_for) index means another scheduler instance already
// queued this planned time and is not an error.
func (s *Store) enqueueDueRun(ctx context.Context, tx pgx.Tx, input WorkflowTriggerInput, codec *secret.Codec) error {
	if err := s.checkWorkflowCapacity(ctx, tx, DefaultWorkflowActiveLimit); err != nil {
		return err
	}
	built, err := s.buildWorkflowSnapshot(ctx, tx, input.Workflow, input.Trigger)
	if err != nil {
		return err
	}
	operationID := StableID("operation", input.RequestID)
	now := utcNow()
	expiresAt, err := s.OperationExpiresAt(ctx, now)
	if err != nil {
		return err
	}
	preview := safejson.Marshal(built.Trigger, 4<<10)
	cipher, err := s.sealSnapshot(codec, built, operationID)
	if err != nil {
		return err
	}
	command, err := tx.Exec(ctx, `
		INSERT INTO operation_runs(
			id, workspace_id, request_id, kind, name, status,
			workflow_id, workflow_version, scheduled_for, workflow_payload_ciphertext,
			source, input, started_at, expires_at
		) VALUES($1, $2, $3, 'workflow', $4, 'queued', $5, $6, $7, $8, 'schedule', $9, $10, $11)
		ON CONFLICT (workspace_id, workflow_id, scheduled_for)
		WHERE source = 'schedule' AND workflow_id IS NOT NULL DO NOTHING`,
		operationID, s.workspaceID, input.RequestID, input.Workflow.Name,
		input.Workflow.ID, input.Workflow.Version, nullTime(input.ScheduledFor), cipher,
		nilIfEmptyJSON(preview), now, expiresAt)
	if err != nil {
		if IsUniqueViolation(err) {
			return nil
		}
		return err
	}
	if command.RowsAffected() == 0 {
		return nil
	}
	payload, _ := json.Marshal(map[string]string{"operationId": operationID})
	_, err = tx.Exec(ctx, `
		INSERT INTO jobs(id, workspace_id, kind, resource_id, operation_id, payload, status, max_attempts, run_after)
		VALUES($1, $2, 'workflow_run', $3, $4, $5, 'queued', 1, now())`,
		StableID("workflow-job", operationID), s.workspaceID, operationID, operationID, payload)
	return err
}

func (s *Store) sealSnapshot(codec *secret.Codec, snapshot workflow.Snapshot, operationID string) ([]byte, error) {
	if codec == nil {
		return nil, errors.New("encryption codec is required for workflow runs")
	}
	plain, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	return codec.Encrypt(plain, workflowAAD(s.workspaceID, operationID))
}

// advanceWorkflowSchedule computes the next planned time strictly after now,
// counting from the previous planned time so downtime skips missed runs.
func advanceWorkflowSchedule(item model.Workflow, planned, now time.Time) (time.Time, error) {
	switch item.ScheduleType {
	case "interval":
		next, err := cronx.AdvanceInterval(item.CronExpression, planned, now)
		if err != nil {
			return time.Time{}, err
		}
		return next.UTC(), nil
	case "cron":
		schedule, err := cronx.Parse(item.CronExpression)
		if err != nil {
			return time.Time{}, err
		}
		location, err := cronx.LoadTimezone(item.ScheduleTimezone)
		if err != nil {
			return time.Time{}, err
		}
		next, err := schedule.Advance(planned, now, location)
		if err != nil {
			return time.Time{}, err
		}
		return next.UTC(), nil
	default:
		return time.Time{}, fmt.Errorf("schedule type %q is not schedulable", item.ScheduleType)
	}
}

// ValidateWorkflowReady re-checks deployability at trigger time: bindings,
// action availability and schedule validity.
func (s *Store) ValidateWorkflowReady(ctx context.Context, item model.Workflow) error {
	if item.Status != "deployed" {
		return fmt.Errorf("%w: workflow is not deployed", ErrConflict)
	}
	if _, err := s.buildWorkflowSnapshot(ctx, s.pool, item, nil); err != nil {
		return err
	}
	if item.ScheduleType == "interval" && cronx.IntervalDuration(item.CronExpression) == 0 {
		return fmt.Errorf("%w: interval label is invalid", ErrConflict)
	}
	if item.ScheduleType == "cron" {
		if _, err := cronx.Parse(item.CronExpression); err != nil {
			return fmt.Errorf("%w: %v", ErrConflict, err)
		}
		if _, err := cronx.LoadTimezone(item.ScheduleTimezone); err != nil {
			return fmt.Errorf("%w: %v", ErrConflict, err)
		}
	}
	return nil
}

// ResolveWorkflowBindings rewrites integration/connection references in the
// graph to canonical IDs and returns validation errors naming the step.
func (s *Store) ResolveWorkflowBindings(ctx context.Context, item model.Workflow) (model.Workflow, error) {
	definition, err := workflow.ParseDefinition(item.Graph)
	if err != nil {
		return model.Workflow{}, fmt.Errorf("%w: %v", ErrConflict, err)
	}
	for index, step := range definition.Steps {
		integration, err := s.integrationByIDOrKey(ctx, s.database(ctx), step.IntegrationID)
		if err != nil || integration.Status != "ready" {
			return model.Workflow{}, fmt.Errorf("%w: step %q has no ready integration", ErrConflict, step.ID)
		}
		action, err := s.actionByKey(ctx, s.database(ctx), step.Action)
		if err != nil || action.Status != "active" || !action.Executable {
			return model.Workflow{}, fmt.Errorf("%w: step %q action %q is not active and executable", ErrConflict, step.ID, step.Action)
		}
		if action.SystemID != integration.SystemID {
			return model.Workflow{}, fmt.Errorf("%w: step %q action and integration belong to different systems", ErrConflict, step.ID)
		}
		connection := model.Connection{}
		if integration.AuthFlow != "none" {
			connection, err = s.connectionForIntegration(ctx, s.database(ctx), integration.ID, step.ConnectionKey)
		}
		if err != nil || (integration.AuthFlow != "none" && (!connection.Enabled || connection.Status != "active" || connection.LastVerifiedAt == nil || connection.VerifiedTargetVersion != integration.TargetVersion || connection.VerifiedRevision != connection.Revision)) {
			return model.Workflow{}, fmt.Errorf("%w: step %q has no active connection", ErrConflict, step.ID)
		}
		definition.Steps[index].IntegrationID = integration.ID
		definition.Steps[index].ConnectionKey = connection.ID
	}
	encoded, err := json.Marshal(definition)
	if err != nil {
		return model.Workflow{}, err
	}
	item.Graph = encoded
	return item, nil
}

// Serialize capacity checks across all workflows in the workspace.
func (s *Store) checkWorkflowCapacity(ctx context.Context, tx pgx.Tx, limit int) error {
	if limit <= 0 {
		limit = DefaultWorkflowActiveLimit
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, s.workspaceID+":workflow-capacity"); err != nil {
		return err
	}
	var active int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE workspace_id=$1 AND kind='workflow_run' AND status IN ('queued','running')`, s.workspaceID).Scan(&active); err != nil {
		return err
	}
	if active >= limit {
		return ErrBusy
	}
	return nil
}
