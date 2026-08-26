---
paths:
  - "**/*.go"
---

# Go checks before opening a PR

For any change touching Go, run the primus checks locally **before opening a PR**. All six must pass.

```sh
make checks
```

After the two `controllergen` runs, the working tree must be clean — CI runs `git-diff` after each and fails the `objects` / `manifests` jobs on any generated-file drift. If they produce a diff, commit it.

Run them through primus, not bare `golangci-lint` / `go test` / `controller-gen` — the targets pin the versions and flags CI uses. Needs `PRIMUS_HOME` set; if it's unset, use the **primus-setter** skill (check first — don't re-clone if it's already there).
