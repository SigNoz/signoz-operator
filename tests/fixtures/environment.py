import logging
from dataclasses import dataclass
from pathlib import Path

import pytest

from fixtures.foundry import Foundry
from fixtures.kind import Kind
from fixtures.kubectl import Kubectl
from fixtures.operator import Operator

logger = logging.getLogger(__name__)

SIGNOZ_NAMESPACE = "signoz"

# Where config/default installs the operator, and so where a
# ClusterProviderConfig's references resolve.
OPERATOR_NAMESPACE = "signoz-operator-system"

# The operator watches every namespace, so the tests run outside its own. One
# namespace holds them all, and a name unique to the test keeps them apart.
TEST_NAMESPACE = "signoz-operator-e2e"


@dataclass(frozen=True)
class Environment:
    kind: Kind
    foundry: Foundry
    operator: Operator
    kubectl: Kubectl
    signoz_service: Path
    ready_timeout: str

    def setup(self) -> None:
        self.kind.create()
        try:
            self.foundry.install()
            self.kubectl.apply_file(self.signoz_service)
            self.kubectl.apply_namespace(TEST_NAMESPACE)
            self.operator.deploy()
        except Exception:
            self.kubectl.collect_diagnostics(OPERATOR_NAMESPACE)
            self.destroy()
            raise

    def assert_ready(self) -> None:
        try:
            for kind, name, namespace in (
                ("deployment", "signoz-telemetrystore-clickhouse-operator", SIGNOZ_NAMESPACE),
                ("statefulset", "signoz-metastore", SIGNOZ_NAMESPACE),
                ("statefulset", "signoz-signoz", SIGNOZ_NAMESPACE),
                ("deployment", "signoz-operator", OPERATOR_NAMESPACE),
            ):
                self.kubectl.rollout_status(kind, name, namespace, self.ready_timeout)
        except RuntimeError:
            self.kubectl.collect_diagnostics(OPERATOR_NAMESPACE)
            raise

    def destroy(self) -> None:
        try:
            self.operator.destroy()
        except RuntimeError as err:
            logger.warning("could not delete the operator manifests: %s", err)
        finally:
            self.foundry.destroy()
            self.kind.destroy()


@pytest.fixture(scope="session")
def environment(
    foundry: Foundry,
    kind: Kind,
    kubectl: Kubectl,
    operator: Operator,
    repository_dir: Path,
    pytestconfig: pytest.Config,
) -> Environment:
    cache_key = "signoz_operator/e2e_environment"
    teardown = pytestconfig.getoption("--teardown")
    reuse = pytestconfig.getoption("--reuse")
    environment = Environment(
        kind=kind,
        foundry=foundry,
        operator=operator,
        kubectl=kubectl,
        signoz_service=repository_dir / "tests" / "e2e" / "signoz" / "service.yaml",
        ready_timeout=pytestconfig.getoption("--ready-timeout"),
    )
    cached = pytestconfig.cache.get(cache_key, None)

    if teardown:
        if cached is None:
            pytest.skip("no retained e2e environment found")
        if cached["cluster_name"] != kind.cluster_name:
            raise pytest.UsageError("--cluster-name must match the retained e2e environment")
        environment.destroy()
        pytestconfig.cache.set(cache_key, None)
        pytest.skip("retained e2e environment deleted")

    if reuse and cached is not None and cached["cluster_name"] == kind.cluster_name and kind.exists():
        logger.info("reusing Kind cluster %s", kind.cluster_name)
        yield environment
        return

    if reuse and cached is not None:
        logger.warning("cached e2e environment has no matching Kind cluster; creating a new one")

    environment.setup()
    if reuse:
        pytestconfig.cache.set(cache_key, {"cluster_name": kind.cluster_name})

    yield environment

    if reuse:
        logger.info("keeping Kind cluster %s because --reuse was requested", kind.cluster_name)
        return

    environment.destroy()
    pytestconfig.cache.set(cache_key, None)
