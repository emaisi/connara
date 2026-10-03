package httpapi

import (
	"apihub-go/internal/executor"
	"apihub-go/internal/model"
	"context"
	"github.com/go-chi/chi/v5"
	"net/http"
)

func (a *api) actionTarget(ctx context.Context, action model.ActionDefinition, request actionRequest, requireVerified bool) (model.Integration, model.Connection, bool, error) {
	var c model.Connection
	if request.IntegrationID == "" {
		return model.Integration{}, c, false, executionError{400, "integration_required", "Choose an execution integration"}
	}
	i, err := a.Store.IntegrationForSystem(ctx, action.SystemKey, request.IntegrationID)
	if err != nil {
		return i, c, false, executionError{404, "integration_not_found", "No enabled integration belongs to this API system"}
	}
	if request.ExpectedAPIVersion > 0 && request.ExpectedAPIVersion != action.Version || request.ExpectedIntegrationVersion > 0 && request.ExpectedIntegrationVersion != i.Version {
		return i, c, false, executionError{409, "configuration_changed", "Configuration changed; refresh the request preview"}
	}
	instance, err := a.Store.AuthInstance(ctx, i.AuthInstanceID)
	if err != nil {
		return i, c, false, err
	}
	noAuth := instance.AuthTemplateFlow == "none"
	if noAuth {
		return i, c, true, nil
	}
	key := request.ConnectionKey
	if key == "" {
		key = request.ConnectionName
	}
	if key == "" {
		return i, c, false, executionError{400, "account_required", "Choose an execution account"}
	}
	c, err = a.Store.Connection(ctx, key)
	if err != nil || c.IntegrationID != i.ID {
		return i, c, false, executionError{404, "connection_not_found", "Account does not belong to the selected integration"}
	}
	if !c.Enabled || c.Status == "disabled" {
		return i, c, false, executionError{409, "account_disabled", "Account is disabled"}
	}
	if request.ExpectedAccountRevision > 0 && request.ExpectedAccountRevision != c.Revision {
		return i, c, false, executionError{409, "configuration_changed", "Account changed; refresh the request preview"}
	}
	if requireVerified && (c.Status != "active" || c.LastVerifiedAt == nil || c.VerifiedTargetVersion != i.TargetVersion || c.VerifiedRevision != c.Revision) {
		return i, c, false, executionError{409, "account_verification_required", "Verify this account on the current API address before running"}
	}
	return i, c, false, nil
}
func (a *api) previewAction(w http.ResponseWriter, r *http.Request) {
	var request actionRequest
	if !decodeJSON(w, r, &request, false) {
		return
	}
	action, err := a.Store.Action(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.writeStoreError(w, r, err, "preview API")
		return
	}
	i, c, noAuth, err := a.actionTarget(r.Context(), action, request, false)
	if err != nil {
		status, code := executionErrorStatus(err)
		writeAdminError(w, status, code, err.Error())
		return
	}
	req, err := executor.BuildRequest(i.BaseURL, definitionAction(action).Runtime, request.Input)
	if err != nil {
		writeAdminError(w, 400, "invalid_input", err.Error())
		return
	}
	if err := a.Catalog.ValidateInput(definitionAction(action), request.Input); err != nil {
		writeAdminError(w, 400, "invalid_input", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"request": executor.RequestPreview(req), "apiVersion": action.Version, "integrationVersion": i.Version, "accountRevision": c.Revision, "authentication": map[string]any{"required": !noAuth, "accountName": c.Name, "instanceName": i.AuthName}, "verified": noAuth || (c.Status == "active" && c.LastVerifiedAt != nil && c.VerifiedTargetVersion == i.TargetVersion && c.VerifiedRevision == c.Revision)})
}
func (a *api) actionExecutionOptions(w http.ResponseWriter, r *http.Request) {
	action, err := a.Store.Action(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.writeStoreError(w, r, err, "execution options")
		return
	}
	integrations, err := a.Store.ListIntegrations(r.Context())
	if err != nil {
		a.writeStoreError(w, r, err, "execution options")
		return
	}
	result := []any{}
	// Fetch only accounts for matching targets, rather than a truncated global page.
	for _, i := range integrations {
		if i.SystemID != action.SystemID || i.Status != "ready" {
			continue
		}
		instance, err := a.Store.AuthInstance(r.Context(), i.AuthInstanceID)
		if err != nil || instance.Status != "ready" {
			continue
		}
		accounts, err := a.Store.ConnectionsForTarget(r.Context(), i.ID)
		if err != nil {
			a.writeStoreError(w, r, err, "execution options")
			return
		}
		result = append(result, map[string]any{"integration": i, "requiresAccount": instance.AuthTemplateFlow != "none", "accounts": accounts})
	}
	writeJSON(w, http.StatusOK, result)
}
