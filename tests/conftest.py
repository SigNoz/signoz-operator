import pytest

pytest_plugins = [
    "fixtures.commander",
    "fixtures.kind",
    "fixtures.kubectl",
    "fixtures.foundry",
    "fixtures.operator",
    "fixtures.signoz",
    "fixtures.environment",
]


def pytest_addoption(parser: pytest.Parser) -> None:
    parser.addoption(
        "--reuse",
        action="store_true",
        default=False,
        help="Keep the Kind cluster after the test and reuse it on the next --reuse run.",
    )
    parser.addoption(
        "--teardown",
        action="store_true",
        default=False,
        help="Delete the Kind cluster retained by a previous --reuse run.",
    )
    parser.addoption(
        "--cluster-name",
        action="store",
        default="signoz-operator-test-e2e",
        help="Name of the Kind cluster managed by this test suite.",
    )
    parser.addoption(
        "--kind-binary-path",
        action="store",
        default="kind",
        help="Path to the kind executable.",
    )
    parser.addoption(
        "--kubectl-binary-path",
        action="store",
        default="kubectl",
        help="Path to the kubectl executable used by the test and foundryctl.",
    )
    parser.addoption(
        "--docker-binary-path",
        action="store",
        default="docker",
        help="Path to the Docker executable used to build the operator image.",
    )
    parser.addoption(
        "--foundry-binary-path",
        action="store",
        default="foundryctl",
        help="Path to the foundryctl executable used to install SigNoz.",
    )
    parser.addoption(
        "--ready-timeout",
        action="store",
        default="15m",
        help="Timeout passed to kubectl rollout status while waiting for workloads.",
    )


def pytest_configure(config: pytest.Config) -> None:
    if config.getoption("--reuse") and config.getoption("--teardown"):
        raise pytest.UsageError("--reuse and --teardown cannot be combined")
