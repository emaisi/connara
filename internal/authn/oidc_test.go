package authn

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"apihub-go/internal/model"
	"apihub-go/internal/secret"
	"apihub-go/internal/testutil"
)

func TestOIDCDiscoveryPinsAuthorizationAndTokenEndpoints(t *testing.T) {
	var issuer string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, request)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys"})
	}))
	defer server.Close()
	issuer = server.URL
	service := New(nil, nil, nil, server.Client())
	instance := model.AuthInstance{TokenURL: issuer + "/token", PublicConfig: json.RawMessage(`{"issuer":"` + issuer + `","authorizationUrl":"` + issuer + `/authorize"}`)}
	if err := service.VerifyOIDCProvider(context.Background(), instance); err != nil {
		t.Fatal(err)
	}
	instance.TokenURL = issuer + "/different"
	if err := service.VerifyOIDCProvider(context.Background(), instance); err == nil {
		t.Fatal("mismatched token endpoint accepted")
	}
	instance.TokenURL = issuer + "/token"
	instance.PublicConfig = json.RawMessage(`{"issuer":"` + issuer + `","authorizationUrl":"` + issuer + `/wrong"}`)
	if err := service.VerifyOIDCProvider(context.Background(), instance); err == nil {
		t.Fatal("mismatched authorization endpoint accepted")
	}
}

func TestOIDCIDTokenClaimsAndSignature(t *testing.T) {
	ctx := context.Background()
	db := testutil.Database(t)
	fixture := testutil.Seed(t, db)
	codec, err := secret.New(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	key, privatePEM := testRSAPEM(t)
	var issuer string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]string{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys"})
		case "/keys":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]string{"kty": "RSA", "use": "sig", "kid": "key-1", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	issuer = server.URL
	template, err := db.AuthTemplate(ctx, "oidc")
	if err != nil {
		t.Fatal(err)
	}
	secretJSON, _ := json.Marshal(map[string]string{"clientId": "client-1"})
	secretBlob, err := codec.Encrypt(secretJSON, []byte(db.WorkspaceID()+":auth-instance:"+fixture.Auth.ID))
	if err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(map[string]string{"issuer": issuer, "authorizationUrl": issuer + "/authorize"})
	testutil.Exec(t, db, `UPDATE auth_instances SET auth_template_id=$2,token_url=$3,public_config=$4,secret_blob=$5 WHERE id=$1`, fixture.Auth.ID, template.ID, issuer+"/token", config, secretBlob)
	instance, err := db.AuthInstance(ctx, fixture.Auth.ID)
	if err != nil {
		t.Fatal(err)
	}
	service := New(db, codec, nil, server.Client())
	now := time.Now().UTC()
	claims := map[string]any{"iss": issuer, "sub": "user-1", "aud": "client-1", "nonce": "nonce-1", "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix()}
	verify := func(claims map[string]any, nonce string) error {
		token, err := signJWT(privatePEM, claims, "key-1")
		if err != nil {
			t.Fatal(err)
		}
		return service.VerifyOIDCTokens(ctx, instance, OAuthTokens{Credentials: map[string]any{"id_token": token}}, nonce)
	}
	if err := verify(claims, "nonce-1"); err != nil {
		t.Fatal(err)
	}
	if err := verify(claims, "wrong"); err == nil {
		t.Fatal("wrong nonce accepted")
	}
	for name, change := range map[string]func(map[string]any){
		"audience":         func(c map[string]any) { c["aud"] = "other-client" },
		"issuer":           func(c map[string]any) { c["iss"] = "https://other.example.com" },
		"expiry":           func(c map[string]any) { c["exp"] = now.Add(-time.Hour).Unix() },
		"authorized party": func(c map[string]any) { c["aud"] = []string{"client-1", "other-client"}; c["azp"] = "other-client" },
	} {
		t.Run(name, func(t *testing.T) {
			modified := make(map[string]any, len(claims)+1)
			for key, value := range claims {
				modified[key] = value
			}
			change(modified)
			if err := verify(modified, "nonce-1"); err == nil {
				t.Fatal("invalid ID token accepted")
			}
		})
	}
}
