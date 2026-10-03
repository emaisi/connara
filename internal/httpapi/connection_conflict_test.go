package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestConnectionNameConflictHasActionableMessage(t *testing.T) {
	a := &api{}
	recorder := httptest.NewRecorder()
	err := fmt.Errorf("save connection: %w", &pgconn.PgError{Code: "23505", ConstraintName: "connections_integration_name_uq", Message: "duplicate key value violates unique constraint"})
	a.writeStoreError(recorder, httptest.NewRequest("POST", "/api/connections", nil), err, "save connection")
	var response struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusConflict || response.Code != "connection_name_exists" || !strings.Contains(response.Message, "更换账号名称") {
		t.Fatalf("unexpected conflict: %d %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(response.Message, "23505") || strings.Contains(response.Message, "connections_integration_name_uq") {
		t.Fatal("database internals exposed in name conflict")
	}
}
