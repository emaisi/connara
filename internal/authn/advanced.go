package authn

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"time"

	"apihub-go/internal/jsonutil"
	"apihub-go/internal/model"
	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

var awsName = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)

type advancedConfig struct {
	Issuer        string `json:"issuer"`
	Subject       string `json:"subject"`
	Audience      string `json:"audience"`
	KeyID         string `json:"keyId"`
	Region        string `json:"region"`
	Service       string `json:"service"`
	CACert        string `json:"caCertificate"`
	ClientAuth    string `json:"clientAuthMethod"`
	TLSForToken   bool   `json:"tlsForToken"`
	TLSForAPI     bool   `json:"tlsForApi"`
	SubjectType   string `json:"subjectTokenType"`
	RequestedType string `json:"requestedTokenType"`
	Resource      string `json:"resource"`
	Scope         string `json:"scope"`
}

func readAdvancedConfig(instance model.AuthInstance) (advancedConfig, error) {
	var config advancedConfig
	if len(instance.PublicConfig) > 0 {
		if err := jsonutil.Unmarshal(instance.PublicConfig, &config); err != nil {
			return config, fmt.Errorf("invalid authentication config: %w", err)
		}
	}
	return config, nil
}

func clientTLS(values map[string]any, config advancedConfig) (*tls.Config, error) {
	certificate := firstString(values, "certificate")
	privateKey := firstString(values, "tls_private_key", "private_key")
	if certificate == "" || privateKey == "" {
		return nil, errors.New("client certificate and private key are required")
	}
	pair, err := tls.X509KeyPair([]byte(certificate), []byte(privateKey))
	if err != nil {
		return nil, errors.New("client certificate or private key is invalid")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) {
		return nil, errors.New("client certificate is not currently valid")
	}
	if len(leaf.ExtKeyUsage) > 0 {
		allowed := false
		for _, usage := range leaf.ExtKeyUsage {
			allowed = allowed || usage == x509.ExtKeyUsageClientAuth || usage == x509.ExtKeyUsageAny
		}
		if !allowed {
			return nil, errors.New("certificate is not valid for client authentication")
		}
	}
	result := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}}
	if config.CACert != "" {
		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("load system CAs: %w", err)
		}
		if !roots.AppendCertsFromPEM([]byte(config.CACert)) {
			return nil, errors.New("CA certificate is invalid")
		}
		result.RootCAs = roots
	}
	return result, nil
}

func awsSigner(values map[string]any, config advancedConfig) (func(context.Context, *http.Request) error, error) {
	if !awsName.MatchString(config.Region) || !awsName.MatchString(config.Service) {
		return nil, errors.New("AWS region and service are required")
	}
	key := firstString(values, "access_key_id")
	secret := firstString(values, "secret_access_key")
	if key == "" || secret == "" {
		return nil, errors.New("AWS access key and secret key are required")
	}
	var until time.Time
	if expiry := firstString(values, "expires_at"); expiry != "" {
		var err error
		until, err = time.Parse(time.RFC3339, expiry)
		if err != nil {
			return nil, errors.New("AWS credential expiry is invalid")
		}
	}
	credentials := aws.Credentials{AccessKeyID: key, SecretAccessKey: secret, SessionToken: firstString(values, "session_token")}
	signer := v4.NewSigner()
	return func(ctx context.Context, request *http.Request) error {
		if !until.IsZero() && !time.Now().Before(until) {
			return errors.New("AWS credentials are expired")
		}
		hash := sha256.New()
		if request.Body != nil {
			if request.GetBody == nil {
				return errors.New("signed request body cannot be replayed")
			}
			body, err := request.GetBody()
			if err != nil {
				return err
			}
			_, err = io.Copy(hash, body)
			body.Close()
			if err != nil {
				return err
			}
		}
		return signer.SignHTTP(ctx, credentials, request, fmt.Sprintf("%x", hash.Sum(nil)), config.Service, config.Region, time.Now().UTC())
	}, nil
}

