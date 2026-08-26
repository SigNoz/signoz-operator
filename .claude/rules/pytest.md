---
paths:
  - "tests/**/*.py"
---

# Python integration tests

- Do not add module-level `_helper()` functions. Inline simple test logic so a
  reader can see what the test does. Extract a helper only when it is genuinely
  non-trivial and reused across many tests, or when explicitly requested.
- Prefer fixture factories that return a callable over indirect parametrization.
  The value should be an explicit argument rather than resolved through
  `request.param`.
- Mark skipped parameter cases with `pytest.param(..., marks=pytest.mark.skip(...))`
  so they skip at collection before their fixtures can create an environment.
- Read test configuration from explicit pytest `--flags` passed by the workflow.
  Do not use environment variables as configuration fallbacks.
