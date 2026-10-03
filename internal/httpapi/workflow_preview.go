package httpapi

import (
	"apihub-go/internal/executor"
	"apihub-go/internal/jsonutil"
	"apihub-go/internal/safejson"
	"apihub-go/internal/workflow"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type workflowPreviewRequest struct {
	Definition        workflow.Definition `json:"definition"`
	StepID            string              `json:"stepId"`
	PreviewMode       string              `json:"previewMode"`
	Trigger           map[string]any      `json:"trigger"`
	SampleOutputs     map[string]any      `json:"sampleOutputs"`
	SampleStatuses    map[string]string   `json:"sampleStatuses"`
	SampleVariables   map[string]any      `json:"sampleVariables"`
	SampleInput       json.RawMessage     `json:"sampleInput"`
	SampleResult      json.RawMessage     `json:"sampleResult"`
	SampleRunMetadata json.RawMessage     `json:"sampleRunMetadata"`
}

func (a *api) previewWorkflowStep(w http.ResponseWriter, r *http.Request) {
	if !workflowCanReadSensitive(r) {
		writeAdminError(w, 403, "forbidden", "Your role does not permit this operation")
		return
	}
	var request workflowPreviewRequest
	if !decodeJSON(w, r, &request, false) {
		return
	}
	if err := a.Store.WorkflowFeatures().CheckEditingDefinition(request.Definition); err != nil {
		writeAdminError(w, 409, "workflow_capability_disabled", err.Error())
		return
	}
	if err := workflow.ValidateDefinition(request.Definition, false, nil); err != nil {
		writeWorkflowError(w, 400, "invalid_graph", err)
		return
	}
	sampleBytes, _ := json.Marshal(map[string]any{"trigger": request.Trigger, "outputs": request.SampleOutputs, "statuses": request.SampleStatuses, "variables": request.SampleVariables, "input": request.SampleInput, "result": request.SampleResult, "runMetadata": request.SampleRunMetadata})
	if len(sampleBytes) > 128<<10 {
		writeAdminError(w, 400, "sample_too_large", "samples exceed 128 KiB")
		return
	}
	var step workflow.Step
	found := false
	for _, candidate := range request.Definition.Steps {
		if candidate.ID == request.StepID {
			step = candidate
			found = true
			break
		}
	}
	if !found {
		writeAdminError(w, 400, "step_not_found", "step is not in definition")
		return
	}
	if step.Type == "code" && !a.Store.WorkflowFeatures().CodeEnabled {
		writeAdminError(w, 409, "workflow_capability_disabled", "code preview is disabled")
		return
	}
	response := map[string]any{"dataSource": "sample", "stepId": step.ID, "validationScope": "workflow_sample", "assignmentPreviewStatus": "not_applicable"}
	unavailable := func(message string) {
		response["status"] = "unavailable"
		response["message"] = message
		writeJSON(w, 200, response)
	}
	if request.PreviewMode == "" {
		request.PreviewMode = "workflow"
	}
	if request.PreviewMode != "workflow" && request.PreviewMode != "code_inputs" {
		writeAdminError(w, 400, "invalid_preview_mode", "unknown preview mode")
		return
	}
	codeOnly := request.PreviewMode == "code_inputs"
	if codeOnly && (step.Type != "code" || request.Trigger != nil || request.SampleOutputs != nil || request.SampleStatuses != nil || request.SampleVariables != nil || len(request.SampleResult) > 0 || len(request.SampleRunMetadata) > 0) || !codeOnly && len(request.SampleInput) > 0 {
		writeAdminError(w, 400, "conflicting_samples", "preview modes cannot mix sample sources")
		return
	}
	statusValues := map[string]any{}
	for id, status := range request.SampleStatuses {
		statusValues[id] = status
	}
	values := map[string]any{"trigger": request.Trigger, "status": statusValues}
	stepIDs := map[string]bool{}
	available := map[string]bool{}
	byID := map[string]workflow.Step{}
	for _, row := range request.Definition.Steps {
		byID[row.ID] = row
	}
	var ancestors func(string)
	ancestors = func(id string) {
		for _, dep := range byID[id].DependsOn {
			if !available[dep] {
				available[dep] = true
				ancestors(dep)
			}
		}
	}
	ancestors(step.ID)
	for _, row := range request.Definition.Steps {
		stepIDs[row.ID] = true
	}
	for id, status := range request.SampleStatuses {
		if !stepIDs[id] || !available[id] || id == step.ID || status != "success" && status != "failed" && status != "skipped" && status != "unknown" {
			writeAdminError(w, 400, "invalid_sample_status", "sample status must identify another declared step")
			return
		}
	}
	for key, value := range request.SampleOutputs {
		if !stepIDs[key] || !available[key] || key == step.ID || key == "trigger" || key == "status" || request.Definition.SchemaVersion == 2 && key == "vars" {
			writeAdminError(w, 400, "invalid_sample_output", "sample output must identify another declared step")
			return
		}
		status, ok := request.SampleStatuses[key]
		if ok && status != "success" {
			writeAdminError(w, 400, "sample_status_conflict", "only successful samples can have output")
			return
		}
		if node := byID[key]; node.Type == "condition" {
			output, ok := value.(map[string]any)
			branchID, _ := output["branchId"].(string)
			valid := false
			for _, branch := range node.Branches {
				valid = valid || branch.ID == branchID
			}
			if !ok || !valid {
				writeAdminError(w, 400, "invalid_branch_sample", "condition samples require a declared branchId")
				return
			}
		}
		values[key] = value
	}
	if request.Definition.SchemaVersion == 2 {
		for id, status := range request.SampleStatuses {
			if len(byID[id].Scope) > 0 {
				active, err := workflow.Activation(workflow.StepToSnapshot(byID[id]), values)
				if err == nil && !active && status != "skipped" {
					writeAdminError(w, 400, "sample_scope_conflict", "sample status conflicts with selected branch")
					return
				}
			}
		}
	}
	if request.SampleVariables != nil {
		values["vars"] = request.SampleVariables
	}
	var metadata *workflow.RunMetadata
	if len(request.SampleRunMetadata) > 0 {
		var fields map[string]json.RawMessage
		if jsonutil.Unmarshal(request.SampleRunMetadata, &fields) != nil || len(fields) != 3 || fields["triggeredAt"] == nil || fields["scheduledFor"] == nil || fields["timezone"] == nil {
			writeAdminError(w, 400, "invalid_run_sample", "run sample requires triggeredAt, scheduledFor and timezone")
			return
		}
		var times struct {
			TriggeredAt  time.Time  `json:"triggeredAt"`
			ScheduledFor *time.Time `json:"scheduledFor"`
			Timezone     string     `json:"timezone"`
		}
		if jsonutil.Unmarshal(request.SampleRunMetadata, &times) != nil || times.TriggeredAt.Location() != time.UTC || times.ScheduledFor != nil && times.ScheduledFor.Location() != time.UTC {
			writeAdminError(w, 400, "invalid_run_sample", "sample times must be UTC RFC3339")
			return
		}
		var err error
		metadata, err = workflow.NewRunMetadata(times.TriggeredAt, times.ScheduledFor, times.Timezone)
		if err != nil {
			writeAdminError(w, 400, "invalid_run_sample", "invalid run sample")
			return
		}
		response["runMetadataPreview"] = metadata
	}
	pinned := workflow.StepToSnapshot(step)
	if !codeOnly {
		if request.Definition.SchemaVersion == 2 {
			active, err := workflow.Activation(pinned, values)
			if err != nil {
				unavailable(err.Error())
				return
			}
			response["activationEvaluation"] = active
			if !active {
				response["status"] = "skipped"
				response["skipReason"] = "branch_not_selected"
				writeJSON(w, 200, response)
				return
			}
		}
		if step.RunIf != nil {
			match, err := workflow.Evaluate(values, step.RunIf)
			if err != nil {
				unavailable(err.Error())
				return
			}
			response["conditionEvaluation"] = match
			if !match {
				response["status"] = "skipped"
				writeJSON(w, 200, response)
				return
			}
		}
	} else {
		response["validationScope"] = "code_only"
		response["mappingPreviewStatus"] = "not_evaluated"
	}
	resolve := func(value any) (any, error) {
		if request.Definition.SchemaVersion == 2 {
			return workflow.ResolveV2(values, value, metadata, false)
		}
		return workflow.Resolve(values, value)
	}
	a.previewMu.Lock()
	if a.previewActive || time.Now().Before(a.previewNext) {
		a.previewMu.Unlock()
		w.Header().Set("Retry-After", "1")
		writeAdminError(w, 429, "workflow_preview_busy", "sample preview is busy; retry when ready")
		return
	}
	a.previewActive = true
	a.previewNext = time.Now().Add(200 * time.Millisecond)
	a.previewMu.Unlock()
	defer func() { a.previewMu.Lock(); a.previewActive = false; a.previewMu.Unlock() }()
	var output any
	assignments := step.Assign
	switch step.Type {
	case "", "api":
		input, err := resolve(step.Input)
		if err != nil {
			unavailable(err.Error())
			return
		}
		object, _ := input.(map[string]any)
		action, err := a.Store.Action(r.Context(), step.Action)
		if err != nil {
			unavailable("API is unavailable")
			return
		}
		integration, _, _, err := a.actionTarget(r.Context(), action, actionRequest{IntegrationID: step.IntegrationID, ConnectionKey: step.ConnectionKey}, false)
		if err != nil {
			unavailable("execution binding is unavailable")
			return
		}
		built, err := executor.BuildRequest(integration.BaseURL, workflow.ActionModel(action).Runtime, object)
		if err != nil {
			unavailable(err.Error())
			return
		}
		if err := a.Catalog.ValidateInput(workflow.ActionModel(action), object); err != nil {
			unavailable(err.Error())
			return
		}
		response["request"] = executor.RequestPreview(built)
		if len(request.SampleResult) > 0 {
			if err := jsonutil.Unmarshal(request.SampleResult, &output); err != nil {
				writeAdminError(w, 400, "invalid_sample", "invalid API sampleResult")
				return
			}
		} else if len(assignments) > 0 {
			response["assignmentPreviewStatus"] = "unavailable"
		}
	case "transform":
		var source any
		if jsonutil.Unmarshal(step.Source, &source) != nil {
			unavailable("source is invalid")
			return
		}
		source, err := resolve(source)
		if err != nil {
			unavailable(err.Error())
			return
		}
		result, summaries, err := workflow.Transform(r.Context(), source, step.Operations, values, metadata)
		if err != nil {
			unavailable(err.Error())
			return
		}
		output = result
		response["operationSummaries"] = summaries
	case "condition":
		for index, branch := range step.Branches {
			match := branch.Default
			if !match {
				var err error
				var trace []map[string]any
				match, trace, err = workflow.EvaluateV2Traced(values, branch.Condition, metadata, fmt.Sprintf("/branches/%d/condition", index))
				previous, _ := response["conditionTrace"].([]map[string]any)
				response["conditionTrace"] = append(previous, trace...)
				if err != nil {
					unavailable(err.Error())
					return
				}
			}
			if match {
				output = map[string]any{"branchId": branch.ID}
				response["selectedBranchId"] = branch.ID
				assignments = branch.Assign
				break
			}
		}
	case "code":
		var input any
		var err error
		if codeOnly {
			err = jsonutil.Unmarshal(request.SampleInput, &input)
		} else {
			input, err = resolve(step.Input)
		}
		if err != nil {
			unavailable("code input sample is unavailable")
			return
		}
		object, ok := input.(map[string]any)
		if !ok {
			unavailable("sampleInput must be an object")
			return
		}
		result, err := a.Store.WorkflowCodeRunner().Execute(r.Context(), pinned, object, false)
		if err != nil {
			response["status"] = "failed"
			response["errorCode"] = workflow.CodeErrorCode(err)
			response["diagnostics"] = result.Diagnostics
			writeJSON(w, 200, response)
			return
		}
		output = result.Output
		response["durationMs"] = result.DurationMS
		response["logCount"] = result.LogCount
		response["logsTruncated"] = result.LogsTruncated
	}
	if output != nil || step.Type == "api" && len(request.SampleResult) > 0 {
		response["outputPreview"] = json.RawMessage(safejson.Marshal(output, 4<<10))
		values[step.ID] = output
		statusValues[step.ID] = workflow.StatusSuccess
	}
	if !codeOnly && len(assignments) > 0 && response["assignmentPreviewStatus"] != "unavailable" {
		if request.SampleVariables == nil {
			response["assignmentPreviewStatus"] = "unavailable"
		} else {
			changes, err := workflow.PrepareAssignments(request.Definition.Variables, assignments, values, metadata)
			if err != nil {
				response["assignmentPreviewStatus"] = "failed"
				response["message"] = err.Error()
			} else {
				response["assignmentPreviewStatus"] = "sample"
				response["variableChangesPreview"] = json.RawMessage(safejson.Marshal(changes, 4<<10))
			}
		}
	}
	response["status"] = "success"
	writeJSON(w, 200, response)
}
