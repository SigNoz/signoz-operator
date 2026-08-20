---
paths:
  - "**/*.go"
---

# Comments

Comment only where it makes sense: non-obvious behavior, constraints, formats, and edge cases. Never restate what the code already says. Rationale that belongs in prose — why a version is pinned, why a job exists, how a subsystem fits together — goes in the README, docs/, or the PR, not the source.

- **No file-level docs, ever.** A file never opens with a banner or overview comment.
- **Keep comments small and concise, free of prose** — a line or two, no essays, no narration.
- **Godoc**: Skip comments that merely restate the identifier, including scaffold boilerplate like "Xxx reconciles a Xxx object" or "SetupWithManager sets up the controller with the Manager".
- Match the comment density and style of the surrounding file — don't introduce a heavier commenting style than the package already uses.
