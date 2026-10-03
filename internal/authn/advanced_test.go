package authn

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"apihub-go/internal/executor"
	"apihub-go/internal/model"
)

func testRSAPEM(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: mustPKCS8(t, key)}))
}

func mustPKCS8(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	data, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSignedAssertionHasValidSignatureAndBoundedClaims(t *testing.T) {
	key, privatePEM := testRSAPEM(t)
	assertion, err := signedAssertion(map[string]any{"private_key": privatePEM}, advancedConfig{Issuer: "service@example.com", Subject: "user@example.com", KeyID: "key-1"}, "https://token.example.com")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(assertion, ".")
	if len(parts) != 3 {
		t.Fatal("JWT must have three segments")
	}
	message := parts[0] + "." + parts[1]
	digest := sha256.Sum256([]byte(message))
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], signature); err != nil {
		t.Fatal(err)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatal(err)
	}
	if claims["iss"] != "service@example.com" || claims["sub"] != "user@example.com" || claims["aud"] != "https://token.example.com" {
		t.Fatalf("incorrect claims: %v", claims)
	}
	if claims["exp"].(float64)-claims["iat"].(float64) > 360 {
		t.Fatal("assertion lifetime is too long")
	}
}

func TestOAuthClientAuthenticationUsesOneMethod(t *testing.T) {
	_, privatePEM := testRSAPEM(t)
	values := map[string]any{"clientId": "client:name", "clientSecret": "p@ss:word", "oauth_private_key": privatePEM}
	for _, method := range []string{"client_secret_post", "client_secret_basic", "private_key_jwt"} {
		instance := model.AuthInstance{TokenURL: "https://id.example.com/token", PublicConfig: json.RawMessage(`{"clientAuthMethod":"` + method + `"}`)}
		form := url.Values{"grant_type": {"client_credentials"}}
		header, err := oauthClientAuth(instance, values, form, instance.TokenURL)
		if err != nil {
			t.Fatal(err)
		}
		switch method {
		case "client_secret_post":
			if header != "" || form.Get("client_secret") != "p@ss:word" || form.Get("client_id") != "client:name" {
				t.Fatalf("incorrect post auth: %v %q", form, header)
			}
		case "client_secret_basic":
			if !strings.HasPrefix(header, "Basic ") || form.Has("client_secret") || form.Has("client_id") {
				t.Fatalf("incorrect basic auth: %v %q", form, header)
			}
		case "private_key_jwt":
			if header != "" || form.Get("client_assertion") == "" || form.Has("client_secret") {
				t.Fatalf("incorrect JWT auth: %v %q", form, header)
			}
		}
	}
}

func TestSigV4SignsEachFinalRequestAndRejectsExpiredCredentials(t *testing.T) {
	values := map[string]any{"access_key_id": "AKIDEXAMPLE", "secret_access_key": "secret", "session_token": "session"}
	sign, err := awsSigner(values, advancedConfig{Region: "us-east-1", Service: "execute-api"})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := http.NewRequest(http.MethodPost, "https://api.example.com/items?page=2", strings.NewReader(`{"a":1}`))
	first.Header.Set("Content-Type", "application/json")
	second, _ := http.NewRequest(http.MethodPost, "https://api.example.com/items?page=2", strings.NewReader(`{"a":2}`))
	second.Header.Set("Content-Type", "application/json")
	for _, request := range []*http.Request{first, second} {
		if err := sign(context.Background(), request); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(request.Header.Get("Authorization"), "Credential=AKIDEXAMPLE/") || request.Header.Get("X-Amz-Security-Token") != "session" {
			t.Fatalf("missing SigV4 fields: %v", request.Header)
		}
	}
	if first.Header.Get("Authorization") == second.Header.Get("Authorization") {
		t.Fatal("body change must change signature")
	}
	values["expires_at"] = time.Now().Add(-time.Second).Format(time.RFC3339)
	sign, err = awsSigner(values, advancedConfig{Region: "us-east-1", Service: "execute-api"})
	if err != nil {
		t.Fatal(err)
	}
	if err := sign(context.Background(), first); err == nil {
		t.Fatal("expired AWS credentials must fail")
	}
}

func TestClientTLSRejectsWrongKeyAndExpiredCertificate(t *testing.T) {
	key, privatePEM := testRSAPEM(t)
	other, otherPEM := testRSAPEM(t)
	_ = other
	createCertificate := func(notAfter time.Time) string {
		der, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: notAfter, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}, &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: notAfter}, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	}
	valid := createCertificate(time.Now().Add(time.Hour))
	if _, err := clientTLS(map[string]any{"certificate": valid, "private_key": privatePEM}, advancedConfig{}); err != nil {
		t.Fatal(err)
	}
	if _, err := clientTLS(map[string]any{"certificate": valid, "private_key": otherPEM}, advancedConfig{}); err == nil {
		t.Fatal("mismatched key must fail")
	}
	if _, err := clientTLS(map[string]any{"certificate": createCertificate(time.Now().Add(-time.Minute)), "private_key": privatePEM}, advancedConfig{}); err == nil {
		t.Fatal("expired certificate must fail")
	}
}

func TestClientTLSKeepsCertificatesIsolatedAcrossRequests(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if len(request.TLS.PeerCertificates) != 1 {
			t.Fatal("client certificate missing")
		}
		_, _ = w.Write([]byte(request.TLS.PeerCertificates[0].Subject.CommonName))
	}))
	server.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert}
	server.StartTLS()
	defer server.Close()
	roots := server.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	guarded := executor.NewGuardedClient(netip.MustParsePrefix("127.0.0.1/32"))
	action := model.Action{Runtime: &model.HTTPActionRuntime{Method: http.MethodGet, Path: "/identity"}}
	for _, commonName := range []string{"connection-a", "connection-b", "connection-a"} {
		key, privatePEM := testRSAPEM(t)
		now := time.Now()
		cert := &x509.Certificate{SerialNumber: big.NewInt(now.UnixNano()), Subject: pkix.Name{CommonName: commonName}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
		der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		certificate := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
		config, err := clientTLS(map[string]any{"certificate": certificate, "private_key": privatePEM}, advancedConfig{})
		if err != nil {
			t.Fatal(err)
		}
		config.RootCAs = roots
		result, err := executor.New(guarded).Action(context.Background(), model.Provider{BaseURL: server.URL}, action, nil, nil, executor.RequestAuth{TLS: config})
		if err != nil {
			t.Fatal(err)
		}
		if string(result.Body) != commonName {
			t.Fatalf("certificate leaked across connections: got %q, want %q", result.Body, commonName)
		}
	}
}
