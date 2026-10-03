package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"apihub-go/internal/catalog"
	"apihub-go/internal/executor"
	"apihub-go/internal/jsonutil"
	"apihub-go/internal/model"
	"apihub-go/internal/safejson"
)

// Execution budget and payload limits shared by both execution paths.
const (
	StepTimeout        = 40 * time.Second
	MaxAccumulatedBody = 8 << 20
	MaxFinalOutput     = 4 << 20
)

// Principal identifies who triggered the run. runtime_token principals are
// re-checked per step; admin and schedule are explicit control-plane
// identities.
type Principal struct {
	Kind    string
	TokenID string
}

// Dependencies abstracts everything the runner needs from its host so the
// engine stays unit-testable and free of storage imports.
type Dependencies interface {
	Action(ctx context.Context, id string) (model.ActionDefinition, error)
	Integration(ctx context.Context, id string) (model.Integration, error)
	Connection(ctx context.Context, id string) (model.Connection, error)
	ResolveAuth(ctx context.Context, connection model.Connection) (executor.RequestAuth, error)
	AuthorizeRun(ctx context.Context, principal Principal) error
	AuthorizeStep(ctx context.Context, principal Principal, action model.ActionDefinition, connection model.Connection) error
}

// Runner executes a pinned snapshot serially in topological order.
type Runner struct {
	Deps        Dependencies
	CodeRunner  *CodeRunner
	Executor    *executor.Executor
	Catalog     *catalog.Catalog
	StepTimeout time.Duration
}

// stepBudget defaults to the shared 40-second per-step limit.
func (r *Runner) stepBudget() time.Duration {
	if r.StepTimeout > 0 {
		return r.StepTimeout
	}
	return StepTimeout
}

// StepOutcome is the per-step result of one attempt. Output stays in memory
// for template resolution; persistence layers only store sanitized previews.
type StepOutcome struct {
	StepID             string
	Title              string
	ActionKey          string
	Status             string // success / failed / skipped / unknown
	ProviderStatus     int
	Output             any
	Error              string
	StepType           string
	FailureClass       string
	Phase              string
	ErrorCode          string
	APICallStatus      string
	StartedAt          time.Time
	CompletedAt        time.Time
	DurationMS         int64
	InputPreview       any
	ResponsePreview    json.RawMessage
	Diagnostics        []Diagnostic
	SelectedBranchID   string
	SkipReason         string
	AssignmentStatus   string
	VariableChanges    map[string]any
	OperationSummaries []map[string]any
	ConditionTrace     []map[string]any
	VariableReads      map[string]any
	LogCount           int
	LogsTruncated      bool
}

// RunResult carries the mapped final output plus per-step outcomes.
type RunResult struct {
	Outputs     map[string]any
	Final       map[string]any
	Outcomes    []StepOutcome
	HasWarnings bool
	FailedSteps []string
}

// Options tunes one run.
type Options struct {
	Principal   Principal
	Attempt     int
	OnStep      func(StepOutcome)
	OnStepStart func(StepOutcome)
}

// Error carries a machine-readable code for HTTP mapping. Unknown marks runs
// whose upstream outcome could not be confirmed and must not be retried.
type Error struct {
	Code    string
	Message string
	StepID  string
	Status  string // failed or unknown
	Phase   string
}

func (e *Error) Error() string {
	if e.StepID != "" {
		return fmt.Sprintf("step %s: %s", e.StepID, e.Message)
	}
	return e.Message
}

