package workflow

import (
	"apihub-go/internal/safejson"
	"encoding/json"
	"errors"
)

func StepEvent(outcome StepOutcome, attempt int, started bool) map[string]any {
	event := "workflow.step.finished"
	if started {
		event = "workflow.step.started"
	}
	attrs := map[string]any{"schemaVersion": 1, "eventType": event, "attempt": attempt, "stepId": outcome.StepID, "stepType": outcome.StepType, "status": outcome.Status, "startedAt": outcome.StartedAt}
	if outcome.ActionKey != "" {
		attrs["actionKey"] = outcome.ActionKey
	}
	if started {
		return attrs
	}
	attrs["completedAt"] = outcome.CompletedAt
	attrs["durationMs"] = outcome.DurationMS
	attrs["assignmentStatus"] = outcome.AssignmentStatus
	if outcome.ProviderStatus != 0 {
		attrs["httpStatus"] = outcome.ProviderStatus
	}
	if len(outcome.ResponsePreview) > 0 {
		attrs["responsePreview"] = outcome.ResponsePreview
	}
	if outcome.SkipReason != "" {
		attrs["skipReason"] = outcome.SkipReason
	}
	if outcome.SelectedBranchID != "" {
		attrs["selectedBranchId"] = outcome.SelectedBranchID
	}
	if outcome.FailureClass != "" && outcome.Status == StatusFailed {
		attrs["failureClass"] = outcome.FailureClass
		attrs["phase"] = outcome.Phase
		attrs["errorCode"] = outcome.ErrorCode
	}
	if len(outcome.Diagnostics) > 0 {
		attrs["diagnostics"] = outcome.Diagnostics
	}
	if outcome.Error != "" {
		attrs["error"] = outcome.Error
	}
	if outcome.APICallStatus != "" {
		attrs["apiCallStatus"] = outcome.APICallStatus
	}
	preview := map[string]any{}
	if outcome.InputPreview != nil {
		preview["inputPreview"] = outcome.InputPreview
	}
	if outcome.Status == StatusSuccess {
		preview["outputPreview"] = outcome.Output
	}
	if len(outcome.VariableReads) > 0 {
		preview["variableReadsPreview"] = outcome.VariableReads
	}
	if len(outcome.ConditionTrace) > 0 {
		attrs["conditionTrace"] = json.RawMessage(safejson.Marshal(outcome.ConditionTrace, 4<<10))
	}
	if len(outcome.VariableChanges) > 0 {
		preview["variableChangesPreview"] = outcome.VariableChanges
	}
	if len(outcome.OperationSummaries) > 0 {
		attrs["operationSummaries"] = outcome.OperationSummaries
	}
	if outcome.StepType == "code" {
		attrs["logCount"] = outcome.LogCount
		attrs["logsTruncated"] = outcome.LogsTruncated
		attrs["preview"] = json.RawMessage(safejson.Marshal(preview, 4<<10))
	} else {
		for key, value := range preview {
			attrs[key] = json.RawMessage(safejson.Marshal(value, 4<<10))
		}
	}
	return attrs
}

// RunEvent records machine state for start/end cards, including failures before a step starts.
func RunEvent(result RunResult, err error) map[string]any {
	attrs := map[string]any{"schemaVersion": 1, "eventType": "workflow.run.finished", "status": "success", "phase": "output", "hasWarnings": result.HasWarnings}
	stepStatuses := map[string]string{}
	for _, outcome := range result.Outcomes {
		stepStatuses[outcome.StepID] = outcome.Status
	}
	attrs["stepStatuses"] = stepStatuses
	if err != nil {
		var typed *Error
		attrs["status"] = "failed"
		attrs["phase"] = "unavailable"
		if errors.As(err, &typed) {
			attrs["phase"] = typed.Phase
			attrs["status"] = typed.Status
			attrs["errorCode"] = typed.Code
			if typed.StepID != "" {
				attrs["stepId"] = typed.StepID
			}
		}
	}
	return attrs
}
