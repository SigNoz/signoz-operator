---
paths:
  - "**/*.go"
---

# Comments

Do not write unnecessary comments where the code is self-explanatory. Document only non-obvious behavior, constraints, formats, and edge cases; never restate what the code already says. Rationale that belongs in prose — why a version is pinned, why a job exists, how a subsystem fits together — goes in the README or the PR, not the source.

- **Godoc**: Skip comments that merely restate the identifier. Document only non-obvious behavior, constraints, formats, and edge cases.
- Match the comment density and style of the surrounding file — don't introduce a heavier commenting style than the package already uses.
