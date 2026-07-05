# AGENTS.md

## Repository Expectations

- Keep `README.md` in sync with every code change.
- Add concise Chinese comments only where the code is not self-explanatory.
- Use the project's existing logging facilities for error handling, important control flow, and performance-sensitive paths; avoid noisy logs.

## Validation

- After backend changes, run `go test ./test/... -v` when feasible.
- After frontend changes under `vue-frontend/`, run `npm run lint` in `vue-frontend/`.
- If you skip a validation step, explain why in the final handoff.

## Architecture Guardrails

- Preserve the repository's layered dependency rules: `interfaces -> application -> domain`, `infrastructure -> domain`, and keep framework dependencies out of `internal/domain`.
- Treat `cmd/mcp` as a separate Go module; avoid coupling it to the root module unless the task explicitly requires it.
