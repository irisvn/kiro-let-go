# kiro-let-go

## What is this?

A Go-based gateway proxy that fronts multiple Kiro AI accounts, exposing OpenAI- and Anthropic-compatible APIs with multi-account failover, anti-ban protections, and quota inspection. It load-balances requests across accounts, recovers from failures automatically, and resists Kiro's ban-detection mechanisms — so you can drop it into existing clients without changing code.

## Build

```bash
make build
```

This produces two binaries in `bin/`:

- `kiro-let-go` — the HTTP server
- `kiro-let-go-cli` — the management CLI

## Configure

1. Copy the example config and edit it:

```bash
cp configs/config.example.json configs/config.json
```

2. Replace the placeholder API keys:

- `server.admin_api_key` — used to manage accounts via the REST admin API and CLI
- `server.proxy_api_key` — used by clients when sending chat requests

3. (Optional) Set `storage.credentials_json_path` if you want to load accounts from a JSON file on disk. The server watches that file and syncs changes automatically.

## Add an account

You need at least one account before the proxy can handle requests. There are three ways to add one.

### 1. CLI

```bash
./bin/kiro-let-go-cli account add \
  --type social \
  --label my-account \
  --refresh-token "<your-refresh-token>" \
  --region us-east-1

./bin/kiro-let-go-cli account add \
  --type apikey \
  --label my-apikey-account \
  --key "ksk_xxxxxxxxxxxxxxxxxxxxxx" \
  --region us-east-1
```

### 2. REST API

```bash
curl -X POST http://localhost:8765/admin/accounts \
  -H "Authorization: Bearer REPLACE_ME_ADMIN" \
  -H "Content-Type: application/json" \
  -d '{
    "label": "my-account",
    "auth_method": "social",
    "refresh_token": "<your-refresh-token>",
    "region": "us-east-1",
    "enabled": true
  }'
```

For API key auth, use `"auth_method": "apikey"` and `"api_key": "ksk_..."` instead of `refresh_token`.

### 3. File

Create a credentials JSON file (e.g., `configs/credentials.json`) and point `storage.credentials_json_path` to it:

```json
[
  {"label":"social-acct","auth_method":"social","refresh_token":"<token>","profile_arn":"<arn>","region":"us-east-1","enabled":true},
  {"label":"apikey-acct","auth_method":"apikey","api_key":"ksk_xxxxxxxxxxxxxxxxxxxxxx","enabled":true}
]
```

The server watches this file and syncs accounts automatically. You can also add `"_delete": true` to an entry to remove an account, or add a single `{"_remove_unlisted": true}` entry to delete any accounts not present in the file.

## Use it

Point your OpenAI or Anthropic client at `http://localhost:8765` and use `REPLACE_ME_PROXY` as the API key.

### Anthropic-compatible endpoint

```bash
curl http://localhost:8765/v1/messages \
  -H "Authorization: Bearer REPLACE_ME_PROXY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4.6",
    "messages": [{"role": "user", "content": "Hello"}],
    "max_tokens": 1024
  }'
```

### OpenAI-compatible endpoint

```bash
curl http://localhost:8765/v1/chat/completions \
  -H "Authorization: Bearer REPLACE_ME_PROXY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4.6",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

Supported models include `claude-sonnet-4.5`, `claude-sonnet-4.6`, `claude-opus-4.5`, `claude-opus-4.6`, `claude-opus-4.7`, and `claude-haiku-4.5`. You can also use the short aliases `sonnet`, `opus`, and `haiku`.

Streaming is supported on both endpoints. Add `"stream": true` to the request body.

## Check quota

### CLI

```bash
# Summary for all accounts
./bin/kiro-let-go-cli quota

# Force a fresh fetch for one account
./bin/kiro-let-go-cli quota <account-id> --force
```

### REST API

```bash
# Summary for all accounts
curl http://localhost:8765/admin/quota \
  -H "Authorization: Bearer REPLACE_ME_ADMIN"

# Quota for a single account
curl "http://localhost:8765/admin/accounts/<account-id>/quota?force=true" \
  -H "Authorization: Bearer REPLACE_ME_ADMIN"
```

## Anti-ban notes

The proxy tries to keep accounts healthy by mimicking real Kiro IDE traffic:

- **Per-account machine IDs.** Each account gets a stable, deterministic machine ID derived from its label. Requests from different accounts look like they come from different machines.
- **Deterministic headers.** Version strings, OS names, and user-agent values are chosen deterministically per account so the same account always sends the same fingerprint.
- **Circuit breaker.** If an account starts failing repeatedly, it's temporarily taken out of rotation. A small probabilistic slice of requests retries the account to see if it recovered.
- **Proxy isolation.** Each account can use its own HTTP or SOCKS5 proxy. The proxy creates a separate HTTP transport per account so connections don't mix.
- **Sticky sessions.** When enabled, consecutive requests from the same conversation stick to the last successful account. This avoids switching identities mid-chat.
- **Opus filtering.** Free-tier accounts are automatically excluded from Opus model requests because they can't run those models.

## Configuration reference

| Section | Field | Default | Description |
|---------|-------|---------|-------------|
| `server` | `host` | `0.0.0.0` | Bind address for the HTTP server |
| `server` | `port` | `8765` | Listen port |
| `server` | `admin_api_key` | *(required)* | Bearer token for admin endpoints |
| `server` | `proxy_api_key` | *(required)* | API key for client chat endpoints |
| `kiro` | `region` | `us-east-1` | Default Kiro region |
| `kiro` | `auth_region` | `us-east-1` | Region used for authentication requests |
| `kiro` | `api_region` | `us-east-1` | Region used for API requests |
| `storage` | `sqlite_path` | `.data/kiro.db` | Path to the SQLite database |
| `storage` | `credentials_json_path` | ` ""` | Path to a JSON file the server watches for account changes |
| `load_balancer` | `strategy` | `round_robin` | How to pick the next account. Only `round_robin` is supported |
| `load_balancer` | `sticky_session` | `true` | Keep the same account for consecutive requests when possible |
| `quota` | `cache_ttl_seconds` | `43200` | How long to cache quota data (12 hours) |
| `failover` | `base_cooldown_sec` | `60` | Initial circuit-breaker cooldown after failures |
| `failover` | `max_backoff_multiplier` | `1440` | Maximum backoff multiplier for circuit breaker |
| `failover` | `probabilistic_retry_chance` | `0.10` | Chance (0-1) to retry a circuit-opened account |
| `failover` | `max_attempts` | `9` | Max retry attempts per upstream request |
| `logging` | `level` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `logging` | `format` | `json` | Log format: `json` or `text` |

All settings can also be set via environment variables with the `KIRO_` prefix, using uppercase and underscores. For example, `KIRO_SERVER_PORT=8080` overrides `server.port`.

## Limitations

These features are not supported:

- **Non-Claude models.** Only Kiro's Claude-family models work. GPT, Gemini, and others are not available.
- **HTTP image URLs.** Images must be sent as base64 data URLs. Remote HTTP/HTTPS image URLs are rejected.
- **Multiple load-balancer strategies.** Only round-robin is implemented.
- **Authentication methods other than social and apikey.** OAuth variants may be accepted as aliases, but they map to the same two backends.
- **Real-time quota push updates.** Quota is polled on demand or cached; there is no webhook or push mechanism.
- **Cross-account conversation history.** Each request is stateless from the proxy's perspective. Conversation state is managed by the client.