func signJWT(privatePEM string, claims map[string]any, kid string) (string, error) {
	block, _ := pem.Decode([]byte(privatePEM))
	if block == nil {
		return "", errors.New("JWT private key is invalid")
	}
	var key any
	var err error
	switch block.Type {
	case "PRIVATE KEY":
		key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		key, err = x509.ParseECPrivateKey(block.Bytes)
	default:
		return "", errors.New("unsupported JWT private key type")
	}
	if err != nil {
		return "", errors.New("JWT private key is invalid")
	}
	header := map[string]any{"typ": "JWT"}
	switch typed := key.(type) {
	case *rsa.PrivateKey:
		if typed.N.BitLen() < 2048 {
			return "", errors.New("JWT RSA key must be at least 2048 bits")
		}
		header["alg"] = "RS256"
	case *ecdsa.PrivateKey:
		if typed.Curve.Params().BitSize != 256 {
			return "", errors.New("JWT ECDSA key must use P-256")
		}
		header["alg"] = "ES256"
	default:
		return "", errors.New("unsupported JWT private key")
	}
	if kid != "" {
		header["kid"] = kid
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	message := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	digest := sha256.Sum256([]byte(message))
	var signature []byte
	switch typed := key.(type) {
	case *rsa.PrivateKey:
		signature, err = rsa.SignPKCS1v15(rand.Reader, typed, crypto.SHA256, digest[:])
	case *ecdsa.PrivateKey:
		var r, s *big.Int
		r, s, err = ecdsa.Sign(rand.Reader, typed, digest[:])
		if err == nil {
			signature = make([]byte, 64)
			r.FillBytes(signature[:32])
			s.FillBytes(signature[32:])
		}
	}
	if err != nil {
		return "", err
	}
	return message + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func signedAssertion(values map[string]any, config advancedConfig, audience string) (string, error) {
	privateKey := firstString(values, "private_key")
	issuer := config.Issuer
	if issuer == "" {
		issuer = firstString(values, "issuer", "client_id")
	}
	if privateKey == "" || issuer == "" {
		return "", errors.New("JWT issuer and private key are required")
	}
	now := time.Now().UTC()
	claims := map[string]any{"iss": issuer, "sub": issuer, "iat": now.Add(-time.Minute).Unix(), "exp": now.Add(5 * time.Minute).Unix()}
	if config.Subject != "" {
		claims["sub"] = config.Subject
	}
	if audience != "" {
		claims["aud"] = audience
	}
	return signJWT(privateKey, claims, config.KeyID)
}

func (s *Service) advancedCredentials(instance model.AuthInstance, credentials map[string]any) (map[string]any, advancedConfig, error) {
	config, err := readAdvancedConfig(instance)
	if err != nil {
		return nil, config, err
	}
	secrets, err := s.AuthInstanceSecrets(instance)
	if err != nil {
		return nil, config, err
	}
	values := make(map[string]any, len(secrets)+len(credentials))
	for key, value := range secrets {
		values[key] = value
	}
	for key, value := range credentials {
		values[key] = value
	}
	return values, config, nil
}

func (s *Service) applyAdvanced(instance model.AuthInstance, credentials map[string]any, resolved *Resolved) error {
	flow := instance.AuthTemplateFlow
	config, err := readAdvancedConfig(instance)
	if err != nil {
		return err
	}
	if flow != "mtls" && flow != "aws_sigv4" && flow != "jwt_direct" && !config.TLSForAPI {
		return nil
	}
	values, _, err := s.advancedCredentials(instance, credentials)
	if err != nil {
		return err
	}
	if flow == "mtls" || config.TLSForAPI {
		resolved.TLS, err = clientTLS(values, config)
		if err != nil {
			return err
		}
	}
	switch flow {
	case "aws_sigv4":
		resolved.Sign, err = awsSigner(values, config)
	case "jwt_direct":
		var assertion string
		assertion, err = signedAssertion(values, config, config.Audience)
		if err == nil {
			resolved.Headers["Authorization"] = "Bearer " + assertion
		}
	}
	return err
}

// CredentialValues combines instance secrets and account input without persisting them.
func (s *Service) CredentialValues(instance model.AuthInstance, credentials map[string]any) (map[string]any, advancedConfig, error) {
	return s.advancedCredentials(instance, credentials)
}
