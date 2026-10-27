import json
from collections.abc import Callable
from typing import Any

from fixtures.environment import TEST_NAMESPACE
from fixtures.kubectl import Kubectl
from fixtures.providerconfig import provider_config_manifest, secret_manifest
from fixtures.resources import await_condition
from fixtures.signoz import SigNoz


def test_ready_records_the_secret_version_and_not_the_credential(
    kubectl: Kubectl,
    signoz: SigNoz,
    provider_config: str,
    api_key_secret: str,
) -> None:
    status = kubectl.get("providerconfig", provider_config, TEST_NAMESPACE)["status"]
    secret = kubectl.get("secret", api_key_secret, TEST_NAMESPACE)

    assert status["observedRefVersions"]["Secret"][f"{TEST_NAMESPACE}/{api_key_secret}"] == secret["metadata"]["resourceVersion"]
    assert signoz.api_key not in json.dumps(status)


def test_not_ready_while_the_secret_is_missing(
    kubectl: Kubectl,
    test_id: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
) -> None:
    name = f"no-secret-{test_id}"
    apply(provider_config_manifest(name, "absent"))

    condition = await_condition(kubectl, "providerconfig", name, "Ready", "False", "SecretNotFound")
    assert "absent" in condition["message"]


def test_not_ready_when_the_secret_has_no_such_key(
    kubectl: Kubectl,
    signoz: SigNoz,
    test_id: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
) -> None:
    name = f"no-key-{test_id}"
    apply(secret_manifest(name, {"a-different-key": signoz.api_key}))
    apply(provider_config_manifest(name, name))

    condition = await_condition(kubectl, "providerconfig", name, "Ready", "False", "KeyNotFound")
    assert "token" in condition["message"]


def test_becomes_ready_when_the_missing_secret_appears(
    kubectl: Kubectl,
    signoz: SigNoz,
    test_id: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
) -> None:
    name = f"waits-{test_id}"
    apply(provider_config_manifest(name, name))
    await_condition(kubectl, "providerconfig", name, "Ready", "False", "SecretNotFound")

    apply(secret_manifest(name, {"token": signoz.api_key}))

    await_condition(kubectl, "providerconfig", name, "Ready", "True", "Resolved")
