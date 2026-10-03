package authn

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"apihub-go/internal/executor"
	"apihub-go/internal/rediscache"
	"apihub-go/internal/secret"
	"apihub-go/internal/testutil"
)

func TestCustomRefreshRotationFailureAndExplicitRelogin(t *testing.T) {
	address := os.Getenv("APIHUB_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("isolated Redis required")
	}
	db := testutil.Database(t)
	f := testutil.Seed(t, db)
	ctx := context.Background()
	cache, err := rediscache.Open(ctx, address, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	codec, _ := secret.New(bytes.Repeat([]byte{9}, 32))
	service := New(db, codec, cache, executor.NewGuardedClient())
	mode := "valid"
	loginCalls, refreshCalls := 0, 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			loginCalls++
			_, _ = w.Write([]byte(`{"token":"login-token","ttl":3600,"refresh":"refresh-1"}`))
			return
		}
		refreshCalls++
		_ = r.ParseForm()
		if r.PostForm.Get("refresh_token") != "refresh-1" && r.PostForm.Get("refresh_token") != "refresh-2" {
			t.Error("refresh token missing")
		}
		switch mode {
		case "invalid-expiry":
			_, _ = w.Write([]byte(`{"token":"bad-new-token","ttl":"bad"}`))
		case "invalid-token":
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"code":701}`))
		default:
			_, _ = w.Write([]byte(`{"token":"refreshed-token","ttl":3600,"refresh":"refresh-2"}`))
		}
	}))
	defer upstream.Close()
	template, _ := db.AuthTemplate(ctx, "username-password-token")
	config := json.RawMessage(`{"authRequest":{"schemaVersion":2,"response":{"expiry":{"mode":"field","format":"duration_seconds"}},"refresh":{"mode":"refresh_token","refreshTokenPath":"$.refresh","request":{"schemaVersion":2,"method":"POST","bodyType":"form","credentialMode":"mapped","parameters":[{"name":"refresh_token","target":"body","value":{"source":"runtime","name":"refresh_token"}}]}},"verification":{"enabled":false}}}`)
	testutil.Exec(t, db, `UPDATE auth_instances SET auth_template_id=$2,token_url=$3,refresh_url=$4,token_path='$.token',expiry_path='$.ttl',header_name='Authorization',header_value_template='Bearer {{token}}',public_config=$5 WHERE id=$1`, f.Auth.ID, template.ID, upstream.URL+"/login", upstream.URL+"/refresh", config)
	blob, err := service.SealCredentials(f.Connection.ID, 1, map[string]any{"username": "user", "password": "password"})
	if err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, db, `UPDATE connections SET credential_blob=$2,revision=1,status='active' WHERE id=$1`, f.Connection.ID, blob)
	resolved, err := service.Resolve(ctx, f.Connection.ID)
	if err != nil || resolved.Credentials["token"] != "login-token" {
		t.Fatalf("login: %v", err)
	}
	expire := func() {
		testutil.Exec(t, db, `UPDATE connections SET token_expires_at=now()-interval '1 minute' WHERE id=$1`, f.Connection.ID)
	}
	expire()
	resolved, err = service.Resolve(ctx, f.Connection.ID)
	if err != nil || resolved.Credentials["refresh_token"] != "refresh-2" || resolved.Credentials["token"] != "refreshed-token" {
		t.Fatalf("rotation: %v %+v", err, resolved.Credentials)
	}
	before, _ := db.Connection(ctx, f.Connection.ID)
	mode = "invalid-expiry"
	expire()
	if _, err := service.Resolve(ctx, f.Connection.ID); err == nil {
		t.Fatal("invalid refresh response accepted")
	}
	after, _ := db.Connection(ctx, f.Connection.ID)
	values, _ := service.OpenCredentials(after)
	if after.Revision != before.Revision || values["token"] != "refreshed-token" {
		t.Fatal("failed refresh replaced credentials")
	}
	var public map[string]any
	_ = json.Unmarshal(config, &public)
	advanced := public["authRequest"].(map[string]any)
	refresh := advanced["refresh"].(map[string]any)
	refresh["fallbackOnInvalidRefreshToken"] = true
	refresh["invalidRefreshCondition"] = map[string]any{"path": "$.code", "operator": "equals", "value": 701}
	config, _ = json.Marshal(public)
	testutil.Exec(t, db, `UPDATE auth_instances SET public_config=$2 WHERE id=$1`, f.Auth.ID, config)
	mode = "invalid-token"
	resolved, err = service.Resolve(ctx, f.Connection.ID)
	if err != nil || resolved.Credentials["token"] != "login-token" || loginCalls != 2 || refreshCalls != 3 {
		t.Fatalf("fallback: %v login=%d refresh=%d", err, loginCalls, refreshCalls)
	}
}
