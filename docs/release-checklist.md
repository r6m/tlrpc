# Release Checklist

Use this checklist before tagging a release.

## Pre-tag checks

```bash
gofmt -w .
go vet ./...
go test -count=1 ./...
make lint
go test ./compat -run Scenario -count=1 -timeout 60s
```

Optional (not required):

```bash
go test -race ./...
```

## Tag and publish

```bash
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

Create a GitHub Release using notes from `CHANGELOG.md`.
