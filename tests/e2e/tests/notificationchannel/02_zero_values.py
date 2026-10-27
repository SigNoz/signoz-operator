from collections.abc import Callable
from pathlib import Path
from typing import Any

from fixtures.environment import TEST_NAMESPACE
from fixtures.kubectl import Kubectl
from fixtures.notificationchannels import NOTIFICATION_CHANNELS_BASE_URL, notification_channel_body, notification_channel_manifest
from fixtures.resources import await_condition
from fixtures.signoz import SigNoz

NOTIFICATION_CHANNEL_TESTDATA = Path(__file__).resolve().parents[3] / "testdata" / "notificationchannel_zero_values.json"


def test_a_false_send_resolved_reaches_signoz(
    kubectl: Kubectl,
    signoz: SigNoz,
    provider_config: str,
    notification_channel_name: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
) -> None:
    body = notification_channel_body(NOTIFICATION_CHANNEL_TESTDATA, notification_channel_name)
    apply(notification_channel_manifest(notification_channel_name, provider_config, body))
    await_condition(kubectl, "notificationchannel", notification_channel_name, "Synced", "True")

    channel_id = kubectl.get("notificationchannel", notification_channel_name, TEST_NAMESPACE)["status"]["signozResource"]["id"]
    remote = signoz.data(f"{NOTIFICATION_CHANNELS_BASE_URL}/{channel_id}")
    assert remote["config"]["spec"]["sendResolved"] is False
