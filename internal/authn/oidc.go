package authn

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"apihub-go/internal/executor"
	"apihub-go/internal/jsonutil"
	"apihub-go/internal/model"
	"github.com/coreos/go-oidc/v3/oidc"
)

func (s *Service) VerifyOIDCTokens(ctx context.Context, instance model.AuthInstance, tokens OAuthTokens, nonce string) error {
	if nonce == "" {
		return errors.New("OIDC nonce is invalid")
	}
	secrets, err := s.AuthInstanceSecrets(instance)
	if err != nil {
		return err
	}
	clientID := firstString(secrets, "clientId", "client_id")
	raw := firstString(tokens.Credentials, "id_token")
	if clientID == "" || raw == "" {
		return errors.New("OIDC client ID or ID token is missing")
	}
	provider, err := s.oidcProvider(ctx, instance)
	if err != nil {
		return err
	}
	config, err := readAdvancedConfig(instance)
	if err != nil {
		return err
	}
	client, cleanup, err := executor.ConfiguredEndpointClient(s.client, config.Issuer, instance.TokenURL, instance.RefreshURL)
	if err != nil {
		return err
	}
	defer cleanup()
	ctx = oidc.ClientContext(ctx, client)
	idToken, err := provider.Verifier(&oidc.Config{ClientID: clientID}).Verify(ctx, raw)
	if err != nil {
		return err
	}
	if idToken.Nonce != nonce {
		return errors.New("OIDC nonce mismatch")
	}
	if strings.TrimSpace(idToken.Subject) == "" {
		return errors.New("OIDC subject is missing")
	}
	var claims struct {
		AuthorizedParty string `json:"azp"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return err
	}
	if (len(idToken.Audience) > 1 || claims.AuthorizedParty != "") && claims.AuthorizedParty != clientID {
		return errors.New("OIDC authorized party does not match client ID")
	}
	return nil
}

func (s *Service) VerifyOIDCProvider(ctx context.Context, instance model.AuthInstance) error {
	_, err := s.oidcProvider(ctx, instance)
	return err
}

func (s *Service) oidcProvider(ctx context.Context, instance model.AuthInstance) (*oidc.Provider, error) {
	config, err := readAdvancedConfig(instance)
	if err != nil {
		return nil, err
	}
	issuerURL, err := url.Parse(config.Issuer)
	if err != nil || !httpScheme(issuerURL) || issuerURL.Host == "" || issuerURL.User != nil || issuerURL.RawQuery != "" || issuerURL.Fragment != "" {
		return nil, errors.New("OIDC issuer is invalid")
	}
	client, cleanup, err := executor.ConfiguredEndpointClient(s.client, config.Issuer, instance.TokenURL, instance.RefreshURL)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	ctx = oidc.ClientContext(ctx, client)
	provider, err := oidc.NewProvider(ctx, config.Issuer)
	if err != nil {
		return nil, err
	}
	if provider.Endpoint().TokenURL != instance.TokenURL {
		return nil, errors.New("OIDC token endpoint does not match issuer discovery")
	}
	var urls struct {
		AuthorizationURL string `json:"authorizationUrl"`
	}
	if err := jsonutil.Unmarshal(instance.PublicConfig, &urls); err != nil || provider.Endpoint().AuthURL != urls.AuthorizationURL {
		return nil, errors.New("OIDC authorization endpoint does not match issuer discovery")
	}
	return provider, nil
}
