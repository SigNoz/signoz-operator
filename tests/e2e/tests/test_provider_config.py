"""A ProviderConfig reports whether its endpoint and credential resolve, not whether SigNoz answered."""

import json
from collections.abc import Callable
from typing import Any

from fixtures.kubectl import Kubectl
from fixtures.resources import API_VERSION
from fixtures.signoz import SIGNOZ_CLUSTER_URL, SigNoz


def test_ready_once_the_endpoint_and_credential_resolve(
    kubectl: Kubectl,
    namespace: str,
    signoz: SigNoz,
    test_id: str,
    provider_config: str,
    api_key_secret: tuple[str, str],
) -> None:
    """The provider_config fixture already waited for Ready; this checks what it recorded."""
    status = kubectl.get("providerconfig", provider_config, namespace)["status"]
    secret_name, _ = api_key_secret
    secret = kubectl.get("secret", secret_name, namespace)

    # The resourceVersion is recorded so a rotated Secret re-resolves. The
    # credential itself never reaches status.
    observed = status["observedRefVersions"]["Secret"][f"{namespace}/{secret_name}"]
    assert observed == secret["metadata"]["resourceVersion"]
    assert signoz.api_key not in json.dumps(status)


def test_not_ready_while_the_secret_is_missing(
    test_id: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
    await_condition: Callable[..., dict[str, Any]],
) -> None:
    name = f"no-secret-{test_id}"
    apply(
        {
            "apiVersion": API_VERSION,
            "kind": "ProviderConfig",
            "metadata": {"name": name},
            "spec": {
                "endpoint": {"value": SIGNOZ_CLUSTER_URL},
                "auth": {"header": {"valueFrom": {"secretKeyRef": {"name": "absent", "key": "token"}}}},
            },
        }
    )

    condition = await_condition("providerconfig", name, "Ready", "False", "SecretNotFound")
    assert "absent" in condition["message"]


def test_not_ready_when_the_secret_has_no_such_key(
    signoz: SigNoz,
    test_id: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
    await_condition: Callable[..., dict[str, Any]],
) -> None:
    name = f"no-key-{test_id}"
    apply(
        {
            "apiVersion": "v1",
            "kind": "Secret",
            "metadata": {"name": name},
            "stringData": {"a-different-key": signoz.api_key},
        }
    )
    apply(
        {
            "apiVersion": API_VERSION,
            "kind": "ProviderConfig",
            "metadata": {"name": name},
            "spec": {
                "endpoint": {"value": SIGNOZ_CLUSTER_URL},
                "auth": {"header": {"valueFrom": {"secretKeyRef": {"name": name, "key": "token"}}}},
            },
        }
    )

    condition = await_condition("providerconfig", name, "Ready", "False", "KeyNotFound")
    assert "token" in condition["message"]


def test_becomes_ready_when_the_missing_secret_appears(
    signoz: SigNoz,
    test_id: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
    await_condition: Callable[..., dict[str, Any]],
) -> None:
    """The Secret is watched, so creating it re-resolves the config without an edit to the config."""
    name = f"waits-{test_id}"
    apply(
        {
            "apiVersion": API_VERSION,
            "kind": "ProviderConfig",
            "metadata": {"name": name},
            "spec": {
                "endpoint": {"value": SIGNOZ_CLUSTER_URL},
                "auth": {"header": {"valueFrom": {"secretKeyRef": {"name": name, "key": "token"}}}},
            },
        }
    )
    await_condition("providerconfig", name, "Ready", "False", "SecretNotFound")

    apply(
        {
            "apiVersion": "v1",
            "kind": "Secret",
            "metadata": {"name": name},
            "stringData": {"token": signoz.api_key},
        }
    )

    await_condition("providerconfig", name, "Ready", "True", "Resolved")
