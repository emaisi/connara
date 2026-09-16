package config

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

const (
	DefaultWorkspaceID = "00000000-0000-0000-0000-000000000001"
	DefaultAdminUserID = "00000000-0000-0000-0000-000000000002"
)

type Config struct {
	Address                string
	DatabaseURL            string
	RedisAddress           string
	RedisPassword          string
	RedisDB                int
	CatalogDirectory       string
	AdminPasswordHash      string
	EncryptionKey          string
	EncryptionKeyVersion   int
	PreviousEncryptionKeys string
	WorkspaceID            string
	WorkspaceSlug          string
	WorkspaceName          string
	AdminUserID            string
	AdminEmail             string
	PublicBaseURL          string
	Role                   string
	AllowedCIDRs           []netip.Prefix
}

func Load() (Config, error) {
	redisDB, err := strconv.Atoi(value("APIHUB_REDIS_DB", "0"))
	if err != nil || redisDB < 0 {
		return Config{}, errors.New("APIHUB_REDIS_DB must be a non-negative integer")
	}
	allowedCIDRs, err := parseCIDRs(os.Getenv("APIHUB_ALLOWED_PRIVATE_CIDRS"))
	if err != nil {
		return Config{}, err
	}
	keyVersion, err := strconv.Atoi(value("APIHUB_ENCRYPTION_KEY_VERSION", "1"))
	if err != nil || keyVersion < 1 || keyVersion > 32767 {
		return Config{}, errors.New("invalid encryption key version")
	}
	config := Config{
		EncryptionKeyVersion: keyVersion, PreviousEncryptionKeys: os.Getenv("APIHUB_PREVIOUS_ENCRYPTION_KEYS"),
		Address:           value("APIHUB_ADDRESS", ":8080"),
		DatabaseURL:       strings.TrimSpace(os.Getenv("APIHUB_DATABASE_URL")),
		RedisAddress:      strings.TrimSpace(os.Getenv("APIHUB_REDIS_ADDR")),
		RedisPassword:     os.Getenv("APIHUB_REDIS_PASSWORD"),
		RedisDB:           redisDB,
		CatalogDirectory:  strings.TrimSpace(os.Getenv("APIHUB_CATALOG_DIR")),
		AdminPasswordHash: strings.TrimSpace(os.Getenv("APIHUB_ADMIN_PASSWORD_HASH")),
		EncryptionKey:     strings.TrimSpace(os.Getenv("APIHUB_ENCRYPTION_KEY")),
		WorkspaceID:       value("APIHUB_WORKSPACE_ID", DefaultWorkspaceID),
		WorkspaceSlug:     value("APIHUB_WORKSPACE_SLUG", "default"),
		WorkspaceName:     value("APIHUB_WORKSPACE_NAME", "APIHub"),
		AdminUserID:       value("APIHUB_ADMIN_USER_ID", DefaultAdminUserID),
		AdminEmail:        value("APIHUB_ADMIN_EMAIL", "admin@localhost"),
		PublicBaseURL:     value("APIHUB_PUBLIC_BASE_URL", "http://127.0.0.1:8080"),
		Role:              value("APIHUB_ROLE", "all"),
		AllowedCIDRs:      allowedCIDRs,
	}
	if config.DatabaseURL == "" {
		return Config{}, errors.New("APIHUB_DATABASE_URL is required")
	}
	if config.RedisAddress == "" {
		return Config{}, errors.New("APIHUB_REDIS_ADDR is required")
	}

	if config.EncryptionKey == "" {
		return Config{}, errors.New("APIHUB_ENCRYPTION_KEY is required")
	}
	switch config.Role {
	case "all", "api", "worker", "scheduler":
	default:
		return Config{}, errors.New("APIHUB_ROLE must be all, api, worker, or scheduler")
	}
	return config, nil
}

func parseCIDRs(raw string) ([]netip.Prefix, error) {
	var prefixes []netip.Prefix
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(item)
		if err != nil {
			return nil, fmt.Errorf("APIHUB_ALLOWED_PRIVATE_CIDRS contains invalid CIDR %q: %w", item, err)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

func value(name, fallback string) string {
	if current := strings.TrimSpace(os.Getenv(name)); current != "" {
		return current
	}
	return fallback
}