// Run executes the snapshot. It returns a RunResult when the workflow
// finished (including runs with tolerated warnings); a non-nil error means
// the run failed or its outcome is unknown.
func (r *Runner) Run(ctx context.Context, snapshot Snapshot, options Options) (RunResult, error) {
	ordered, err := OrderSnapshots(snapshot.Steps)
	if err != nil {
		return RunResult{}, &Error{Phase: "definition", Code: "workflow_step_failed", Message: err.Error(), Status: StatusFailed}
	}
	if snapshot.SchemaVersion != 0 && snapshot.SchemaVersion != 1 && snapshot.SchemaVersion != 2 {
		return RunResult{}, &Error{Phase: "definition", Code: "workflow_not_supported", Message: "snapshot schema is not supported", Status: StatusFailed}
	}
	for _, step := range ordered {
		if step.Type != "" && step.Type != "api" && (snapshot.SchemaVersion != 2 || step.Type != "code" && step.Type != "condition" && step.Type != "transform") {
			return RunResult{}, &Error{Phase: "definition", Code: "workflow_not_supported", Message: "snapshot node type is not supported", Status: StatusFailed}
		}
	}
	if err := r.Deps.AuthorizeRun(ctx, options.Principal); err != nil {
		return RunResult{}, &Error{Phase: "authorization", Code: "policy_denied", Message: "run identity is no longer valid", Status: StatusFailed}
	}
	hasCode := false
	for _, step := range ordered {
		hasCode = hasCode || step.Type == "code"
	}
	if hasCode {
		release, err := r.CodeRunner.BeginFormal(ctx)
		if err != nil {
			return RunResult{}, &Error{Phase: "preflight", Code: CodeErrorCode(err), Message: CodeErrorCode(err), Status: StatusFailed}
		}
		defer release()
		if err := r.CodeRunner.Preflight(ctx); err != nil {
			return RunResult{}, &Error{Phase: "preflight", Code: CodeErrorCode(err), Message: CodeErrorCode(err), Status: StatusFailed}
		}
		for _, step := range ordered {
			if step.Type == "code" && step.RunnerBuild != CodeBuild {
				return RunResult{}, &Error{Phase: "preflight", Code: "code_runtime_unavailable", Message: "code build is incompatible", Status: StatusFailed}
			}
		}
	}
	variables := map[string]any{}
	if snapshot.SchemaVersion == 2 {
		copied, err := cloneJSON(snapshot.InitialVariables)
		if err != nil {
			return RunResult{}, err
		}
		if values, ok := copied.(map[string]any); ok {
			variables = values
		}
		if err := ValidateTrigger(snapshot.InputSchema, snapshot.Trigger); err != nil {
			return RunResult{}, &Error{Phase: "input", Code: "workflow_input_invalid", Message: "trigger input does not match schema", Status: StatusFailed}
		}
	}
	result := RunResult{Outputs: map[string]any{}, FailedSteps: []string{}}
	statuses := map[string]string{}
	statusValues := map[string]any{}
	templateContext := map[string]any{"trigger": snapshot.Trigger, "status": statusValues}
	if snapshot.SchemaVersion == 2 {
		templateContext["vars"] = variables
	}
	accumulated := 0
	for _, step := range ordered {
		started := time.Now()
		start := StepOutcome{StepID: step.ID, Title: step.Title, StepType: step.Type, ActionKey: step.ActionKey, Status: "running", StartedAt: started.UTC()}
		if start.StepType == "" {
			start.StepType = "api"
		}
		if options.OnStepStart != nil {
			options.OnStepStart(start)
		}
		outcome := r.executeStep(ctx, step, snapshot, options, templateContext, statuses, &accumulated)
		if snapshot.SchemaVersion == 2 && outcome.Status != StatusSkipped && outcome.VariableReads == nil {
			outcome.VariableReads = variableReads(step, nil, outcome.ConditionTrace, templateContext)
		}
		outcome.StepType = start.StepType
		outcome.StartedAt = start.StartedAt
		outcome.CompletedAt = time.Now().UTC()
		outcome.DurationMS = time.Since(started).Milliseconds()
		if outcome.Status == StatusFailed || outcome.Status == StatusUnknown {
			if outcome.ErrorCode == "" {
				outcome.ErrorCode = "workflow_step_failed"
			}
			if len(outcome.Diagnostics) == 0 {
				outcome.Diagnostics = []Diagnostic{{Code: outcome.ErrorCode, StepID: step.ID, Phase: outcome.Phase, Severity: "error", Message: outcome.ErrorCode}}
			}
			for i := range outcome.Diagnostics {
				outcome.Diagnostics[i].StepID = step.ID
				if outcome.Diagnostics[i].FieldPath == "" {
					switch outcome.Phase {
					case "code":
						outcome.Diagnostics[i].FieldPath = "/code"
					case "variables":
						outcome.Diagnostics[i].FieldPath = "/assign"
					case "input":
						outcome.Diagnostics[i].FieldPath = "/input"
					}
				}
			}
		}
		result.Outcomes = append(result.Outcomes, outcome)
		statuses[step.ID] = outcome.Status
		statusValues[step.ID] = outcome.Status
		if options.OnStep != nil {
			options.OnStep(outcome)
		}
		switch outcome.Status {
		case StatusSuccess:
			result.Outputs[step.ID] = outcome.Output
			templateContext[step.ID] = outcome.Output
		case StatusFailed, StatusUnknown:
			result.FailedSteps = append(result.FailedSteps, step.ID)
			if outcome.Status == StatusUnknown {
				return result, &Error{Code: "workflow_result_unknown", Phase: outcome.Phase, Message: outcome.Error, StepID: step.ID, Status: StatusUnknown}
			}
			if step.OnError != "continue" || snapshot.SchemaVersion == 2 && (outcome.StepType != "api" || outcome.FailureClass != "api_response") {
				return result, &Error{Code: "workflow_step_failed", Phase: outcome.Phase, Message: outcome.Error, StepID: step.ID, Status: StatusFailed}
			}
			result.HasWarnings = true
		}
	}
	if err := r.Deps.AuthorizeRun(ctx, options.Principal); err != nil {
		return result, &Error{Phase: "authorization", Code: "policy_denied", Message: "run identity is no longer valid", Status: StatusFailed}
	}
	final, err := ResolveOutput(templateContext, snapshot.Output)
	if snapshot.SchemaVersion == 2 {
		final, err = ResolveV2(templateContext, snapshot.Output, snapshot.RunMetadata, true)
	}
	if err != nil {
		return result, &Error{Phase: "output", Code: "workflow_step_failed", Message: err.Error(), Status: StatusFailed}
	}
	finalObject, ok := final.(map[string]any)
	if !ok {
		return result, &Error{Phase: "output", Code: "workflow_step_failed", Message: "workflow output mapping must resolve to a JSON object", Status: StatusFailed}
	}
	encoded, marshalErr := json.Marshal(finalObject)
	if marshalErr != nil || len(encoded) > MaxFinalOutput {
		return result, &Error{Phase: "output", Code: "workflow_output_too_large", Message: "final workflow output exceeds 4 MiB", Status: StatusFailed}
	}
	result.Final = finalObject
	if result.HasWarnings {
		result.FailedSteps = compact(result.FailedSteps)
	} else {
		result.FailedSteps = []string{}
	}
	return result, nil
}

