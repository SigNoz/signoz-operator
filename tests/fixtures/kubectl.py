import json
import logging
import subprocess
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import pytest

from fixtures.commander import Commander
from fixtures.kind import Kind

logger = logging.getLogger(__name__)


@dataclass(frozen=True)
class Kubectl:
    command: Commander
    kubeconfig: Path

    def run(self, *args: str, stdin: str | None = None) -> subprocess.CompletedProcess[str]:
        return self.command.run("--kubeconfig", str(self.kubeconfig), *args, stdin=stdin)

    def apply_kustomize(self, path: Path) -> None:
        self.run("apply", "--kustomize", str(path))

    def delete_kustomize(self, path: Path) -> None:
        self.run("delete", "--ignore-not-found", "--kustomize", str(path))

    def apply_file(self, path: Path) -> None:
        self.run("apply", "--filename", str(path))

    def delete_file(self, path: Path) -> None:
        self.run("delete", "--ignore-not-found", "--filename", str(path))

    def apply(self, manifest: str, namespace: str) -> None:
        self.run("apply", "--namespace", namespace, "--filename", "-", stdin=manifest)

    def delete(self, manifest: str, namespace: str, wait: bool = True) -> None:
        self.run(
            "delete",
            "--ignore-not-found",
            f"--wait={str(wait).lower()}",
            "--namespace",
            namespace,
            "--filename",
            "-",
            stdin=manifest,
        )

    def get(self, kind: str, name: str, namespace: str) -> dict[str, Any]:
        result = self.run("get", kind, name, "--namespace", namespace, "--output", "json")
        return json.loads(result.stdout)

    def exists(self, kind: str, name: str, namespace: str) -> bool:
        try:
            self.get(kind, name, namespace)
        except RuntimeError:
            return False
        return True

    def patch(self, kind: str, name: str, namespace: str, patch: dict[str, Any]) -> None:
        self.run("patch", kind, name, "--namespace", namespace, "--type", "merge", "--patch", json.dumps(patch))

    def apply_namespace(self, namespace: str) -> None:
        """Creates a namespace, or leaves the one a --reuse run left behind alone."""
        manifest = {"apiVersion": "v1", "kind": "Namespace", "metadata": {"name": namespace}}
        self.run("apply", "--filename", "-", stdin=json.dumps(manifest))

    def rollout_status(self, kind: str, name: str, namespace: str, timeout: str) -> None:
        self.run("rollout", "status", f"{kind}/{name}", "--namespace", namespace, f"--timeout={timeout}")

    def collect_diagnostics(self, operator_namespace: str) -> None:
        for args in (
            ("get", "pods", "--all-namespaces", "--output", "wide"),
            ("get", "events", "--all-namespaces", "--sort-by=.lastTimestamp"),
            ("logs", "deployment/signoz-operator", "--namespace", operator_namespace, "--all-containers=true"),
        ):
            try:
                result = self.run(*args)
            except RuntimeError as err:
                logger.warning("could not collect diagnostics with kubectl %s: %s", " ".join(args), err)
            else:
                logger.error("kubectl %s:\n%s", " ".join(args), result.stdout)


@pytest.fixture(scope="session")
def kubectl(kind: Kind, repository_dir: Path, pytestconfig: pytest.Config) -> Kubectl:
    command = Commander.from_path(
        pytestconfig.getoption("--kubectl-binary-path"),
        repository_dir,
        {"KUBECONFIG": str(kind.kubeconfig)},
    )
    return Kubectl(command=command, kubeconfig=kind.kubeconfig)
