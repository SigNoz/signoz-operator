from collections.abc import Callable
from typing import Any

import pytest

from fixtures.kubectl import Kubectl
from fixtures.resources import API_VERSION, await_condition
from fixtures.signoz import SIGNOZ_CLUSTER_URL, SigNoz


def provider_config_manifest(name: str, secret_name: str, secret_key: str = "token") -> dict[str, Any]:
    return {
        "apiVersion": API_VERSION,
        "kind": "ProviderConfig",
        "metadata": {"name": name},
        "spec": {
            "endpoint": {"value": SIGNOZ_CLUSTER_URL},
            "auth": {"header": {"valueFrom": {"secretKeyRef": {"name": secret_name, "key": secret_key}}}},
        },
    }


def secret_manifest(name: str, data: dict[str, str]) -> dict[str, Any]:
    return {"apiVersion": "v1", "kind": "Secret", "metadata": {"name": name}, "stringData": data}


@pytest.fixture
def api_key_secret(signoz: SigNoz, test_id: str, apply: Callable[[dict[str, Any]], dict[str, Any]]) -> str:
    name = f"signoz-api-{test_id}"
    apply(secret_manifest(name, {"token": signoz.api_key}))
    return name


@pytest.fixture
def provider_config(
    kubectl: Kubectl,
    test_id: str,
    api_key_secret: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
) -> str:
    name = f"signoz-{test_id}"
    apply(provider_config_manifest(name, api_key_secret))
    await_condition(kubectl, "providerconfig", name, "Ready", "True", "Resolved")
    return name
