// Package workflow implements multi-action orchestration: step chains and
// DAGs over registered actions with restricted conditional branching and
// template input mapping. It contains no storage or HTTP wiring; callers
// adapt it through the Dependencies interface.
package workflow

import (
	"errors"
	"fmt"

	"apihub-go/internal/jsonutil"
	"apihub-go/internal/model"
)

// MaxSteps bounds the size of one workflow graph.
const MaxSteps = 20

// MaxGraphBytes / MaxInputBytes bound definition payloads so every encrypted
// run snapshot stays cheap to store per run.
const (
	MaxGraphBytes  = 256 << 10
	MaxInputBytes  = 64 << 10
	MaxStepOnRunIf = 8
)

// Step is one node of the graph as authored in the definition JSON.
type Step struct {
	ID            string         `json:"id"`
	Title         string         `json:"title,omitempty"`
	Action        string         `json:"action"`
	IntegrationID string         `json:"integrationId"`
	ConnectionKey string         `json:"connectionKey"`
	Input         map[string]any `json:"input,omitempty"`
	DependsOn     []string       `json:"dependsOn,omitempty"`
	RunIf         *Condition     `json:"runIf,omitempty"`
	OnError       string         `json:"onError,omitempty"`
}

// Definition is the parsed workflows.graph document.
type Definition struct {
	Steps  []Step         `json:"steps"`
	Output map[string]any `json:"output"`
}

// ParseDefinition decodes and structurally validates a graph document.
func ParseDefinition(data []byte) (Definition, error) {
	if len(data) == 0 {
		return Definition{Steps: []Step{}, Output: map[string]any{}}, nil
	}
	var definition Definition
	if err := jsonutil.Unmarshal(data, &definition); err != nil {
		return Definition{}, fmt.Errorf("graph is not valid JSON: %w", err)
	}
	if definition.Steps == nil {
		definition.Steps = []Step{}
	}
	if definition.Output == nil {
		definition.Output = map[string]any{}
	}
	return definition, nil
}

// StepSnapshot pins one step to concrete action, integration and connection
// IDs at trigger time; later edits cannot change an accepted run.
type StepSnapshot struct {
	ID            string         `json:"id"`
	Title         string         `json:"title,omitempty"`
	ActionKey     string         `json:"actionKey"`
	ActionID      string         `json:"actionId"`
	ActionVersion int64          `json:"actionVersion"`
	IntegrationID string         `json:"integrationId"`
	ConnectionID  string         `json:"connectionId"`
	Input         map[string]any `json:"input,omitempty"`
	DependsOn     []string       `json:"dependsOn,omitempty"`
	RunIf         *Condition     `json:"runIf,omitempty"`
	OnError       string         `json:"onError,omitempty"`
}

// Snapshot is the immutable definition of one run, stored encrypted.
type Snapshot struct {
	WorkflowID      string         `json:"workflowId"`
	WorkflowVersion int64          `json:"workflowVersion"`
	Trigger         map[string]any `json:"trigger"`
	Steps           []StepSnapshot `json:"steps"`
	Output          map[string]any `json:"output"`
}

// StepStatus values exposed through the read-only status table.
const (
	StatusSuccess = "success"
	StatusFailed  = "failed"
	StatusSkipped = "skipped"
	StatusUnknown = "unknown"
)

// ErrStepFailed marks a run whose failure is confirmed (provider answered
// with a non-2xx or validation failed before dispatch).
var ErrStepFailed = errors.New("workflow step failed")

// ActionModel converts a stored action definition into the catalog action
// shape used by validation and the executor. It is shared by the admin test
// path, sync jobs and workflow steps so all paths behave identically.
func ActionModel(item model.ActionDefinition) model.Action {
	input, output := map[string]any{}, map[string]any{}
	_ = jsonutil.Unmarshal(item.InputSchema, &input)
	_ = jsonutil.Unmarshal(item.OutputSchema, &output)
	return model.Action{
		ID: item.ActionKey, Service: item.SystemKey, Name: item.Name,
		Description: item.Description, RequiredScopes: item.RequiredScopes,
		InputSchema: input, OutputSchema: output,
		Runtime:    &model.HTTPActionRuntime{Method: item.HTTPMethod, Path: item.RelativePath, RequestConfig: item.RequestConfig, ResponseConfig: item.ResponseConfig, ExecutionConfig: item.ExecutionConfig},
		Executable: item.Executable,
	}
}
