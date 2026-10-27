from dataclasses import dataclass
from pathlib import Path

import pytest

from fixtures.commander import Commander
from fixtures.kind import Kind
from fixtures.kubectl import Kubectl

OPERATOR_IMAGE = "signoz-operator-e2e:latest"


@dataclass(frozen=True)
class Operator:
    docker: Commander
    kind: Kind
    kubectl: Kubectl
    overlay: Path

    def deploy(self) -> None:
        self.docker.run("build", "--tag", OPERATOR_IMAGE, ".", timeout=900)
        self.kind.load_image(OPERATOR_IMAGE)
        self.kubectl.apply_kustomize(self.overlay)

    def destroy(self) -> None:
        self.kubectl.delete_kustomize(self.overlay)


@pytest.fixture(scope="session")
def operator(kind: Kind, kubectl: Kubectl, repository_dir: Path, pytestconfig: pytest.Config) -> Operator:
    return Operator(
        docker=Commander.from_path(pytestconfig.getoption("--docker-binary-path"), repository_dir),
        kind=kind,
        kubectl=kubectl,
        overlay=repository_dir / "tests" / "e2e" / "operator",
    )
