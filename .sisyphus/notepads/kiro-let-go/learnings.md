 - CLI root now validates cobra reserved names before executing.
 - Version subcommand must avoid shared init hooks so it can print without config/db setup.
 - Existing CLI tests may call command helpers directly, so nil-safe guards are useful around optional runtime dependencies.
 - Server entry flags can stay bound to config keys by normalizing CLI hyphens to underscores before passing the FlagSet into viper-backed config loading.
 - Graceful shutdown is simplest when the signal goroutine only cancels the errgroup context and long-lived workers translate context cancellation into nil on normal exit.

  - Anthropic route registration needs a non-/v1 router because Handler.Register hardcodes /v1/messages; wrapping the root router with proxy auth preserves the correct path and middleware.
  - Existing server auth tests still instantiate server.New with nil admin dependencies, so the real admin list route must remain nil-safe and return an empty list instead of panicking.
  - The Anthropic count_tokens endpoint can share the existing message/system/tool normalization path and reuse normalizedToKiro plus estimateInputTokens for parity with /v1/messages token accounting.
  - Switching circuit retries to rand.Float64() keeps the breaker distribution test valid while removing deterministic retry counters from both the breaker and manager.
  - Example config coverage can load configs/config.example.json directly after replacing REPLACE_ME_* placeholders; Validate() then confirms the file is production-ready.

- F2/F4 fixes: Anthropic handler test seams should live on Handler fields, not mutable package vars; server Run can use net.Listen + Serve so tests with port 0 can read the actual bound address through an unexported channel.
- Claude-only API validation means existing OpenAI tests must use Claude model IDs except for explicit non-Claude rejection cases.

## F4 scope fidelity re-run - 2026-05-20
- Verdict: APPROVE. Claude-only validation present in Anthropic/OpenAI Validate() functions.
- Anti-ban checks passed: machine ID, stable headers, per-account proxy, no quota goroutine/ticker/sleep, circuit breaker/backoff/probabilistic retry.
- Account channels passed: CLI add, REST admin POST /accounts, fsnotify watcher.
- Scope checks passed: no IdC/OIDC/SSO references, no GPT/Gemini/websearch/cache/compress/prompt-filter Go references, no web UI files found.
- Infrastructure passed: modernc.org/sqlite and sticky sessions present.
