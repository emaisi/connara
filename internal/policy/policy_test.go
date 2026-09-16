package policy

import (
	"testing"

	"apihub-go/internal/model"
)

func TestPolicyDenyWinsAndConnectionGrantMatchesStableKeys(t *testing.T) {
	token := model.RuntimeToken{
		AllowedActions:     []string{"github.*"},
		BlockedActions:     []string{"github.delete_*"},
		AllowedConnections: []string{"github-main:default"},
	}
	if !AllowsAction(token, "github.list_repositories") {
		t.Fatal("expected list action to be allowed")
	}
	if AllowsAction(token, "github.delete_repository") {
		t.Fatal("blocked action must win")
	}
	connection := model.Connection{IntegrationKey: "github-main", Name: "default"}
	if !AllowsConnection(token, connection) {
		t.Fatal("integration and connection name grant should match")
	}
	if AllowsConnection(token, model.Connection{IntegrationKey: "slack-main", Name: "default"}) {
		t.Fatal("ungranted connection should be denied")
	}
}

func TestEmptyActionPolicyDeniesByDefault(t *testing.T) {
	if AllowsAction(model.RuntimeToken{}, "github.list_repositories") {
		t.Fatal("a token without allow rules must not grant every action")
	}
}
