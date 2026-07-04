## What & why

<!-- What does this change do, and what problem does it solve? Link related issues. -->

## How was it tested?

<!-- go test ./..., manual steps, platforms covered. -->

## Checklist

- [ ] `go build ./... && go vet ./... && go test ./...` pass locally
- [ ] `gofmt` clean; `golangci-lint run` clean
- [ ] Docs updated if behavior changed (docs/ + wiki pages are synced by `script/sync-wiki.sh`)
- [ ] CHANGELOG.md entry added under the unreleased version (user-visible changes)
- [ ] New templates registered in `server/webui/template_names_test.go` (WebUI changes only)
