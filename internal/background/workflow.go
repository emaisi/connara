package background

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"apihub-go/internal/authn"
	"apihub-go/internal/catalog"
	"apihub-go/internal/executor"
	"apihub-go/internal/model"
	"apihub-go/internal/policy"
	"apihub-go/internal/safejson"
	"apihub-go/internal/store"
	"apihub-go/internal/workflow"
)

// workflowRunBudget bounds one asynchronous workflow run.
const workflowRunBudget = 15 * time.Minute

// storeDeps adapts the store and authn service to the workflow engine so the
// HTTP sync path and the background worker share one implementation.
type storeDeps struct {
	store *store.Store
	auth  *authn.Service
}

func (d storeDeps) Action(ctx context.Context, id string) (model.ActionDefinition, error) {
	return d.store.Action(ctx, id)
}

func (d storeDeps) Integration(ctx context.Context, id string) (model.Integration, error) {
	return d.store.Integration(ctx, id)
}

func (d storeDeps) Connection(ctx context.Context, id string) (model.Connection, error) {
	return d.store.Connection(ctx, id)
}

func (d storeDeps) ResolveAuth(ctx context.Context, connection model.Connection) (executor.RequestAuth, error) {
	integration, err := d.store.Integration(ctx, connection.IntegrationID)
	if err != nil {
		return executor.RequestAuth{}, err
	}
	if !connection.Enabled || connection.LastVerifiedAt == nil || connection.VerifiedTargetVersion != integration.TargetVersion || connection.VerifiedRevision != connection.Revision {
		return executor.RequestAuth{}, errors.New("account must be verified on the current target")
	}
	resolved, err := d.auth.ResolveConnection(ctx, connection)
	if err != nil {
		return executor.RequestAuth{}, err
	}
	return resolved.RequestAuth(), nil
}

// AuthorizeStep re-reads the runtime token on every step (deny-wins) so
// revocation or expiry stops the remaining steps. Control-plane principals
// (manual admin triggers, scheduler) run under their explicit identity.
func (d storeDeps) AuthorizeStep(ctx context.Context, principal workflow.Principal, action model.ActionDefinition, connection model.Connection) error {
	if principal.Kind != "runtime_token" {
		return nil
	}
	token, err := d.store.RuntimeTokenByID(ctx, principal.TokenID)
	if err != nil {
		return fmt.Errorf("runtime token is no longer valid")
	}
	if !policy.AllowsAction(token, action.ActionKey) {
		return errors.New("runtime policy denied this action")
	}
	if connection.ID != "" && !policy.AllowsConnection(token, connection) {
		return errors.New("runtime policy denied this connection")
	}
	return nil
}

// NewWorkflowRunner wires the shared engine for both execution paths.
func NewWorkflowRunner(database *store.Store, auth *authn.Service, actionExecutor *executor.Executor, providerCatalog *catalog.Catalog) *workflow.Runner {
	return &workflow.Runner{
		Deps:     storeDeps{store: database, auth: auth},
		Executor: actionExecutor,
		Catalog:  providerCatalog,
	}
}

