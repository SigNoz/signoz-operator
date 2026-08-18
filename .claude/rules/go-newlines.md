---
paths:
  - "**/*.go"
---

# Newlines before logical statements (Go)

Leave a blank line before each logical statement inside a function body — `if`, `for`, `switch`, `return`, and a new local declaration that starts a fresh step. Adjacent lines that are part of the same step stay together. The goal: each step reads as its own paragraph.

```go
func example() ([]Item, error) {
	cfg, err := load()
	if err != nil {
		return nil, err
	}

	items := make([]Item, 0, len(cfg.Raw))
	for _, r := range cfg.Raw {
		items = append(items, convert(r))
	}

	return items, nil
}
```

A call and the error check that belongs to it are the same step, so they stay adjacent; the blank line goes before the next step.

## Constructor + closure registration

When registering things via a constructor and a closure — e.g. cobra `AddCommand(newLeaf(name, desc, func(...) error { ... }))` — keep the constructor arguments and the closure signature on one line; don't add redundant per-argument line breaks. Only the closure body statement(s) go on their own lines.

```go
sync.AddCommand(newLeaf("bootstrap", "Create migration-tracking tables", func(ctx context.Context, m *migration.Migrator) error {
	return m.Bootstrap(ctx)
}))
```
