import logging
from dataclasses import dataclass
from pathlib import Path

import pytest

from fixtures.commander import Commander

logger = logging.getLogger(__name__)


@dataclass(frozen=True)
class Kind:
    command: Commander
    cluster_name: str
    config: Path
    kubeconfig: Path

    def exists(self) -> bool:
        result = self.command.run("get", "clusters")
        return self.cluster_name in result.stdout.splitlines()

    def create(self) -> None:
        if self.exists():
            raise RuntimeError(
                f"Kind cluster {self.cluster_name!r} already exists. "
                "Use --reuse to connect to a cluster created by this suite, or delete it first."
            )

        self.kubeconfig.parent.mkdir(parents=True, exist_ok=True)
        self.kubeconfig.unlink(missing_ok=True)
        self.command.run(
            "create",
            "cluster",
            "--name",
            self.cluster_name,
            "--config",
            str(self.config),
            "--kubeconfig",
            str(self.kubeconfig),
            "--wait",
            "5m",
        )

    def load_image(self, image: str) -> None:
        self.command.run("load", "docker-image", image, "--name", self.cluster_name, timeout=300)

    def destroy(self) -> None:
        logger.info("deleting Kind cluster %s", self.cluster_name)
        try:
            self.command.run("delete", "cluster", "--name", self.cluster_name, "--kubeconfig", str(self.kubeconfig))
        finally:
            self.kubeconfig.unlink(missing_ok=True)
            try:
                self.kubeconfig.parent.rmdir()
            except OSError:
                pass


@pytest.fixture(scope="session")
def kind(repository_dir: Path, pytestconfig: pytest.Config) -> Kind:
    tests_dir = repository_dir / "tests"
    return Kind(
        command=Commander.from_path(pytestconfig.getoption("--kind-binary-path"), repository_dir),
        cluster_name=pytestconfig.getoption("--cluster-name"),
        config=tests_dir / "kind.yaml",
        kubeconfig=tests_dir / "tmp" / "kubeconfig",
    )
