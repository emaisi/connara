package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"apihub-go/internal/model"
	"github.com/jackc/pgx/v5"
)

type BootstrapInput struct {
	WorkspaceID       string
	WorkspaceSlug     string
	WorkspaceName     string
	AdminUserID       string
	AdminEmail        string
	AdminPasswordHash string
	PublicBaseURL     string
	Providers         []model.Provider
}

type builtinAuthTemplate struct {
	Key              string
	Name             string
	Flow             string
	CredentialSchema any
	TokenRequest     any
	InjectionRules   any
}

var builtinAuthTemplates = []builtinAuthTemplate{
	{Key: "no_auth", Name: "无需认证", Flow: "none", CredentialSchema: map[string]any{}, TokenRequest: map[string]any{}, InjectionRules: []any{}},
	{Key: "api_key", Name: "API 密钥 / Bearer 令牌", Flow: "static", CredentialSchema: fields(field("apiKey", "访问令牌", true)), TokenRequest: map[string]any{}, InjectionRules: rules(rule("header", "Authorization", "Bearer {{apiKey}}"))},
	{Key: "oauth2", Name: "OAuth 2.0 授权码", Flow: "oauth2_code", CredentialSchema: fields(), TokenRequest: map[string]any{"method": "POST"}, InjectionRules: rules(rule("header", "Authorization", "Bearer {{access_token}}"))},
	{Key: "oauth1", Name: "OAuth 1.0a", Flow: "oauth1", CredentialSchema: fields(), TokenRequest: map[string]any{}, InjectionRules: []any{}},
	{Key: "basic", Name: "Basic 认证", Flow: "static", CredentialSchema: fields(field("username", "用户名", false), field("password", "密码", true)), TokenRequest: map[string]any{}, InjectionRules: rules(rule("header", "Authorization", "Basic {{basic_token}}"))},
	{Key: "hmac", Name: "HMAC / AWS SigV4", Flow: "request_signing", CredentialSchema: fields(field("access_key", "Access Key", false), field("secret_key", "Secret Key", true)), TokenRequest: map[string]any{}, InjectionRules: []any{}},
	{Key: "username-password-token", Name: "用户名密码换 Token", Flow: "password_token", CredentialSchema: fields(field("username", "系统用户名", false), field("password", "系统密码", true)), TokenRequest: map[string]any{"method": "POST", "bodyType": "json"}, InjectionRules: rules(rule("header", "Authorization", "Bearer {{token}}"))},
	{Key: "client-credentials", Name: "OAuth 2.0 客户端凭证", Flow: "client_credentials", CredentialSchema: fields(field("client_id", "客户端 ID", false), field("client_secret", "客户端密钥", true)), TokenRequest: map[string]any{"method": "POST", "bodyType": "form"}, InjectionRules: rules(rule("header", "Authorization", "Bearer {{access_token}}"))},
	{Key: "mtls", Name: "mTLS 双向证书", Flow: "mtls", CredentialSchema: fields(field("certificate", "客户端证书", true), field("private_key", "私钥", true)), TokenRequest: map[string]any{}, InjectionRules: []any{}},
	{Key: "oidc", Name: "OIDC / 企业 SSO", Flow: "oidc", CredentialSchema: fields(), TokenRequest: map[string]any{}, InjectionRules: rules(rule("header", "Authorization", "Bearer {{access_token}}"))},
	{Key: "jwt", Name: "JWT 服务账号", Flow: "jwt", CredentialSchema: fields(field("issuer", "Issuer", false), field("private_key", "私钥", true)), TokenRequest: map[string]any{}, InjectionRules: rules(rule("header", "Authorization", "Bearer {{jwt}}"))},
	{Key: "saml-token-exchange", Name: "SAML / Token Exchange", Flow: "token_exchange", CredentialSchema: fields(field("assertion", "SAML Assertion", true)), TokenRequest: map[string]any{"method": "POST", "bodyType": "form"}, InjectionRules: rules(rule("header", "Authorization", "Bearer {{access_token}}"))},
	{Key: "kerberos", Name: "Kerberos / NTLM 网关", Flow: "gateway", CredentialSchema: fields(field("gateway_token", "网关令牌", true)), TokenRequest: map[string]any{}, InjectionRules: rules(rule("header", "Authorization", "Negotiate {{gateway_token}}"))},
}

