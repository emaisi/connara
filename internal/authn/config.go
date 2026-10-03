package authn

import (
	"errors"
	"net/url"
	"strings"

	"apihub-go/internal/executor"
	"apihub-go/internal/jsonutil"
	"apihub-go/internal/model"
)

func httpScheme(value *url.URL) bool {
	return value != nil && (value.Scheme == "http" || value.Scheme == "https")
}

func ValidateInstanceConfiguration(instance model.AuthInstance) error {
	config, err := readAdvancedConfig(instance)
	if err != nil {
		return err
	}
	switch config.ClientAuth {
	case "", "client_secret_post", "client_secret_basic", "private_key_jwt", "tls_client_auth":
	default:
		return errors.New("unsupported OAuth client authentication method")
	}
	for _, rawURL := range []string{instance.TokenURL, instance.RefreshURL} {
		if rawURL == "" {
			continue
		}
		parsed, err := url.Parse(rawURL)
		if err != nil || !httpScheme(parsed) || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
			return errors.New("authentication URLs must be absolute http(s) URLs without user info or fragments")
		}
	}
	for _, path := range []string{instance.TokenPath, instance.ExpiryPath} {
		if path != "" {
			if _, ok := jsonutil.PathSegments(path); !ok {
				return errors.New("token or expiry JSON path is invalid")
			}
		}
	}
	var verification struct {
		VerificationPath string `json:"verificationPath"`
	}
	if len(instance.PublicConfig) > 0 {
		if err := jsonutil.Unmarshal(instance.PublicConfig, &verification); err != nil {
			return errors.New("invalid verification configuration")
		}
	}
	if verification.VerificationPath != "" {
		if err := executor.ValidatePath(verification.VerificationPath); err != nil {
			return err
		}
	}
	flow := instance.AuthTemplateFlow
	if (flow == "password_token" || flow == "client_credentials") && instance.TokenPath == "" {
		return errors.New("token JSON path is required")
	}
	if flow == "oauth2_code" || flow == "oidc" {
		var oauth struct {
			AuthorizationURL string `json:"authorizationUrl"`
		}
		if err := jsonutil.Unmarshal(instance.PublicConfig, &oauth); err != nil {
			return errors.New("OAuth authorization URL is invalid")
		}
		authorize, err := url.Parse(oauth.AuthorizationURL)
		if err != nil || !httpScheme(authorize) || authorize.Host == "" || authorize.User != nil || authorize.Fragment != "" {
			return errors.New("OAuth authorization URL must be an absolute http(s) URL")
		}
	}
	if flow == "oidc" {
		issuer, err := url.Parse(config.Issuer)
		if err != nil || !httpScheme(issuer) || issuer.Host == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" {
			return errors.New("OIDC issuer must be an absolute http(s) URL")
		}
	}
	if flow == "aws_sigv4" && (!awsName.MatchString(config.Region) || !awsName.MatchString(config.Service)) {
		return errors.New("AWS region and service are required")
	}
	if flow == "token_exchange" && !strings.HasPrefix(config.SubjectType, "urn:ietf:params:oauth:token-type:") {
		return errors.New("subject token type must be a standard OAuth token type URN")
	}
	if flow == "token_exchange" && config.RequestedType != "" && !strings.HasPrefix(config.RequestedType, "urn:ietf:params:oauth:token-type:") {
		return errors.New("requested token type must be a standard OAuth token type URN")
	}
	if flow == "jwt_direct" && (config.Issuer == "" || config.Audience == "") {
		return errors.New("direct JWT issuer and audience are required")
	}
	switch flow {
	case "oauth2_code", "oidc", "client_credentials", "password_token", "jwt_bearer_grant", "token_exchange":
		if instance.TokenURL == "" {
			return errors.New("token URL is required")
		}
	}
	return nil
}

func (s *Service) ValidateInstanceSecrets(instance model.AuthInstance) error {
	config, err := readAdvancedConfig(instance)
	if err != nil {
		return err
	}
	values, err := s.AuthInstanceSecrets(instance)
	if err != nil {
		return err
	}
	if instance.AuthTemplateFlow == "oauth2_code" || instance.AuthTemplateFlow == "oidc" {
		if firstString(values, "clientId", "client_id") == "" {
			return errors.New("OAuth client ID is required")
		}
	}
	if firstString(values, "clientId", "client_id") != "" && (instance.AuthTemplateFlow == "oauth2_code" || instance.AuthTemplateFlow == "oidc" || config.ClientAuth != "") {
		if _, err := oauthClientAuth(instance, values, url.Values{}, instance.TokenURL); err != nil {
			return err
		}
	}
	if config.ClientAuth == "tls_client_auth" || config.TLSForToken {
		if _, err := clientTLS(values, config); err != nil {
			return err
		}
	}
	return nil
}

// ValidateRequestSecrets checks references against this instance only.
func (s *Service) ValidateRequestSecrets(instance model.AuthInstance, template model.AuthTemplate) error {
	request, err := EffectiveRequest(instance, template)
	if err != nil {
		return err
	}
	advanced, err := ParseAuthRequest(instance)
	if err != nil {
		return err
	}
	secrets, err := s.AuthInstanceSecrets(instance)
	if err != nil {
		return err
	}
	requests := []RequestConfig{request}
	if advanced.Refresh.Request != nil {
		requests = append(requests, *advanced.Refresh.Request)
	}
	for _, config := range requests {
		values := []RequestValue{}
		for _, param := range config.Parameters {
			values = append(values, param.Value)
		}
		for _, header := range config.Headers {
			values = append(values, header.Value)
		}
		for _, value := range values {
			if value.Source == "instance_secret" {
				if secret, ok := secrets[value.Name]; !ok || secret == nil || secret == "" {
					return errors.New("referenced instance secret is missing: " + value.Name)
				}
			}
		}
	}
	return nil
}
