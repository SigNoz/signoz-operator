from collections.abc import Callable
from http import HTTPStatus
from pathlib import Path
from typing import Any

from fixtures.environment import TEST_NAMESPACE
from fixtures.kubectl import Kubectl
from fixtures.notificationchannels import NOTIFICATION_CHANNELS_BASE_URL, notification_channel_body
from fixtures.resources import RESOURCE_INTERVAL, await_condition, eventually
from fixtures.rules import RULES_BASE_URL, rule_body, rule_manifest
from fixtures.signoz import SigNoz

TESTDATA = Path(__file__).resolve().parents[3] / "testdata"


def test_an_unchanged_rule_settles_in_sync(
    kubectl: Kubectl,
    signoz: SigNoz,
    provider_config: str,
    notification_channel_name: str,
    rule_name: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
) -> None:
    channel = notification_channel_body(TESTDATA / "notificationchannel.json", notification_channel_name)
    assert signoz.post(NOTIFICATION_CHANNELS_BASE_URL, channel).status_code == HTTPStatus.CREATED

    apply(rule_manifest(rule_name, provider_config, rule_body(TESTDATA / "rule.json", rule_name, notification_channel_name)))
    await_condition(kubectl, "rule", rule_name, "Synced", "True")

    # The first resync after the create finds nothing to change, unless the operator sees drift that is not there.
    await_condition(kubectl, "rule", rule_name, "Synced", "True", "Synced", timeout=6 * int(RESOURCE_INTERVAL.removesuffix("s")))


def test_a_change_made_in_signoz_to_an_omitted_field_is_reverted(
    kubectl: Kubectl,
    signoz: SigNoz,
    provider_config: str,
    notification_channel_name: str,
    rule_name: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
) -> None:
    channel = notification_channel_body(TESTDATA / "notificationchannel.json", notification_channel_name)
    assert signoz.post(NOTIFICATION_CHANNELS_BASE_URL, channel).status_code == HTTPStatus.CREATED

    body = rule_body(TESTDATA / "rule.json", rule_name, notification_channel_name)
    apply(rule_manifest(rule_name, provider_config, body))
    await_condition(kubectl, "rule", rule_name, "Synced", "True")
    rule_id = kubectl.get("rule", rule_name, TEST_NAMESPACE)["status"]["signozResource"]["id"]

    assert "disabled" not in body
    assert signoz.put(f"{RULES_BASE_URL}/{rule_id}", {**body, "disabled": True}).status_code == HTTPStatus.NO_CONTENT
    assert signoz.data(f"{RULES_BASE_URL}/{rule_id}")["disabled"] is True

    eventually(
        lambda: signoz.data(f"{RULES_BASE_URL}/{rule_id}")["disabled"] is False,
        f"rule {rule_id} to be enabled again",
    )
