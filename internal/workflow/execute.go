package workflow

import (
	"apihub-go/internal/jsonutil"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func StepToSnapshot(step Step) StepSnapshot {
	return StepSnapshot{ID: step.ID, Title: step.Title, Type: step.Type, Input: step.Input, DependsOn: step.DependsOn, RunIf: step.RunIf, OnError: step.OnError, Scope: step.Scope, Assign: step.Assign, Source: step.Source, Operations: step.Operations, Branches: step.Branches, Language: step.Language, RuntimeProfile: step.RuntimeProfile, Code: step.Code, InputSchema: step.InputSchema, OutputSchema: step.OutputSchema}
}

func Activation(step StepSnapshot, values map[string]any) (bool, error) {
	for _, scope := range step.Scope {
		condition, ok := values[scope.ConditionID].(map[string]any)
		if !ok {
			return false, fmt.Errorf("condition output %q is unavailable", scope.ConditionID)
		}
		selected, ok := condition["branchId"].(string)
		if !ok {
			return false, fmt.Errorf("condition output has no branch ID")
		}
		if selected != scope.BranchID {
			return false, nil
		}
	}
	return true, nil
}

func (r *Runner) executeStep(ctx context.Context, step StepSnapshot, snapshot Snapshot, options Options, values map[string]any, statuses map[string]string, accumulated *int) StepOutcome {
	fail := func(phase, class, code, message string) StepOutcome {
		return StepOutcome{StepID: step.ID, Title: step.Title, ActionKey: step.ActionKey, Status: StatusFailed, Phase: phase, FailureClass: class, ErrorCode: code, Error: message, AssignmentStatus: "not_applicable"}
	}
	if snapshot.SchemaVersion == 2 {
		active, err := Activation(step, values)
		if err != nil {
			return fail("branch", "local", "missing_reference", err.Error())
		}
		if !active {
			return StepOutcome{StepID: step.ID, Title: step.Title, Status: StatusSkipped, SkipReason: "branch_not_selected", AssignmentStatus: "not_applicable"}
		}
		if err := r.Deps.AuthorizeRun(ctx, options.Principal); err != nil {
			return fail("target", "authorization", "policy_denied", "run identity is no longer valid")
		}
	}
	var outcome StepOutcome
	assignments := step.Assign
	assignmentPath := "/assign"
	if step.Type == "" || step.Type == "api" {
		outcome = r.runStep(ctx, step, snapshot, options, values, statuses, accumulated)
	} else {
		outcome = StepOutcome{StepID: step.ID, Title: step.Title, Status: StatusSuccess, AssignmentStatus: "not_applicable"}
		switch step.Type {
		case "condition":
			selected := ""
			for branchIndex, branch := range step.Branches {
				match := branch.Default
				if !branch.Default {
					var err error
					var trace []map[string]any
					match, trace, err = EvaluateV2Traced(values, branch.Condition, snapshot.RunMetadata, fmt.Sprintf("/branches/%d/condition", branchIndex))
					outcome.ConditionTrace = append(outcome.ConditionTrace, trace...)
					if err != nil {
						result := fail("condition", "local", "condition_invalid", err.Error())
						result.ConditionTrace = outcome.ConditionTrace
						return result
					}
				}
				if match {
					selected = branch.ID
					assignments = branch.Assign
					assignmentPath = fmt.Sprintf("/branches/%d/assign", branchIndex)
					for index := branchIndex + 1; index < len(step.Branches); index++ {
						outcome.ConditionTrace = append(outcome.ConditionTrace, map[string]any{"fieldPath": fmt.Sprintf("/branches/%d/condition", index), "status": "not_evaluated"})
					}
					break
				}
			}
			if selected == "" {
				return fail("condition", "local", "condition_invalid", "condition has no matching branch")
			}
			outcome.Output = map[string]any{"branchId": selected}
			outcome.SelectedBranchID = selected
		case "transform":
			source, err := decodeValue(step.Source)
			if err != nil {
				return fail("transform", "local", "transform_invalid", "source is invalid")
			}
			source, err = ResolveV2(values, source, snapshot.RunMetadata, false)
			if err != nil {
				return fail("input", "local", "missing_reference", err.Error())
			}
			outcome.InputPreview = source
			output, summaries, err := Transform(ctx, source, step.Operations, values, snapshot.RunMetadata)
			if err != nil {
				result := fail("transform", "local", "transform_invalid", err.Error())
				result.InputPreview = outcome.InputPreview
				result.OperationSummaries = summaries
				return result
			}
			outcome.Output = output
			outcome.OperationSummaries = summaries
		case "code":
			input, err := ResolveV2(values, step.Input, snapshot.RunMetadata, false)
			if err != nil {
				return fail("input", "local", "missing_reference", err.Error())
			}
			object, ok := input.(map[string]any)
			if !ok {
				return fail("input", "local", "code_input_invalid", "code input must be an object")
			}
			outcome.InputPreview = object
			response, err := r.CodeRunner.Execute(ctx, step, object, true)
			if err != nil {
				code := CodeErrorCode(err)
				class := "local"
				if strings.Contains(code, "runtime") || strings.Contains(code, "capacity") || strings.Contains(code, "worker") {
					class = "infrastructure"
				}
				result := fail("code", class, code, code)
				result.Diagnostics = response.Diagnostics
				result.InputPreview = object
				result.LogCount = response.LogCount
				result.LogsTruncated = response.LogsTruncated
				return result
			}
			outcome.Output = response.Output
			outcome.LogCount = response.LogCount
			outcome.LogsTruncated = response.LogsTruncated
		default:
			return fail("definition", "infrastructure", "workflow_not_supported", "unsupported node type")
		}
		raw, err := json.Marshal(outcome.Output)
		if err != nil || *accumulated+len(raw) > MaxAccumulatedBody {
			return fail("output", "local", "workflow_output_too_large", "workflow output budget exceeded")
		}
		*accumulated += len(raw)
	}
	if outcome.Status != StatusSuccess || snapshot.SchemaVersion != 2 {
		return outcome
	}
	// Candidate output is only visible to post-execution assignment preparation.
	candidate := map[string]any{}
	for key, value := range values {
		candidate[key] = value
	}
	candidate[step.ID] = outcome.Output
	statusValues := map[string]any{}
	if statuses, ok := values["status"].(map[string]any); ok {
		for id, status := range statuses {
			statusValues[id] = status
		}
	}
	statusValues[step.ID] = StatusSuccess
	candidate["status"] = statusValues
	outcome.VariableReads = variableReads(step, assignments, outcome.ConditionTrace, values)
	changes, err := PrepareAssignments(snapshot.Variables, assignments, candidate, snapshot.RunMetadata)
	if err == nil {
		vars := map[string]any{}
		if current, ok := values["vars"].(map[string]any); ok {
			for key, value := range current {
				vars[key] = value
			}
		}
		for key, value := range changes {
			vars[key] = value
		}
		candidate["vars"] = vars
		raw, encodeErr := json.Marshal(candidate)
		if encodeErr != nil || len(raw) > MaxAccumulatedBody {
			err = fmt.Errorf("workflow data exceeds 8 MiB")
		}
	}
	if err != nil {
		outcome.Status = StatusFailed
		outcome.FailureClass = "local"
		outcome.Phase = "variables"
		outcome.ErrorCode = "variable_assignment_invalid"
		outcome.Error = "variable update failed: " + err.Error()
		outcome.Diagnostics = Diagnostics(err)
		for i := range outcome.Diagnostics {
			outcome.Diagnostics[i].FieldPath = assignmentPath + strings.TrimPrefix(outcome.Diagnostics[i].FieldPath, "/assign")
		}
		outcome.AssignmentStatus = "failed"
		if len(assignments) == 0 {
			outcome.Phase = "output"
			outcome.ErrorCode = "workflow_output_too_large"
			outcome.AssignmentStatus = "not_applicable"
		}
		outcome.Output = nil
		return outcome
	}
	if len(assignments) > 0 {
		values["vars"] = candidate["vars"]
		outcome.AssignmentStatus = "committed"
		outcome.VariableChanges = changes
	}
	return outcome
}

func variableReads(step StepSnapshot, assignments []Assignment, trace []map[string]any, values map[string]any) map[string]any {
	reads := map[string]any{}
	var walk func(any)
	walk = func(value any) {
		switch item := value.(type) {
		case string:
			refs, _ := CollectReferences(item)
			for _, ref := range refs {
				if ref.Alias == "vars" {
					parts, _ := jsonutil.PathSegments(ref.Path)
					if len(parts) > 1 && !parts[1].IsIndex {
						if vars, ok := values["vars"].(map[string]any); ok {
							if v, found := vars[parts[1].Key]; found {
								reads[parts[1].Key] = v
							}
						}
					}
				}
			}
		case []any:
			for _, v := range item {
				walk(v)
			}
		case map[string]any:
			if marker, ok := item["$value"].(map[string]any); ok && len(item) == 1 {
				kind, _ := marker["kind"].(string)
				if kind == "literal" || kind == "run" {
					return
				}
				if kind == "branch" {
					id, _ := marker["conditionId"].(string)
					if branch, ok := values[id].(map[string]any); ok {
						if cases, ok := marker["cases"].(map[string]any); ok {
							key, _ := branch["branchId"].(string)
							walk(cases[key])
						}
					}
					return
				}
			}
			for _, v := range item {
				walk(v)
			}
		}
	}
	walk(step.Input)
	if len(step.Source) > 0 {
		source, _ := decodeValue(step.Source)
		walk(source)
	}
	for _, operation := range step.Operations {
		walk(operation)
	}
	for _, assignment := range assignments {
		value, _ := decodeValue(assignment.Value)
		walk(value)
	}
	for _, entry := range trace {
		if entry["status"] != "not_evaluated" {
			if paths, ok := entry["variablePaths"].([]string); ok {
				for _, path := range paths {
					walk("{{" + path + "}}")
				}
			}
			if path, ok := entry["path"].(string); ok {
				walk("{{" + path + "}}")
			}
		}
	}
	return reads
}
