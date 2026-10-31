## What and why

Closes #

## How I tested it

- [ ] `go test ./... -count=1` (api)
- [ ] `npx tsc --noEmit && npm run lint` (web)

## Checklist

- [ ] New tables have RLS policies and minimal grants
- [ ] Personal workspace data stays private
- [ ] Writes are audited and use `If-Match` where they update
- [ ] Tests cover the permission boundary (wrong role or wrong tenant is refused)
- [ ] README updated if behavior or setup changed
