# T30 Learnings

## Patterns
- Use `gin.New()` instead of `gin.Default()` to avoid built-in logger middleware
- Middleware ordering matters: request ID first so subsequent middleware can use it, then logging, then recovery, then CORS
- Auth middleware should be applied at route group level (`v1.Use(...)`) not globally
- Use `strings.CutPrefix` instead of `HasPrefix` + `TrimPrefix` combo (Go 1.20+)
- `slog.LogAttrs` is more efficient than `slog.Info` with many attributes

## Conventions
- Keep middleware functions in dedicated files per concern
- Factory functions accept dependencies as parameters and return `gin.HandlerFunc`
- Use shared constants/keys across middleware in the same package (e.g., `requestIDKey`)
- Recovery middleware must never leak panic details to the client

## Successful Approaches
- Testing gin routes with `httptest.NewRecorder()` and `engine.ServeHTTP()` works well
- For Run() shutdown tests, use a real port and `context.WithCancel()` to trigger graceful shutdown
- Use `gin.SetMode(gin.TestMode)` in tests to suppress default log output

