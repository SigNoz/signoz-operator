import json
import logging
from http import HTTPStatus
from pathlib import Path
from typing import Any

import pytest

from fixtures.resources import API_VERSION, RESOURCE_INTERVAL
from fixtures.signoz import SigNoz

logger = logging.getLogger(__name__)

NOTIFICATION_CHANNELS_BASE_URL = "/api/v2/notification_channels"


def notification_channel_body(path: Path, name: str, url: str | None = None) -> dict[str, Any]:
    """displayName is set explicitly: it is immutable, and SigNoz defaults an omitted one to name."""
    body = json.loads(path.read_text())
    body["name"] = name
    body["displayName"] = name
    if url is not None:
        body["config"]["spec"]["url"] = url
    return body


def notification_channel_manifest(
    name: str,
    provider_config: str,
    body: dict[str, Any],
    reclaim_policy: str = "Delete",
    suspend: bool = False,
) -> dict[str, Any]:
    return {
        "apiVersion": API_VERSION,
        "kind": "NotificationChannel",
        "metadata": {"name": name},
        "spec": {
            "providerConfigRef": {"name": provider_config},
            "interval": RESOURCE_INTERVAL,
            "reclaimPolicy": reclaim_policy,
            "suspend": suspend,
            "objectTemplate": {"spec": body},
        },
    }


@pytest.fixture
def notification_channel_name(signoz: SigNoz, test_id: str) -> str:
    """Deletes the SigNoz channel of that name at teardown, since an orphaned or adopted one outlives its resource."""
    name = f"e2e-{test_id}"

    yield name

    listing = signoz.get(f"{NOTIFICATION_CHANNELS_BASE_URL}?limit=200&offset=0")
    if listing.status_code != HTTPStatus.OK:
        logger.warning("Could not list notification channels to reclaim: %s", listing.text)
        return

    for channel in listing.json()["data"]["channels"]:
        if channel["name"] == name:
            signoz.delete(f"{NOTIFICATION_CHANNELS_BASE_URL}/{channel['id']}")
