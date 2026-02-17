CREATE DOMAIN plugin_id AS TEXT;

CREATE TYPE plugin_category AS ENUM ('ai-agent', 'plugin', 'app');

CREATE TABLE plugins (
    id plugin_id PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    server_endpoint TEXT NOT NULL,
    category plugin_category NOT NULL,
    audited BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE plugin_apikey (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plugin_id plugin_id NOT NULL REFERENCES plugins(id) ON DELETE CASCADE,
    apikey TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NULL,
    status INT NOT NULL DEFAULT 1 CHECK (status IN (0, 1))
);

CREATE TABLE vault_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_id VARCHAR(255) NOT NULL UNIQUE,
    public_key VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE plugin_policies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    public_key TEXT NOT NULL,
    plugin_id plugin_id NOT NULL,
    plugin_version TEXT NOT NULL,
    policy_version INTEGER NOT NULL,
    signature TEXT NOT NULL,
    recipe TEXT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
