# Contributing

1. Fork the repository and work on a feature branch.
2. Do not commit OAuth credentials, tokens, phone numbers, SMS security codes, or private logs.
3. Run `go test ./...`, `go vet ./...`, and `go test -race ./...` before opening a pull request.
4. Keep the project dependency-light, transparent, and auditable. Avoid packers, obfuscation, hidden persistence, telemetry, and silent privilege elevation.
5. Security-sensitive changes should include tests and a short threat-model note in the pull request.
