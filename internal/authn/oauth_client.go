package authn

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"time"

	"apihub-go/internal/executor"
	"apihub-go/internal/model"
)

func oauthClientAuth(instance model.AuthInstance, values map[string]any, form url.Values, tokenURL string) (string, error) {
	config, err := readAdvancedConfig(instance)
	if err != nil {
		return "", err
	}
	clientID := firstString(values, "clientId", "client_id")
	if clientID == "" {
		if config.ClientAuth == "" || config.ClientAuth == "client_secret_post" {
			return "", nil // Preserve existing public-client refresh configurations.
		}
		return "", errors.New("OAuth client ID is required")
	}
	switch config.ClientAuth {
	case "", "client_secret_post":
		form.Set("client_id", clientID)
		if secret := firstString(values, "clientSecret", "client_secret"); secret != "" {
			form.Set("client_secret", secret)
		}
		return "", nil
	case "client_secret_basic":
		secret := firstString(values, "clientSecret", "client_secret")
		if secret == "" {
			return "", errors.New("OAuth client secret is required")
		}
		encoded := url.QueryEscape(clientID) + ":" + url.QueryEscape(secret)
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(encoded)), nil
	case "private_key_jwt":
		privateKey := firstString(values, "oauth_private_key", "private_key")
		if privateKey == "" {
			return "", errors.New("OAuth private key is required")
		}
		jti := make([]byte, 24)
		if _, err := rand.Read(jti); err != nil {
			return "", err
		}
		now := time.Now().UTC()
		claims := map[string]any{"iss": clientID, "sub": clientID, "aud": tokenURL, "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(), "jti": base64.RawURLEncoding.EncodeToString(jti)}
		assertion, err := signJWT(privateKey, claims, config.KeyID)
		if err != nil {
			return "", err
		}
		form.Set("client_assertion_type", "urn:ietf:params:oauth:client-assertion-type:jwt-bearer")
		form.Set("client_assertion", assertion)
		return "", nil
	case "tls_client_auth":
		form.Set("client_id", clientID)
		return "", nil
	default:
		return "", errors.New("unsupported OAuth client authentication method")
	}
}

func (s *Service) tokenClient(instance model.AuthInstance, values map[string]any) (*http.Client, func(), error) {
	config, err := readAdvancedConfig(instance)
	if err != nil {
		return nil, nil, err
	}
	client := s.client
	tlsCleanup := func() {}
	if config.TLSForToken || config.ClientAuth == "tls_client_auth" {
		tlsConfig, err := clientTLS(values, config)
		if err != nil {
			return nil, nil, err
		}
		client, tlsCleanup, err = executor.ClientWithTLS(client, tlsConfig)
		if err != nil {
			return nil, nil, err
		}
	}
	scoped, cleanup, err := executor.ConfiguredEndpointClient(client, instance.TokenURL, instance.RefreshURL)
	if err != nil {
		tlsCleanup()
		return nil, nil, err
	}
	return scoped, func() { cleanup(); tlsCleanup() }, nil
}
