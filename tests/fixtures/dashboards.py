import json
import logging
from http import HTTPStatus
from pathlib import Path
from typing import Any

import pytest

from fixtures.resources import API_VERSION, RESOURCE_INTERVAL
from fixtures.signoz import SigNoz

logger = logging.getLogger(__name__)

DASHBOARDS_BASE_URL = "/api/v2/dashboards"


def dashboard_body(path: Path, name: str, display_name: str | None = None, tags: list[dict[str, str]] | None = None) -> dict[str, Any]:
    body = json.loads(path.read_text())
    body["name"] = name
    body["spec"]["display"]["name"] = display_name or name
    if tags is not None:
        body["tags"] = tags
    return body


def dashboard_manifest(
    name: str,
    provider_config: str,
    body: dict[str, Any],
    reclaim_policy: str = "Delete",
    suspend: bool = False,
    annotations: dict[str, str] | None = None,
) -> dict[str, Any]:
    return {
        "apiVersion": API_VERSION,
        "kind": "Dashboard",
        "metadata": {"name": name, "annotations": annotations or {}},
        "spec": {
            "providerConfigRef": {"name": provider_config},
            "interval": RESOURCE_INTERVAL,
            "reclaimPolicy": reclaim_policy,
            "suspend": suspend,
            "objectTemplate": {"spec": body},
        },
    }


@pytest.fixture
def dashboard_name(signoz: SigNoz, test_id: str) -> str:
    """Deletes the SigNoz dashboard of that name at teardown, since an orphaned or adopted one outlives its resource."""
    name = f"e2e-{test_id}"

    yield name

    listing = signoz.get(f"{DASHBOARDS_BASE_URL}?limit=100&offset=0")
    if listing.status_code != HTTPStatus.OK:
        logger.warning("Could not list dashboards to reclaim: %s", listing.text)
        return

    for dashboard in listing.json()["data"]["dashboards"]:
        if dashboard["name"] == name:
            signoz.delete(f"{DASHBOARDS_BASE_URL}/{dashboard['id']}")