func (r *Runner) runStep(ctx context.Context, step StepSnapshot, snapshot Snapshot, options Options, templateContext map[string]any, statuses map[string]string, accumulated *int) StepOutcome {
	outcome := StepOutcome{StepID: step.ID, Title: step.Title, ActionKey: step.ActionKey, FailureClass: "local", Phase: "input", AssignmentStatus: "not_applicable"}
	if step.RunIf != nil {
		matches, err := Evaluate(templateContext, step.RunIf)
		if err != nil {
			outcome.Status = StatusFailed
			outcome.Error = err.Error()
			return outcome
		}
		if !matches {
			outcome.Status = StatusSkipped
			outcome.SkipReason = "condition_not_met"
			return outcome
		}
	}
	action, err := r.Deps.Action(ctx, step.ActionID)
	if err != nil {
		outcome.Status = StatusFailed
		outcome.Error = fmt.Sprintf("action %q could not be loaded: %v", step.ActionKey, err)
		return outcome
	}
	if action.Status != "active" || !action.Executable {
		outcome.Status = StatusFailed
		outcome.Error = fmt.Sprintf("action %q is not active and executable", step.ActionKey)
		return outcome
	}
	if action.Version != step.ActionVersion {
		outcome.Status = StatusFailed
		outcome.Error = fmt.Sprintf("action %q changed version during the run; trigger the workflow again", step.ActionKey)
		return outcome
	}
	resolvedInput, err := Resolve(templateContext, step.Input)
	if snapshot.SchemaVersion == 2 {
		resolvedInput, err = ResolveV2(templateContext, step.Input, snapshot.RunMetadata, false)
	}
	if err != nil {
		outcome.Status = StatusFailed
		outcome.Error = err.Error()
		return outcome
	}
	input, ok := resolvedInput.(map[string]any)
	if !ok {
		input = map[string]any{}
	}
	catalogAction := ActionModel(action)
	if err := r.Catalog.ValidateInput(catalogAction, input); err != nil {
		outcome.Status = StatusFailed
		outcome.Error = err.Error()
		outcome.Diagnostics = FieldDiagnostic(err, step.ID, "input", "/input")
		return outcome
	}
	integration, err := r.Deps.Integration(ctx, step.IntegrationID)
	if err != nil || integration.Status != "ready" {
		outcome.Status = StatusFailed
		outcome.Error = fmt.Sprintf("integration for step %q is not ready", step.ID)
		return outcome
	}
	connection := model.Connection{}
	if integration.AuthFlow != "none" {
		connection, err = r.Deps.Connection(ctx, step.ConnectionID)
	}
	if err != nil || (integration.AuthFlow != "none" && (connection.Status != "active" || connection.IntegrationID != integration.ID)) {
		outcome.Status = StatusFailed
		outcome.Error = fmt.Sprintf("connection for step %q is not active on the bound integration", step.ID)
		return outcome
	}
	if action.SystemID != integration.SystemID {
		outcome.Status = StatusFailed
		outcome.Error = fmt.Sprintf("action %q belongs to another system than step %q integration", step.ActionKey, step.ID)
		return outcome
	}
	if err := r.Deps.AuthorizeStep(ctx, options.Principal, action, connection); err != nil {
		outcome.Status = StatusFailed
		outcome.FailureClass = "authorization"
		outcome.Phase = "target"
		outcome.Error = fmt.Sprintf("policy denied step %q: %v", step.ID, err)
		return outcome
	}
	var resolved executor.RequestAuth
	if integration.AuthFlow != "none" {
		resolved, err = r.Deps.ResolveAuth(ctx, connection)
	}
	if err != nil {
		outcome.Status = StatusFailed
		outcome.Error = fmt.Sprintf("credentials for step %q could not be resolved: %v", step.ID, err)
		return outcome
	}
	budget := r.stepBudget()
	if configured := executor.Budget(action.ExecutionConfig); configured < budget {
		budget = configured
	}
	stepCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	provider := model.Provider{Service: action.SystemKey, DisplayName: action.SystemKey, BaseURL: integration.BaseURL}
	effective, prepareErr := executor.PrepareInput(catalogAction.Runtime, input)
	if prepareErr != nil {
		outcome.Status = StatusFailed
		outcome.Error = "input preparation failed"
		return outcome
	}
	outcome.InputPreview = effective
	result, callErr := r.Executor.Action(stepCtx, provider, catalogAction, input, nil, resolved)
	if callErr != nil {
		// The upstream may or may not have executed; the outcome is unknown
		// and must not be retried or swallowed by onError=continue.
		outcome.Status = StatusFailed
		if executor.OutcomeUnknown(callErr) {
			outcome.Status = StatusUnknown
		}
		outcome.FailureClass = "infrastructure"
		if outcome.Status == StatusUnknown {
			outcome.FailureClass = "unknown"
		}
		outcome.Phase = "request"
		outcome.Error = fmt.Sprintf("provider request for step %q failed: %v", step.ID, callErr)
		return outcome
	}
	outcome.ProviderStatus = result.Status
	if len(result.Body) == 0 {
		outcome.ResponsePreview = json.RawMessage(`null`)
	} else if json.Valid(result.Body) {
		outcome.ResponsePreview = safejson.Marshal(json.RawMessage(result.Body), 4<<10)
	} else {
		outcome.ResponsePreview = safejson.Marshal(string(result.Body), 4<<10)
	}
	outcome.APICallStatus = "failed"
	if checkErr := executor.CheckResponse(catalogAction.Runtime, result.Status, result.Body, catalogAction.OutputSchema); checkErr != nil {
		outcome.Status = StatusFailed
		outcome.FailureClass = "api_response"
		outcome.Phase = "response"
		outcome.Error = fmt.Sprintf("step %q response check failed: %v", step.ID, checkErr)
		return outcome
	}
	outcome.APICallStatus = "success"
	*accumulated += len(result.Body)
	if *accumulated > MaxAccumulatedBody {
		outcome.Status = StatusFailed
		outcome.Phase = "output"
		outcome.ErrorCode = "workflow_output_too_large"
		outcome.Error = fmt.Sprintf("workflow accumulated responses exceed %d MiB", MaxAccumulatedBody>>20)
		return outcome
	}
	value, parseErr := parseProviderBody(result.Body)
	if parseErr != nil {
		outcome.Status = StatusFailed
		outcome.Error = parseErr.Error()
		return outcome
	}
	outcome.Status = StatusSuccess
	outcome.APICallStatus = "success"
	outcome.Output = value
	return outcome
}

// parseProviderBody decodes an upstream response: empty bodies are null and
// non-JSON bodies become plain strings that only whole-value references can
// read. Nested paths into them fail in PathLookup.
func parseProviderBody(body []byte) (any, error) {
	if len(body) == 0 {
		return nil, nil
	}
	var value any
	if err := jsonutil.Unmarshal(body, &value); err != nil {
		return string(body), nil
	}
	return value, nil
}

func compact(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

// IsUnknown reports whether an error marks an unconfirmable upstream result.
func IsUnknown(err error) bool {
	var typed *Error
	return errors.As(err, &typed) && typed.Status == StatusUnknown
}
