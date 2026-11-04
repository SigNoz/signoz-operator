import json
import logging
from http import HTTPStatus
from pathlib import Path
from typing import Any

import pytest

from fixtures.resources import API_VERSION, RESOURCE_INTERVAL
from fixtures.signoz import SigNoz

logger = logging.getLogger(__name__)

RULES_BASE_URL = "/api/v2/rules"


def rule_body(path: Path, name: str, channel: str) -> dict[str, Any]:
    body = json.loads(path.read_text())
    body["alert"] = name
    for threshold in body["condition"]["thresholds"]["spec"]:
        threshold["channels"] = [channel]
    return body


def rule_manifest(
    name: str,
    provider_config: str,
    body: dict[str, Any],
    reclaim_policy: str = "Delete",
    suspend: bool = False,
) -> dict[str, Any]:
    return {
        "apiVersion": API_VERSION,
        "kind": "Rule",
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
def rule_name(signoz: SigNoz, test_id: str) -> str:
    """Deletes the SigNoz rule of that name at teardown, since an orphaned or adopted one outlives its resource."""
    name = f"e2e-{test_id}"

    yield name

    listing = signoz.get(RULES_BASE_URL)
    if listing.status_code != HTTPStatus.OK:
        logger.warning("Could not list rules to reclaim: %s", listing.text)
        return

    for rule in listing.json()["data"]:
        if rule["alert"] == name:
            signoz.delete(f"{RULES_BASE_URL}/{rule['id']}")
