BEGIN;

-- Existing workspaces do not run Bootstrap again. Add only new built-in
-- templates; keep old ambiguous template keys and existing instances intact.
INSERT INTO auth_templates (
    id, workspace_id, template_key, name, source, flow_type, status,
    credential_schema, token_request, injection_rules
)
SELECT gen_random_uuid(), w.id, t.template_key, t.name, 'builtin',
       t.flow_type, 'published', t.credential_schema::jsonb,
       t.token_request::jsonb, t.injection_rules::jsonb
FROM workspaces w
CROSS JOIN (VALUES
    ('jwt-direct', 'JWT 服务账号 · 直接签发', 'jwt_direct',
     '{"type":"object","fields":[{"name":"issuer","label":"Issuer","secret":false},{"name":"private_key","label":"私钥","secret":true}]}', '{}', '[]'),
    ('jwt-bearer-grant', 'JWT 服务账号 · 换取令牌', 'jwt_bearer_grant',
     '{"type":"object","fields":[{"name":"issuer","label":"Issuer","secret":false},{"name":"private_key","label":"私钥","secret":true}]}',
     '{"method":"POST","bodyType":"form"}', '[{"target":"header","name":"Authorization","template":"Bearer {{access_token}}"}]'),
    ('aws-sigv4', 'AWS SigV4', 'aws_sigv4',
     '{"type":"object","fields":[{"name":"access_key_id","label":"Access Key ID","secret":false},{"name":"secret_access_key","label":"Secret Access Key","secret":true},{"name":"session_token","label":"Session Token","secret":true}]}', '{}', '[]'),
    ('oauth2-token-exchange', 'OAuth 2.0 Token Exchange', 'token_exchange',
     '{"type":"object","fields":[{"name":"subject_token","label":"Subject Token","secret":true}]}',
     '{"method":"POST","bodyType":"form"}', '[{"target":"header","name":"Authorization","template":"Bearer {{access_token}}"}]')
) AS t(template_key, name, flow_type, credential_schema, token_request, injection_rules)
ON CONFLICT (workspace_id, template_key) WHERE deleted_at IS NULL DO NOTHING;

INSERT INTO schema_migrations(version) VALUES (5);
COMMIT;
