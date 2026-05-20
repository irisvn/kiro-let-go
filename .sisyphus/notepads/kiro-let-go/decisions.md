 - Kept cmd/cli/main.go as a thin shim that only executes the internal CLI and formats top-level errors with a red `[error]` prefix.
 - Reused internal/cli package wiring for account/quota/server/version commands instead of duplicating command logic in cmd/cli.
 - Wired cmd/server through a testable build/run split so exit-code handling, signal-driven shutdown, and startup failures can be exercised without calling os.Exit in tests.

 - Wired real server routes directly in internal/server/server.go: proxy-authenticated Anthropic/OpenAI endpoints plus admin.RegisterRoutes with Store, Manager, QuotaFetcher, and Circuit dependencies from cmd/server/main.go.
 - Injected account.ProfileARN inside internal/kiro/dispatcher.buildKiroRequest so handlers can keep calling NormalizedToKiro(..., "") while per-account profile ARN still reaches the upstream Kiro payload.
 - Implemented POST /v1/messages/count_tokens inside the Anthropic handler by reusing the same normalization and token-estimation flow as POST /v1/messages, avoiding a separate token counting code path.
