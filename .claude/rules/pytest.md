---
paths:
  - "tests/**/*.py"
---

# pytest conventions

For the e2e suite under `tests/`. Setup, running, and layout live in [`tests/README.md`](../../tests/README.md).

- **No `_`-prefixed helper functions in test modules.** A reader must see what a test does in its body alone. Inline the logic; repetition across tests is cheaper than indirection. Shared setup or assertions go in `tests/fixtures/`.
- **Fixtures live in `tests/fixtures/`.** Never in test modules or suite `conftest.py` files. Reuse what's there first; register a new module in `tests/conftest.py` `pytest_plugins`.
- **Fixture only when there is a lifecycle; otherwise a plain function.** A fixture owns setup/teardown (`apply` deleting what it applied) or provisions a resource (the cluster, a ready `ProviderConfig`). A stateless builder or lookup (`dashboard_manifest`, `await_condition`) is a plain importable function in the matching `tests/fixtures/` module.
- **Fixtures own their cleanup.** Seed and teardown go together in one fixture (`yield`, then clean up), so tests sharing the namespace don't interfere.
- **Tests own their testdata paths.** A fixture or helper that reads `tests/testdata/` takes the file path as an argument; the test module decides which file. Never hard-code a testdata path in `tests/fixtures/`, so adding testdata never means editing a fixture.
- **Fixture-factory over indirect parametrization.** A fixture that yields a callable is clearer than `indirect=True` + `request.param`.
- **Skip at collection, not inside the test body.** Use `pytest.param(..., marks=pytest.mark.skip(reason="…"))` so the case short-circuits before its fixtures bring up an environment.
- **Test config comes from explicit `--flags`, not the environment.** Declare options in `tests/conftest.py` `pytest_addoption`; no `os.environ` fallbacks.
- **Name suite files with a two-digit prefix (`NN_*.py`)** under `tests/e2e/tests/<suite>/`. `pyproject.toml` collects only `[0-9][0-9]_*.py` and `setup.py`; anything else is silently skipped.
- **Assert exact status codes** with `http.HTTPStatus`. SigNoz answers an unrouted path with `200` and its web app, so `< 400` passes on a wrong route.
