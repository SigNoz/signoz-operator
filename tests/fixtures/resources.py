import json
import logging
import re
import time
from collections.abc import Callable
from typing import Any

import pytest

from fixtures.environment import TEST_NAMESPACE
from fixtures.kubectl import Kubectl

logger = logging.getLogger(__name__)

API_VERSION = "resources.signoz.io/v1alpha1"
RESOURCE_INTERVAL = "5s"
ANNOTATION_SIGNOZ_RESOURCE_ID = "resources.signoz.io/signoz-resource-id"
RESOURCE_FINALIZER = "resources.signoz.io/finalizer"
CONDITION_TIMEOUT = 120.0
POLL_INTERVAL = 2.0


def eventually(check: Callable[[], Any], description: str, timeout: float = CONDITION_TIMEOUT) -> Any:
    deadline = time.monotonic() + timeout
    last: Any = None
    while time.monotonic() < deadline:
        last = check()
        if last:
            return last
        time.sleep(POLL_INTERVAL)

    raise AssertionError(f"timed out after {timeout}s waiting for {description}; last value was {last!r}")


def await_condition(
    kubectl: Kubectl,
    kind: str,
    name: str,
    condition: str,
    status: str,
    reason: str | None = None,
    timeout: float = CONDITION_TIMEOUT,
) -> dict[str, Any]:
    def matching() -> dict[str, Any] | None:
        resource = kubectl.get(kind, name, TEST_NAMESPACE)
        for candidate in resource.get("status", {}).get("conditions", []):
            if candidate["type"] != condition:
                continue
            if candidate["status"] != status or (reason is not None and candidate["reason"] != reason):
                return None
            return candidate
        return None

    return eventually(matching, f"{kind}/{name} condition {condition}={status} reason={reason}", timeout)


@pytest.fixture
def test_id(request: pytest.FixtureRequest) -> str:
    slug = re.sub(r"[^a-z0-9]+", "-", request.node.name.lower()).removeprefix("test-").strip("-")
    return f"{slug[:40].strip('-')}-{int(time.monotonic() * 1000) % 100000}"


@pytest.fixture
def apply(kubectl: Kubectl) -> Callable[[dict[str, Any]], dict[str, Any]]:
    """Deletes newest first, so a resource is reclaimed before the provider config it reclaims through."""
    applied: list[dict[str, Any]] = []

    def apply_manifest(manifest: dict[str, Any]) -> dict[str, Any]:
        kubectl.apply(json.dumps(manifest), TEST_NAMESPACE)
        identity = (manifest["kind"], manifest["metadata"]["name"])
        if identity not in [(m["kind"], m["metadata"]["name"]) for m in applied]:
            applied.append(manifest)
        return manifest

    yield apply_manifest

    for manifest in reversed(applied):
        try:
            kubectl.delete(json.dumps(manifest), TEST_NAMESPACE)
        except RuntimeError as err:
            logger.warning("Could not delete %s/%s: %s", manifest["kind"], manifest["metadata"]["name"], err)
