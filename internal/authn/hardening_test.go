package authn

import (
	"apihub-go/internal/rediscache"
	"apihub-go/internal/secret"
	"apihub-go/internal/testutil"
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTemplateSubstitutionIsLiteral(t *testing.T) {
	value, err := renderTemplate("Bearer {{token}} {{token}}", map[string]any{"token": "{{token}}"})
	if err != nil || value != "Bearer {{token}} {{token}}" {
		t.Fatalf("%s %v", value, err)
	}
	if _, err := renderTemplate("{{missing}}", map[string]any{}); err == nil {
		t.Fatal("missing credential accepted")
	}
}
func TestConcurrentRefreshAndTransientRecovery(t *testing.T) {
	db := testutil.Database(t)
	f := testutil.Seed(t, db)
	address := os.Getenv("APIHUB_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("isolated Redis required")
	}
	ctx := context.Background()
	cache, err := rediscache.Open(ctx, address, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	codec, _ := secret.New(bytes.Repeat([]byte{1}, 32))
	var calls atomic.Int32
	var fail atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.Sleep(350 * time.Millisecond)
		if fail.Load() {
			w.WriteHeader(503)
			w.Write([]byte(`{"error":"temporarily_unavailable"}`))
			return
		}
		w.Write([]byte(`{"access_token":"fresh","refresh_token":"rotated","expires_in":3600}`))
	}))
	defer upstream.Close()
	template, err := db.AuthTemplate(ctx, "oauth2")
	if err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, db, `UPDATE auth_instances SET auth_template_id=$2,token_url=$3,token_path='$.access_token',expiry_path='$.expires_in' WHERE id=$1`, f.Auth.ID, template.ID, upstream.URL)
	service := New(db, codec, cache, upstream.Client())
	seal := func() {
		blob, err := service.SealCredentials(f.Connection.ID, 1, map[string]any{"access_token": "old", "refresh_token": "refresh"})
		if err != nil {
			t.Fatal(err)
		}
		testutil.Exec(t, db, `UPDATE connections SET credential_blob=$2,revision=1,status='active',token_expires_at=now()-interval '1 minute' WHERE id=$1`, f.Connection.ID, blob)
	}
	seal()
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resolved, err := service.Resolve(ctx, f.Connection.ID)
			if err == nil && resolved.Credentials["access_token"] != "fresh" {
				err = errors.New("stale token")
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("concurrent refresh called upstream %d times", calls.Load())
	}
	seal()
	fail.Store(true)
	if _, err := service.Resolve(ctx, f.Connection.ID); err == nil {
		t.Fatal("expected transient failure")
	}
	connection, _ := db.Connection(ctx, f.Connection.ID)
	if connection.Status != "active" {
		t.Fatal("transient failure disabled connection")
	}
	fail.Store(false)
	if _, err := service.Resolve(ctx, f.Connection.ID); err != nil {
		t.Fatalf("retry did not recover: %v", err)
	}
}
func TestStaticVerificationRequiresProviderProbe(t *testing.T) {
	db := testutil.Database(t)
	f := testutil.Seed(t, db)
	codec, _ := secret.New(bytes.Repeat([]byte{1}, 32))
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer valid" {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	service := New(db, codec, nil, server.Client())
	blob, _ := service.SealCredentials(f.Connection.ID, 1, map[string]any{"token": "valid", "apiKey": "valid"})
	testutil.Exec(t, db, `UPDATE connections SET credential_blob=$2 WHERE id=$1`, f.Connection.ID, blob)
	connection, err := service.Verify(context.Background(), f.Connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if connection.LastVerifiedAt != nil || calls.Load() != 0 {
		t.Fatal("configured credentials incorrectly verified")
	}
	testutil.Exec(t, db, `UPDATE integrations SET base_url=$2 WHERE id=$1`, f.Integration.ID, server.URL)
	testutil.Exec(t, db, `UPDATE auth_instances SET public_config='{"verificationPath":"/me"}' WHERE id=$1`, f.Auth.ID)
	connection, err = service.Verify(context.Background(), f.Connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if connection.LastVerifiedAt == nil || calls.Load() != 1 {
		t.Fatal("probe did not verify connection")
	}
}
