package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesSPAFallback(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/connections", nil)
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), `<div id="root"></div>`) {
		t.Fatal("SPA fallback did not serve the frontend index")
	}
}
