package executor

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"apihub-go/internal/model"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func TestJoinTargetKeepsConfiguredHost(t *testing.T) {
	target, err := joinTarget("https://api.example.com/v1/", "/orders/42")
	if err != nil {
		t.Fatal(err)
	}
	if target.Host != "api.example.com" || target.Path != "/orders/42" {
		t.Fatalf("unexpected target: %s", target)
	}
}

func TestActionAppliesDeclaredAuthenticationRules(t *testing.T) {
	var captured *http.Request
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		captured = request.Clone(request.Context())
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		}, nil
	})}
	exec := New(client)
	_, err := exec.Action(
		context.Background(),
		model.Provider{BaseURL: "https://api.example.com"},
		model.Action{Runtime: &model.HTTPActionRuntime{Method: http.MethodGet, Path: "/items"}},
		map[string]any{"page": 2},
		nil,
		RequestAuth{
			Headers: map[string]string{"Authorization": "Bearer secret", "X-Tenant-ID": "north"},
			Query:   map[string]string{"api_key": "key-1"},
			Cookies: map[string]string{"session": "cookie-1"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if captured.Header.Get("Authorization") != "Bearer secret" || captured.Header.Get("X-Tenant-ID") != "north" {
		t.Fatalf("authentication headers were not applied: %v", captured.Header)
	}
	if captured.URL.Query().Get("api_key") != "key-1" || captured.URL.Query().Get("page") != "2" {
		t.Fatalf("authentication or action query was not applied: %s", captured.URL.RawQuery)
	}
	cookie, err := captured.Cookie("session")
	if err != nil || cookie.Value != "cookie-1" {
		t.Fatalf("authentication cookie was not applied: %v", err)
	}
}