func field(name, label string, secret bool) map[string]any {
	return map[string]any{"name": name, "label": label, "secret": secret}
}

func fields(values ...map[string]any) map[string]any {
	return map[string]any{"type": "object", "fields": values}
}

func rule(target, name, template string) map[string]any {
	return map[string]any{"target": target, "name": name, "template": template}
}

func rules(values ...map[string]any) []map[string]any { return values }

func (s *Store) Bootstrap(ctx context.Context, input BootstrapInput) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, input.WorkspaceID+":bootstrap"); err != nil {
			return err
		}
		var done bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bootstrap_state WHERE workspace_id=$1)`, input.WorkspaceID).Scan(&done); err != nil {
			return err
		}
		if done {
			return nil
		}
		if _, err := tx.Exec(ctx, `INSERT INTO bootstrap_state(workspace_id) VALUES($1)`, input.WorkspaceID); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO workspaces(id, slug, name, status)
			VALUES($1, $2, $3, 'active')
			ON CONFLICT(id) DO UPDATE SET slug = EXCLUDED.slug, name = EXCLUDED.name, updated_at = now()`,
			input.WorkspaceID, input.WorkspaceSlug, input.WorkspaceName); err != nil {
			return fmt.Errorf("bootstrap workspace: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO users(id, email, display_name, password_hash, status)
			VALUES($1, $2, 'Administrator', $3, 'active')
			ON CONFLICT(id) DO UPDATE SET
				email = EXCLUDED.email,
				password_hash = COALESCE(users.password_hash, EXCLUDED.password_hash),
				updated_at = now()`,
			input.AdminUserID, input.AdminEmail, input.AdminPasswordHash); err != nil {
			return fmt.Errorf("bootstrap admin user: %w", err)
		}
		memberID := StableID("member", input.WorkspaceID+":"+input.AdminUserID)
		if _, err := tx.Exec(ctx, `
			INSERT INTO workspace_members(id, workspace_id, user_id, role, status)
			VALUES($1, $2, $3, 'owner', 'active')
			ON CONFLICT(workspace_id, user_id) DO UPDATE SET role = 'owner', status = 'active', updated_at = now()`,
			memberID, input.WorkspaceID, input.AdminUserID); err != nil {
			return fmt.Errorf("bootstrap admin membership: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO platform_settings(
				workspace_id, platform_name, public_base_url, runtime_parameters,
				operation_retention_days, audit_retention_days, updated_by
			) VALUES($1, $2, $3, '{"SYNC_PAGE_SIZE":"100"}'::jsonb, 30, 365, $4)
			ON CONFLICT(workspace_id) DO NOTHING`, input.WorkspaceID, input.WorkspaceName, input.PublicBaseURL, input.AdminUserID); err != nil {
			return fmt.Errorf("bootstrap platform settings: %w", err)
		}
		templateIDs := make(map[string]string, len(builtinAuthTemplates))
		for _, template := range builtinAuthTemplates {
			id := StableID("auth-template", input.WorkspaceID+":"+template.Key)
			templateIDs[template.Key] = id
			credentialSchema, _ := json.Marshal(template.CredentialSchema)
			tokenRequest, _ := json.Marshal(template.TokenRequest)
			injectionRules, _ := json.Marshal(template.InjectionRules)
			if _, err := tx.Exec(ctx, `
				INSERT INTO auth_templates(
					id, workspace_id, template_key, name, source, flow_type, status,
					credential_schema, token_request, injection_rules
				) VALUES($1, $2, $3, $4, 'builtin', $5, 'published', $6, $7, $8)
				ON CONFLICT(workspace_id, template_key) WHERE deleted_at IS NULL DO UPDATE SET
					name = EXCLUDED.name, flow_type = EXCLUDED.flow_type,
					credential_schema = EXCLUDED.credential_schema,
					token_request = EXCLUDED.token_request, injection_rules = EXCLUDED.injection_rules,
					updated_at = now()`, id, input.WorkspaceID, template.Key, template.Name, template.Flow,
				credentialSchema, tokenRequest, injectionRules); err != nil {
				return fmt.Errorf("bootstrap auth template %s: %w", template.Key, err)
			}
		}
		groups := make(map[string]string)
		for _, provider := range input.Providers {
			groupName := "其他"
			if len(provider.Categories) > 0 && strings.TrimSpace(provider.Categories[0]) != "" {
				groupName = strings.TrimSpace(provider.Categories[0])
			}
			groupID, ok := groups[groupName]
			if !ok {
				groupID = StableID("system-group", input.WorkspaceID+":"+groupName)
				groups[groupName] = groupID
				if _, err := tx.Exec(ctx, `
					INSERT INTO system_groups(id, workspace_id, name)
					VALUES($1, $2, $3)
					ON CONFLICT(workspace_id, name) WHERE deleted_at IS NULL DO UPDATE SET updated_at = now()`,
					groupID, input.WorkspaceID, groupName); err != nil {
					return fmt.Errorf("bootstrap system group %s: %w", groupName, err)
				}
			}
			systemID := StableID("system", input.WorkspaceID+":"+provider.Service)
			if _, err := tx.Exec(ctx, `
				INSERT INTO systems(
					id, workspace_id, system_key, name, source, group_id, description,
					homepage_url, status, catalog_version
				) VALUES($1, $2, $3, $4, 'imported', $5, $6, NULLIF($7, ''), 'active', 'startup')
				ON CONFLICT(workspace_id, system_key) WHERE deleted_at IS NULL DO UPDATE SET
					name = EXCLUDED.name,
					description = EXCLUDED.description, homepage_url = EXCLUDED.homepage_url,
					catalog_version = EXCLUDED.catalog_version, updated_at = now()`,
				systemID, input.WorkspaceID, provider.Service, provider.DisplayName, groupID,
				provider.Description, provider.HomepageURL); err != nil {
				return fmt.Errorf("bootstrap system %s: %w", provider.Service, err)
			}
			for i, authType := range provider.AuthTypes {
				templateID, ok := templateIDs[authType]
				if !ok {
					continue
				}
				pairID := StableID("system-auth", systemID+":"+templateID)
				if _, err := tx.Exec(ctx, `
					INSERT INTO system_auth_templates(id, workspace_id, system_id, auth_template_id, is_default)
					VALUES($1, $2, $3, $4, $5)
					ON CONFLICT(workspace_id, system_id, auth_template_id) DO UPDATE SET is_default = EXCLUDED.is_default`,
					pairID, input.WorkspaceID, systemID, templateID, i == 0); err != nil {
					return fmt.Errorf("bootstrap system auth %s/%s: %w", provider.Service, authType, err)
				}
			}
			for _, action := range provider.Actions {
				if action.Runtime == nil {
					continue
				}
				actionID := StableID("action", input.WorkspaceID+":"+action.ID)
				inputSchema, _ := json.Marshal(action.InputSchema)
				outputSchema, _ := json.Marshal(action.OutputSchema)
				if _, err := tx.Exec(ctx, `
					INSERT INTO actions(
						id, workspace_id, action_key, name, description, source, system_id,
						http_method, relative_path, required_scopes, input_schema, output_schema,
						executable, status, catalog_version
					) VALUES($1, $2, $3, $4, $5, 'imported', $6, $7, $8, $9, $10, $11, $12, 'active', 'startup')
					ON CONFLICT(workspace_id, action_key) WHERE deleted_at IS NULL DO UPDATE SET
						name = EXCLUDED.name, description = EXCLUDED.description,
						http_method = EXCLUDED.http_method, relative_path = EXCLUDED.relative_path,
						required_scopes = EXCLUDED.required_scopes, input_schema = EXCLUDED.input_schema,
						output_schema = EXCLUDED.output_schema, executable = EXCLUDED.executable,
						catalog_version = EXCLUDED.catalog_version, updated_at = now()`,
					actionID, input.WorkspaceID, action.ID, action.Name, action.Description, systemID,
					action.Runtime.Method, action.Runtime.Path, action.RequiredScopes, inputSchema, outputSchema,
					action.Executable); err != nil {
					return fmt.Errorf("bootstrap action %s: %w", action.ID, err)
				}
			}
		}
		return nil
	})
}
