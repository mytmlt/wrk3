## Summary

<!-- One paragraph: what changes and why. -->

## Verification

<!-- Paste evidence: go build/vet/test logs, wrk3 commands, status output. -->

```console
$
```

## Docs / changelog

- [ ] `README.md` / `docs/` updated (if user-facing)
- [ ] `CHANGELOG.md` `[Unreleased]` entry added

## Checklist

- [ ] `go build ./... && go vet ./... && go test ./... -count=1` green
- [ ] `ocr review --from origin/main --to <branch> --audience agent` run; every finding fixed or justified (note outcome)
- [ ] New Source/Runner types registered in `registry.go` (if any)
- [ ] No secrets, absolute personal paths, or local-only configs committed
