import copy
import json
import logging
import re
import time
from collections.abc import Callable
from pathlib import Path
from typing import Any

import pytest

from fixtures.environment import TEST_NAMESPACE
from fixtures.kubectl import Kubectl
from fixtures.resources import API_VERSION, RESOURCE_INTERVAL
from fixtures.signoz import SIGNOZ_CLUSTER_URL, SigNoz

logger = logging.getLogger(__name__)

# Long enough for the operator to notice a change and finish a round trip to
# SigNoz, short enough that a genuine failure does not stall the suite.
CONDITION_TIMEOUT = 120.0
POLL_INTERVAL = 2.0


@pytest.fixture(scope="session")
def testdata_dir(repository_dir: Path) -> Path:
    return repository_dir / "tests" / "testdata"


@pytest.fixture
def namespace() -> str:
    """The namespace the tests' resources live in.

    The shipped deployment watches every namespace and binds a ClusterRole, so
    this is deliberately not the operator's own namespace. Tests share it and
    are isolated by a name unique to the test.
    """
    return TEST_NAMESPACE


@pytest.fixture
def test_id(request: pytest.FixtureRequest) -> str:
    """A short slug unique to this test, so resources sharing a namespace cannot collide."""
    slug = re.sub(r"[^a-z0-9]+", "-", request.node.name.lower()).removeprefix("test-").strip("-")
    return f"{slug[:40].strip('-')}-{int(time.monotonic() * 1000) % 100000}"


@pytest.fixture
def apply(kubectl: Kubectl, namespace: str) -> Callable[[dict[str, Any]], dict[str, Any]]:
    """Applies a manifest and deletes it at teardown, newest first.

    Reverse order matters: a mirrored resource is removed before the provider
    config it reclaims through, so its finalizer can still resolve a connection.
    """
    applied: list[dict[str, Any]] = []

    def apply_manifest(manifest: dict[str, Any]) -> dict[str, Any]:
        kubectl.apply(json.dumps(manifest), namespace)
        identity = (manifest["kind"], manifest["metadata"]["name"])
        if identity not in [(m["kind"], m["metadata"]["name"]) for m in applied]:
            applied.append(manifest)
        return manifest

    yield apply_manifest

    for manifest in reversed(applied):
        try:
            kubectl.delete(json.dumps(manifest), namespace)
        except RuntimeError as err:
            logger.warning("could not delete %s/%s: %s", manifest["kind"], manifest["metadata"]["name"], err)


@pytest.fixture
def eventually() -> Callable[..., Any]:
    """Polls until a check returns a truthy value, and reports the last one it saw when it does not."""

    def poll(check: Callable[[], Any], description: str, timeout: float = CONDITION_TIMEOUT) -> Any:
        deadline = time.monotonic() + timeout
        last: Any = None
        while time.monotonic() < deadline:
            last = check()
            if last:
                return last
            time.sleep(POLL_INTERVAL)

        raise AssertionError(f"timed out after {timeout}s waiting for {description}; last value was {last!r}")

    return poll


@pytest.fixture
def await_condition(kubectl: Kubectl, namespace: str, eventually: Callable[..., Any]) -> Callable[..., dict[str, Any]]:
    """Waits for one condition on a resource to reach a status, and optionally a reason."""

    def wait(
        kind: str,
        name: str,
        condition: str,
        status: str,
        reason: str | None = None,
        timeout: float = CONDITION_TIMEOUT,
    ) -> dict[str, Any]:
        def matching() -> dict[str, Any] | None:
            resource = kubectl.get(kind, name, namespace)
            for candidate in resource.get("status", {}).get("conditions", []):
                if candidate["type"] != condition:
                    continue
                if candidate["status"] != status:
                    return None
                if reason is not None and candidate["reason"] != reason:
                    return None
                return candidate
            return None

        described = f"{kind}/{name} condition {condition}={status}"
        if reason is not None:
            described += f" with reason {reason}"

        return eventually(matching, described, timeout)

    return wait


@pytest.fixture
def api_key_secret(
    signoz: SigNoz,
    test_id: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
) -> tuple[str, str]:
    """A Secret holding the service account key, and the key within it."""
    name = f"signoz-api-{test_id}"
    apply(
        {
            "apiVersion": "v1",
            "kind": "Secret",
            "metadata": {"name": name},
            "stringData": {"token": signoz.api_key},
        }
    )
    return name, "token"


@pytest.fixture
def provider_config(
    test_id: str,
    api_key_secret: tuple[str, str],
    apply: Callable[[dict[str, Any]], dict[str, Any]],
    await_condition: Callable[..., dict[str, Any]],
) -> str:
    """A ready ProviderConfig pointing the operator at the in-cluster SigNoz."""
    name = f"signoz-{test_id}"
    secret_name, secret_key = api_key_secret
    apply(
        {
            "apiVersion": API_VERSION,
            "kind": "ProviderConfig",
            "metadata": {"name": name},
            "spec": {
                "endpoint": {"value": SIGNOZ_CLUSTER_URL},
                "auth": {"header": {"valueFrom": {"secretKeyRef": {"name": secret_name, "key": secret_key}}}},
            },
        }
    )

    await_condition("providerconfig", name, "Ready", "True", "Resolved")
    return name


@pytest.fixture
def dashboard_name(test_id: str) -> str:
    """A dashboard name unique to this test, since SigNoz objects outlive the cluster resources."""
    return f"e2e-{test_id}"


@pytest.fixture
def apply_dashboard(
    provider_config: str,
    testdata_dir: Path,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
) -> Callable[..., dict[str, Any]]:
    """Applies a Dashboard whose SigNoz body comes from testdata, and returns the object template it sent.

    Reapplying the same name edits the resource in place, which is how the update
    and drift cases change desired state.
    """
    template = json.loads((testdata_dir / "dashboard.json").read_text())

    def apply_dashboard_manifest(
        name: str,
        display_name: str | None = None,
        tags: list[dict[str, str]] | None = None,
        reclaim_policy: str = "Delete",
        suspend: bool = False,
        annotations: dict[str, str] | None = None,
    ) -> dict[str, Any]:
        object_spec = copy.deepcopy(template)
        object_spec["name"] = name
        object_spec["spec"]["display"]["name"] = display_name or name
        if tags is not None:
            object_spec["tags"] = tags

        metadata: dict[str, Any] = {"name": name}
        if annotations:
            metadata["annotations"] = annotations

        apply(
            {
                "apiVersion": API_VERSION,
                "kind": "Dashboard",
                "metadata": metadata,
                "spec": {
                    "providerConfigRef": {"name": provider_config},
                    "interval": RESOURCE_INTERVAL,
                    "reclaimPolicy": reclaim_policy,
                    "suspend": suspend,
                    "objectTemplate": {"spec": object_spec},
                },
            }
        )
        return object_spec

    return apply_dashboard_manifest


@pytest.fixture
def reclaim_dashboards(signoz: SigNoz, dashboard_name: str) -> None:
    """Deletes anything the test left in SigNoz, which deleting cluster resources cannot reach."""
    yield
    listing = signoz.get("/api/v2/dashboards?limit=100&offset=0")
    if listing.status_code != 200:
        logger.warning("could not list dashboards to reclaim: %s", listing.text)
        return

    for dashboard in listing.json()["data"]["dashboards"]:
        if dashboard["name"] == dashboard_name:
            signoz.delete(f"/api/v2/dashboards/{dashboard['id']}")
