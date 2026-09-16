package testutil

import (
	"apihub-go/internal/model"
	"apihub-go/internal/store"
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"testing"
	"time"
)

func Database(t *testing.T) *store.Store {
	t.Helper()
	url := os.Getenv("APIHUB_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("APIHUB_TEST_DATABASE_URL is not set (isolated PostgreSQL required)")
	}
	id := ID(t.Name())
	db, err := store.Open(context.Background(), url, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := db.Bootstrap(context.Background(), store.BootstrapInput{WorkspaceID: id, WorkspaceSlug: id, WorkspaceName: "test", AdminUserID: ID("admin"), AdminEmail: id + "@example.test", PublicBaseURL: "http://127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	return db
}
func ID(label string) string { return store.StableID(label, fmt.Sprintf("%x", random())) }
func random() []byte {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}
func Exec(t *testing.T, db *store.Store, query string, args ...any) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), query, args...); err != nil {
		t.Fatal(err)
	}
}

type Fixture struct {
	System      model.System
	Auth        model.AuthInstance
	Integration model.Integration
	Connection  model.Connection
	Action      model.ActionDefinition
	Task        model.SyncTask
}

func Seed(t *testing.T, db *store.Store) Fixture {
	t.Helper()
	ctx := context.Background()
	w := db.WorkspaceID()
	f := Fixture{}
	groups, err := db.ListSystemGroups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	groupID := ""
	if len(groups) > 0 {
		groupID = groups[0].ID
	} else {
		groupID = ID("group")
		Exec(t, db, `INSERT INTO system_groups(id,workspace_id,name,sort_order) VALUES($1,$2,'test',0)`, groupID, w)
	}
	template, err := db.AuthTemplate(ctx, "api_key")
	if err != nil {
		t.Fatal(err)
	}
	f.System, err = db.CreateSystem(ctx, model.System{SystemKey: "test", Name: "test", GroupID: groupID}, template.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.Auth, err = db.SaveAuthInstance(ctx, model.AuthInstance{InstanceKey: "test", Name: "test", SystemID: f.System.ID, AuthTemplateID: template.ID, Status: "ready", HeaderName: "Authorization", HeaderValueTemplate: "Bearer {{token}}", PublicConfig: []byte(`{}`), KeyVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	f.Integration, err = db.SaveIntegration(ctx, model.Integration{IntegrationKey: "test", Name: "test", SystemID: f.System.ID, AuthInstanceID: f.Auth.ID, BaseURL: "https://api.example.test", Status: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	f.Connection, err = db.SaveConnection(ctx, store.SaveConnectionInput{Connection: model.Connection{ConnectionKey: "test", Name: "test", IntegrationID: f.Integration.ID, AuthInstanceID: f.Auth.ID, Status: "active", CredentialBlob: []byte(`{}`), KeyVersion: 1, Tags: []string{}}, EndUser: model.EndUser{ExternalKey: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	f.Action, err = db.SaveAction(ctx, model.ActionDefinition{ActionKey: "test.list", Name: "list", SystemID: f.System.ID, IntegrationID: f.Integration.ID, HTTPMethod: "GET", RelativePath: "/records", Executable: true, Status: "active", InputSchema: []byte(`{"type":"object"}`), OutputSchema: []byte(`{}`), RequiredScopes: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	next := time.Now().Add(-time.Minute)
	f.Task, err = db.SaveSyncTask(ctx, model.SyncTask{TaskKey: "test", Name: "test", IntegrationID: f.Integration.ID, ConnectionID: f.Connection.ID, ActionID: f.Action.ID, Status: "deployed", ScheduleType: "interval", CronExpression: "每 15 分钟", NextRunAt: &next, Input: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	return f
}