// RunWorkflowJob executes one queued workflow job from its pinned snapshot.
// Workflow-level failures (step failures, unknown upstream outcomes) are
// committed here through the fenced transaction and reported as success to
// the job machinery; only infrastructure errors bubble up for FailJob.
func (s *Service) RunWorkflowJob(ctx context.Context, job model.Job) error {
	ctx, cancel := context.WithTimeout(ctx, workflowRunBudget)
	defer cancel()
	snapshot, err := s.store.WorkflowRunSnapshot(ctx, s.codec, job.OperationID)
	if err != nil {
		return err
	}
	operation, err := s.store.Operation(ctx, job.OperationID)
	if err != nil {
		return err
	}
	principal := workflow.Principal{Kind: operation.Source}
	switch operation.Source {
	case "runtime":
		principal.Kind = "runtime_token"
		principal.TokenID = operation.RuntimeTokenID
	case "manual":
		principal.Kind = "admin"
	case "schedule":
		principal.Kind = "schedule"
	default:
		return fmt.Errorf("workflow run has unsupported source %q", operation.Source)
	}
	if principal.Kind == "runtime_token" {
		if _, tokenErr := s.store.RuntimeTokenByID(ctx, principal.TokenID); tokenErr != nil {
			message := "runtime token was revoked or expired before the run started"
			return s.commitWorkflowFailure(job, operation.ID, "failed", "policy_denied", message)
		}
	}
	if err := s.store.MarkWorkflowOperationRunning(ctx, operation.ID); err != nil {
		return err
	}
	runner := NewWorkflowRunner(s.store, s.auth, s.executor, s.catalog)
	result, runErr := runner.Run(ctx, snapshot, workflow.Options{
		Principal: principal,
		Attempt:   job.Attempt,
		OnStep: func(outcome workflow.StepOutcome) {
			attributes := map[string]any{
				"attempt":    job.Attempt,
				"stepId":     outcome.StepID,
				"actionKey":  outcome.ActionKey,
				"status":     outcome.Status,
				"httpStatus": outcome.ProviderStatus,
			}
			if outcome.Title != "" {
				attributes["title"] = outcome.Title
			}
			if outcome.Error != "" {
				attributes["error"] = outcome.Error
			}
			if outcome.Output != nil {
				attributes["responsePreview"] = json.RawMessage(safejson.Marshal(outcome.Output, 4<<10))
			}
			if err := s.store.AddOperationEvent(ctx, operation.ID, levelForStep(outcome.Status), messageForStep(outcome), store.MarshalJSON(attributes)); err != nil {
				s.logger.ErrorContext(ctx, "record workflow step event", "operation_id", operation.ID, "step", outcome.StepID, "error", err)
			}
		},
	})
	if runErr == nil {
		plain, err := json.Marshal(result.Final)
		if err != nil {
			return err
		}
		cipher, err := s.codec.Encrypt(plain, []byte(s.store.WorkspaceID()+":operation-run:"+operation.ID))
		if err != nil {
			return err
		}
		preview := safejson.Marshal(map[string]any{
			"stepCount": len(snapshot.Steps), "hasWarnings": result.HasWarnings,
			"failedSteps": result.FailedSteps,
		}, 4<<10)
		finishCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		return s.store.FinishWorkflowRun(finishCtx, job, operation.ID, "success", 0, preview, cipher, "", "", model.IdempotencyRecord{}, nil)
	}
	var typed *workflow.Error
	if !errors.As(runErr, &typed) {
		return runErr
	}
	status := typed.Status
	if status != workflow.StatusUnknown {
		status = "failed"
	}
	message := typed.Message
	if typed.StepID != "" {
		message = fmt.Sprintf("step %s: %s", typed.StepID, typed.Message)
	}
	return s.commitWorkflowFailure(job, operation.ID, status, typed.Code, message)
}

func (s *Service) commitWorkflowFailure(job model.Job, operationID, status, code, message string) error {
	finishCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	return s.store.FailWorkflowRun(finishCtx, job, operationID, status, code, message, model.IdempotencyRecord{}, nil)
}

func levelForStep(status string) string {
	switch status {
	case workflow.StatusSuccess:
		return "info"
	case workflow.StatusSkipped:
		return "info"
	case workflow.StatusUnknown:
		return "error"
	default:
		return "warn"
	}
}

func messageForStep(outcome workflow.StepOutcome) string {
	label := outcome.Title
	if label == "" {
		label = outcome.StepID
	}
	switch outcome.Status {
	case workflow.StatusSuccess:
		return "工作流步骤 " + label + " 执行成功"
	case workflow.StatusSkipped:
		return "工作流步骤 " + label + " 被条件跳过"
	default:
		return "工作流步骤 " + label + " 执行失败"
	}
}
