// Package workflow implements multi-action orchestration: step chains and
// DAGs over registered actions with restricted conditional branching and
// template input mapping. It contains no storage or HTTP wiring; callers
// adapt it through the Dependencies interface.
package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

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
	ID             string           `json:"id"`
	Type           string           `json:"type,omitempty"`
	Title          string           `json:"title,omitempty"`
	Action         string           `json:"action,omitempty"`
	IntegrationID  string           `json:"integrationId,omitempty"`
	ConnectionKey  string           `json:"connectionKey,omitempty"`
	Input          map[string]any   `json:"input,omitempty"`
	DependsOn      []string         `json:"dependsOn,omitempty"`
	RunIf          *Condition       `json:"runIf,omitempty"`
	OnError        string           `json:"onError,omitempty"`
	Scope          []BranchScope    `json:"scope,omitempty"`
	Assign         []Assignment     `json:"assign,omitempty"`
	Source         json.RawMessage  `json:"source,omitempty"`
	Operations     []map[string]any `json:"operations,omitempty"`
	Branches       []Branch         `json:"branches,omitempty"`
	Language       string           `json:"language,omitempty"`
	RuntimeProfile string           `json:"runtimeProfile,omitempty"`
	Code           string           `json:"code,omitempty"`
	InputSchema    map[string]any   `json:"inputSchema,omitempty"`
	OutputSchema   map[string]any   `json:"outputSchema,omitempty"`
}

type BranchScope struct {
	ConditionID string `json:"conditionId"`
	BranchID    string `json:"branchId"`
}

type Assignment struct {
	Variable string          `json:"variable"`
	Value    json.RawMessage `json:"value"`
}

type Variable struct {
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	Description string          `json:"description,omitempty"`
	Nullable    bool            `json:"nullable,omitempty"`
	Initial     json.RawMessage `json:"initial"`
}

type Branch struct {
	ID        string       `json:"id"`
	Title     string       `json:"title,omitempty"`
	Default   bool         `json:"default,omitempty"`
	Condition *Condition   `json:"condition,omitempty"`
	Assign    []Assignment `json:"assign,omitempty"`
}

// Empty type is the legacy API spelling, never an unknown local node.
func (s Step) IsAPI() bool { return s.Type == "" || s.Type == "api" }

// Definition is the parsed workflows.graph document.
type Definition struct {
	SchemaVersion int            `json:"schemaVersion,omitempty"`
	InputSchema   map[string]any `json:"inputSchema,omitempty"`
	Variables     []Variable     `json:"variables,omitempty"`
	Steps         []Step         `json:"steps"`
	Output        map[string]any `json:"output"`
}

// ParseDefinition decodes and structurally validates a graph document.
func ParseDefinition(data []byte) (Definition, error) {
	if len(data) == 0 {
		return Definition{Steps: []Step{}, Output: map[string]any{}}, nil
	}
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		return Definition{}, fmt.Errorf("graph must be a JSON object")
	}
	var definition Definition
	if len(data) > MaxGraphBytes {
		return Definition{}, fmt.Errorf("graph exceeds 256 KiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&definition); err != nil {
		return Definition{}, fmt.Errorf("graph is not valid JSON: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Definition{}, fmt.Errorf("graph must contain exactly one JSON object")
	}
	if definition.SchemaVersion != 0 && definition.SchemaVersion != 1 && definition.SchemaVersion != 2 {
		return Definition{}, fmt.Errorf("unsupported graph schemaVersion %d", definition.SchemaVersion)
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
	ActionName      string           `json:"actionName,omitempty"`
	SystemKey       string           `json:"systemKey,omitempty"`
	HTTPMethod      string           `json:"httpMethod,omitempty"`
	RelativePath    string           `json:"relativePath,omitempty"`
	IntegrationName string           `json:"integrationName,omitempty"`
	ConnectionName  string           `json:"connectionName,omitempty"`
	ID              string           `json:"id"`
	Type            string           `json:"type,omitempty"`
	Title           string           `json:"title,omitempty"`
	ActionKey       string           `json:"actionKey"`
	ActionID        string           `json:"actionId"`
	ActionVersion   int64            `json:"actionVersion"`
	IntegrationID   string           `json:"integrationId"`
	ConnectionID    string           `json:"connectionId"`
	Input           map[string]any   `json:"input,omitempty"`
	DependsOn       []string         `json:"dependsOn,omitempty"`
	RunIf           *Condition       `json:"runIf,omitempty"`
	OnError         string           `json:"onError,omitempty"`
	Scope           []BranchScope    `json:"scope,omitempty"`
	Assign          []Assignment     `json:"assign,omitempty"`
	Source          json.RawMessage  `json:"source,omitempty"`
	Operations      []map[string]any `json:"operations,omitempty"`
	Branches        []Branch         `json:"branches,omitempty"`
	Language        string           `json:"language,omitempty"`
	RuntimeProfile  string           `json:"runtimeProfile,omitempty"`
	RunnerBuild     string           `json:"runnerBuild,omitempty"`
	Code            string           `json:"code,omitempty"`
	InputSchema     map[string]any   `json:"inputSchema,omitempty"`
	OutputSchema    map[string]any   `json:"outputSchema,omitempty"`
}

// Snapshot is the immutable definition of one run, stored encrypted.
type Snapshot struct {
	WorkflowName     string          `json:"workflowName,omitempty"`
	EditorLayout     json.RawMessage `json:"editorLayout,omitempty"`
	SchemaVersion    int             `json:"schemaVersion,omitempty"`
	InputSchema      map[string]any  `json:"inputSchema,omitempty"`
	Variables        []Variable      `json:"variables,omitempty"`
	InitialVariables map[string]any  `json:"initialVariables,omitempty"`
	RunMetadata      *RunMetadata    `json:"runMetadata,omitempty"`
	WorkflowID       string          `json:"workflowId"`
	WorkflowVersion  int64           `json:"workflowVersion"`
	Trigger          map[string]any  `json:"trigger"`
	Steps            []StepSnapshot  `json:"steps"`
	Output           map[string]any  `json:"output"`
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
