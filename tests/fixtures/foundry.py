import os
import shutil
from dataclasses import dataclass
from pathlib import Path

import pytest

from fixtures.commander import Commander
from fixtures.kubectl import Kubectl


@dataclass(frozen=True)
class Foundry:
    command: Commander
    casting: Path
    pours: Path
    lock: Path

    def install(self) -> None:
        self.command.run("cast", "--no-ledger", "--file", str(self.casting), "--pours", str(self.pours), timeout=360)

    def destroy(self) -> None:
        shutil.rmtree(self.pours, ignore_errors=True)
        self.lock.unlink(missing_ok=True)


@pytest.fixture(scope="session")
def foundry(kubectl: Kubectl, repository_dir: Path, pytestconfig: pytest.Config) -> Foundry:
    tests_dir = repository_dir / "tests"
    return Foundry(
        command=Commander.from_path(
            pytestconfig.getoption("--foundry-binary-path"),
            repository_dir,
            {
                "KUBECONFIG": str(kubectl.kubeconfig),
                "PATH": str(kubectl.command.executable.parent) + os.pathsep + os.environ.get("PATH", ""),
            },
        ),
        casting=tests_dir / "casting.yaml",
        pours=tests_dir / "pours",
        lock=tests_dir / "casting.yaml.lock",
    )
