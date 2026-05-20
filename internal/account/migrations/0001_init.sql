CREATE TABLE IF NOT EXISTS accounts (
  id TEXT PRIMARY KEY,
  label TEXT NOT NULL,
  auth_method TEXT NOT NULL,
  access_token TEXT,
  refresh_token TEXT,
  api_key TEXT,
  expires_at TEXT,
  profile_arn TEXT,
  region TEXT NOT NULL DEFAULT 'us-east-1',
  auth_region TEXT,
  api_region TEXT,
  machine_id TEXT NOT NULL,
  proxy_url TEXT,
  proxy_username TEXT,
  proxy_password TEXT,
  enabled INTEGER NOT NULL DEFAULT 1,
  disabled_reason TEXT,
  failure_count INTEGER NOT NULL DEFAULT 0,
  last_failure_at TEXT,
  success_count INTEGER NOT NULL DEFAULT 0,
  last_used_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS quota_cache (
  account_id TEXT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
  payload_json TEXT NOT NULL,
  fetched_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_accounts_enabled ON accounts(enabled);
CREATE INDEX IF NOT EXISTS idx_accounts_auth_method ON accounts(auth_method);

CREATE TABLE IF NOT EXISTS _migrations (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
);
