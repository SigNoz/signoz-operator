---
paths:
  - "**/*.py"
---

# Python comments

Comment only non-obvious behavior, constraints, formats, and edge cases. Rationale (why a version is pinned, why a job exists, how the suite fits together) goes in the README or the PR, not the source.

- **Names carry the meaning.** If a docstring is needed to explain what a function does, fix the name.
- **No file-level docstring.** The filename says what the module is for.
- **Docstrings only when they say something the name and signature don't.** A line, not a narrative.
- **No narration.** Comment the constraint or the edge case, not what the next line does.
