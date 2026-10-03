package executor

import (
	"context"
	"errors"
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
	if target.Host != "api.example.com" || target.Path != "/v1/orders/42" {
		t.Fatalf("unexpected target: %s", target)
	}
}

func TestActionSignsFinalRequestAndStopsOnSigningFailure(t *testing.T) {
	called := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		called++
		if request.Header.Get("X-Signed-Query") != request.URL.RawQuery || request.Header.Get("X-Tenant") != "north" {
			t.Fatalf("signer did not see final request: %s %v", request.URL, request.Header)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})}
	action := model.Action{Runtime: &model.HTTPActionRuntime{Method: http.MethodGet, Path: "/items"}}
	auth := RequestAuth{Headers: map[string]string{"X-Tenant": "north"}, Query: map[string]string{"api_key": "secret"}, Sign: func(_ context.Context, request *http.Request) error {
		request.Header.Set("X-Signed-Query", request.URL.RawQuery)
		return nil
	}}
	_, err := New(client).Action(context.Background(), model.Provider{BaseURL: "https://api.example.com"}, action, map[string]any{"page": 2}, nil, auth)
	if err != nil || called != 1 {
		t.Fatalf("expected one signed request, called=%d err=%v", called, err)
	}
	auth.Sign = func(context.Context, *http.Request) error { return errors.New("bad key") }
	_, err = New(client).Action(context.Background(), model.Provider{BaseURL: "https://api.example.com"}, action, nil, nil, auth)
	if err == nil || called != 1 {
		t.Fatalf("signing failure must stop dispatch, called=%d err=%v", called, err)
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

func TestActionTraceMasksConfiguredAuthentication(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("X-Custom-Credential") != "private-value" || req.URL.Query().Get("custom-session") != "private-value" {
			t.Fatal("authentication was not sent")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"X-Upstream-Id": {"trace-1"}, "Set-Cookie": {"session=private-value"}}, Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
	})}
	result, err := New(client).Action(context.Background(), model.Provider{BaseURL: "https://example.com"}, model.Action{Runtime: &model.HTTPActionRuntime{Method: "GET", Path: "/items"}}, nil, nil, RequestAuth{Headers: map[string]string{"X-Custom-Credential": "private-value"}, Query: map[string]string{"custom-session": "private-value"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Request == nil || result.Request.Headers.Get("X-Custom-Credential") != "[REDACTED]" || strings.Contains(result.Request.URL, "private-value") {
		t.Fatalf("unsafe trace: %+v", result.Request)
	}
	if result.Header.Get("X-Upstream-Id") != "trace-1" || string(result.Body) != `{"ok":true}` {
		t.Fatal("response details missing")
	}
}
