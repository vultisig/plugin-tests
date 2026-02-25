-- name: UpsertPlugin :exec
INSERT INTO plugins (id, title, description, server_endpoint, category, audited)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (id) DO UPDATE SET
    title = EXCLUDED.title,
    description = EXCLUDED.description,
    server_endpoint = EXCLUDED.server_endpoint,
    category = EXCLUDED.category,
    audited = EXCLUDED.audited,
    updated_at = NOW();

-- name: UpsertPluginAPIKey :exec
INSERT INTO plugin_apikey (id, plugin_id, apikey, created_at, expires_at, status)
VALUES (gen_random_uuid(), $1, $2, NOW(), NULL, 1)
ON CONFLICT DO NOTHING;

-- name: UpsertVaultToken :exec
INSERT INTO vault_tokens (token_id, public_key, expires_at, last_used_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (token_id) DO UPDATE SET
    public_key = EXCLUDED.public_key,
    expires_at = EXCLUDED.expires_at,
    last_used_at = EXCLUDED.last_used_at,
    revoked_at = NULL,
    updated_at = NOW();

-- name: UpsertPluginPolicy :exec
INSERT INTO plugin_policies (id, public_key, plugin_id, plugin_version, policy_version, signature, recipe, active)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (id) DO UPDATE SET
    public_key = EXCLUDED.public_key,
    plugin_id = EXCLUDED.plugin_id,
    plugin_version = EXCLUDED.plugin_version,
    policy_version = EXCLUDED.policy_version,
    signature = EXCLUDED.signature,
    recipe = EXCLUDED.recipe,
    active = EXCLUDED.active,
    updated_at = NOW();
