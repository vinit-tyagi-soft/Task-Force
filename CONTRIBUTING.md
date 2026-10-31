# Contributing to TaskForce

Thanks for helping build TaskForce! This guide covers how to get set up and what a good pull request looks like.

## Ways to help

- Pick an [open issue](https://github.com/vinit-tyagi-soft/Task-Force/issues). Issues labeled `P0` matter most right now, and `good first issue` is a good place to start.
- Report bugs or suggest features with the issue templates.
- Improve docs, tests or accessibility.

For anything larger than a small fix, comment on the issue (or open one) first so we can agree on the approach before you write code.

## Development setup

Follow the [Quick start](README.md#quick-start) in the README. You need Go 1.26+, Node.js 20+ and PostgreSQL 17, plus both the `taskforce` and `taskforce_test` databases.

## Workflow

1. Fork the repository and create a branch from `main` (for example `feat/oidc-login` or `fix/calendar-timezone`).
2. Make your change with tests.
3. Run the checks:

   ```bash
   cd api && gofmt -l . && go vet ./... && go test ./... -count=1
   cd web && npx tsc --noEmit && npm run lint
   ```

4. Open a pull request against `main` and fill in the template. CI must pass before review.

## Project rules

These keep TaskForce safe for multi-tenant use. Pull requests that break them won't be merged.

- **Personal work stays private.** Personal workspace data must never appear in organization views, reports, exports, alerts, webhooks, API tokens or AI answers.
- **Every table uses RLS.** New tables need `ENABLE` and `FORCE ROW LEVEL SECURITY`, policies, and only the grants `taskforce_app` actually needs. Child rows use composite foreign keys so they stay inside their tenant or workspace.
- **Migrations only move forward.** Add a new numbered file in `api/migrations/` and never edit one that has been released.
- **Check authorization in `internal/authz`.** Return 404 for resources the caller can't read and 403 for writes they may not make.
- **Writes are audited and versioned.** Write the audit event in the same transaction, and use `If-Match` / `ETag` for updates.
- **Test the boundaries.** Integration tests in `api/internal/httpapi` run as `taskforce_app`. Add a test showing that the wrong role or the wrong tenant is refused.
- **No secrets in the repo.** Use environment variables.

## Code style

- Go: `gofmt`, standard library first, small handlers, errors wrapped with context.
- TypeScript: follow the existing components and the `useApi` / `api` helpers in `web/lib/api.ts`. ESLint must pass.
- Commit messages: a short imperative summary line (for example "Add OIDC login"), then a body if needed.

## Security issues

Please don't open public issues for vulnerabilities. Use GitHub's [private vulnerability reporting](https://github.com/vinit-tyagi-soft/Task-Force/security/advisories/new) instead.

## License

By contributing, you agree that your contributions are licensed under the [MIT License](LICENSE).
