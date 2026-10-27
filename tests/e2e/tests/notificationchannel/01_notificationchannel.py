import json
import time
from collections.abc import Callable
from http import HTTPStatus
from pathlib import Path
from typing import Any

from fixtures.environment import TEST_NAMESPACE
from fixtures.kubectl import Kubectl
from fixtures.notificationchannels import NOTIFICATION_CHANNELS_BASE_URL, notification_channel_body, notification_channel_manifest
from fixtures.resources import API_VERSION, RESOURCE_FINALIZER, RESOURCE_INTERVAL, await_condition, eventually
from fixtures.signoz import SigNoz

NOTIFICATION_CHANNEL_TESTDATA = Path(__file__).resolve().parents[3] / "testdata" / "notificationchannel.json"


def test_creates_the_notification_channel_and_records_its_id(
    kubectl: Kubectl,
    signoz: SigNoz,
    provider_config: str,
    notification_channel_name: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
) -> None:
    body = notification_channel_body(NOTIFICATION_CHANNEL_TESTDATA, notification_channel_name)
    apply(notification_channel_manifest(notification_channel_name, provider_config, body))

    await_condition(kubectl, "notificationchannel", notification_channel_name, "Ready", "True")
    await_condition(kubectl, "notificationchannel", notification_channel_name, "Synced", "True")

    resource = kubectl.get("notificationchannel", notification_channel_name, TEST_NAMESPACE)
    remote = signoz.data(f"{NOTIFICATION_CHANNELS_BASE_URL}/{resource['status']['signozResource']['id']}")
    assert remote["name"] == notification_channel_name
    assert remote["displayName"] == notification_channel_name
    assert remote["config"]["kind"] == "webhook"
    assert remote["config"]["spec"]["url"] == body["config"]["spec"]["url"]
    assert RESOURCE_FINALIZER in resource["metadata"]["finalizers"]
    assert resource["status"]["observedHash"]


def test_an_edited_spec_reaches_signoz(
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

    edited = notification_channel_body(NOTIFICATION_CHANNEL_TESTDATA, notification_channel_name, url="https://alerts.example.com/edited")
    apply(notification_channel_manifest(notification_channel_name, provider_config, edited))

    eventually(
        lambda: (
            signoz.data(f"{NOTIFICATION_CHANNELS_BASE_URL}/{channel_id}")["config"]["spec"]["url"] == "https://alerts.example.com/edited"
        ),
        f"notification channel {channel_id} to carry the edited url",
    )
    assert kubectl.get("notificationchannel", notification_channel_name, TEST_NAMESPACE)["status"]["signozResource"]["id"] == channel_id


def test_a_change_made_in_signoz_is_reverted(
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

    drifted = notification_channel_body(NOTIFICATION_CHANNEL_TESTDATA, notification_channel_name, url="https://alerts.example.com/drifted")
    assert signoz.put(f"{NOTIFICATION_CHANNELS_BASE_URL}/{channel_id}", {"config": drifted["config"]}).status_code == HTTPStatus.OK

    eventually(
        lambda: signoz.data(f"{NOTIFICATION_CHANNELS_BASE_URL}/{channel_id}")["config"]["spec"]["url"] == body["config"]["spec"]["url"],
        f"notification channel {channel_id} to be restored to the spec",
    )


def test_a_suspended_resource_is_left_alone(
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

    apply(notification_channel_manifest(notification_channel_name, provider_config, body, suspend=True))
    await_condition(kubectl, "notificationchannel", notification_channel_name, "Suspended", "True", "Suspended")
    await_condition(kubectl, "notificationchannel", notification_channel_name, "Ready", "False", "Suspended")

    drifted = notification_channel_body(
        NOTIFICATION_CHANNEL_TESTDATA, notification_channel_name, url="https://alerts.example.com/suspended"
    )
    assert signoz.put(f"{NOTIFICATION_CHANNELS_BASE_URL}/{channel_id}", {"config": drifted["config"]}).status_code == HTTPStatus.OK

    # Long enough for several resyncs, so an uncorrected drift means suspend held.
    time.sleep(4 * int(RESOURCE_INTERVAL.removesuffix("s")))
    assert signoz.data(f"{NOTIFICATION_CHANNELS_BASE_URL}/{channel_id}")["config"]["spec"]["url"] == "https://alerts.example.com/suspended"


def test_reclaim_delete_removes_the_notification_channel(
    kubectl: Kubectl,
    signoz: SigNoz,
    provider_config: str,
    notification_channel_name: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
) -> None:
    body = notification_channel_body(NOTIFICATION_CHANNEL_TESTDATA, notification_channel_name)
    apply(notification_channel_manifest(notification_channel_name, provider_config, body, reclaim_policy="Delete"))
    await_condition(kubectl, "notificationchannel", notification_channel_name, "Synced", "True")
    channel_id = kubectl.get("notificationchannel", notification_channel_name, TEST_NAMESPACE)["status"]["signozResource"]["id"]

    kubectl.run("delete", "notificationchannel", notification_channel_name, "--namespace", TEST_NAMESPACE)

    assert not kubectl.exists("notificationchannel", notification_channel_name, TEST_NAMESPACE)
    eventually(
        lambda: signoz.get(f"{NOTIFICATION_CHANNELS_BASE_URL}/{channel_id}").status_code == HTTPStatus.NOT_FOUND,
        f"notification channel {channel_id} to be deleted in SigNoz",
    )


def test_reclaim_orphan_leaves_the_notification_channel(
    kubectl: Kubectl,
    signoz: SigNoz,
    provider_config: str,
    notification_channel_name: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
) -> None:
    body = notification_channel_body(NOTIFICATION_CHANNEL_TESTDATA, notification_channel_name)
    apply(notification_channel_manifest(notification_channel_name, provider_config, body, reclaim_policy="Orphan"))
    await_condition(kubectl, "notificationchannel", notification_channel_name, "Synced", "True")
    channel_id = kubectl.get("notificationchannel", notification_channel_name, TEST_NAMESPACE)["status"]["signozResource"]["id"]

    kubectl.run("delete", "notificationchannel", notification_channel_name, "--namespace", TEST_NAMESPACE)

    assert not kubectl.exists("notificationchannel", notification_channel_name, TEST_NAMESPACE)
    assert signoz.get(f"{NOTIFICATION_CHANNELS_BASE_URL}/{channel_id}").status_code == HTTPStatus.OK


def test_an_existing_notification_channel_is_adopted(
    kubectl: Kubectl,
    signoz: SigNoz,
    provider_config: str,
    notification_channel_name: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
) -> None:
    body = notification_channel_body(NOTIFICATION_CHANNEL_TESTDATA, notification_channel_name)
    created = signoz.post(NOTIFICATION_CHANNELS_BASE_URL, body)
    assert created.status_code == HTTPStatus.CREATED

    apply(notification_channel_manifest(notification_channel_name, provider_config, body))
    await_condition(kubectl, "notificationchannel", notification_channel_name, "Synced", "True")

    assert (
        kubectl.get("notificationchannel", notification_channel_name, TEST_NAMESPACE)["status"]["signozResource"]["id"]
        == created.json()["data"]["id"]
    )


def test_a_json_spec_keeps_send_resolved_off(
    kubectl: Kubectl,
    signoz: SigNoz,
    provider_config: str,
    notification_channel_name: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
) -> None:
    body = notification_channel_body(NOTIFICATION_CHANNEL_TESTDATA, notification_channel_name)
    body["config"]["spec"]["sendResolved"] = False
    manifest = {
        "apiVersion": API_VERSION,
        "kind": "NotificationChannel",
        "metadata": {"name": notification_channel_name},
        "spec": {
            "providerConfigRef": {"name": provider_config},
            "interval": RESOURCE_INTERVAL,
            "objectTemplate": {"jsonSpec": json.dumps(body)},
        },
    }
    apply(manifest)
    await_condition(kubectl, "notificationchannel", notification_channel_name, "Synced", "True")
    channel_id = kubectl.get("notificationchannel", notification_channel_name, TEST_NAMESPACE)["status"]["signozResource"]["id"]

    assert signoz.data(f"{NOTIFICATION_CHANNELS_BASE_URL}/{channel_id}")["config"]["spec"]["sendResolved"] is False

    body["config"]["spec"]["url"] = "https://alerts.example.com/edited"
    manifest["spec"]["objectTemplate"]["jsonSpec"] = json.dumps(body)
    apply(manifest)

    eventually(
        lambda: (
            signoz.data(f"{NOTIFICATION_CHANNELS_BASE_URL}/{channel_id}")["config"]["spec"]["url"] == "https://alerts.example.com/edited"
        ),
        f"notification channel {channel_id} to carry the edited url",
    )
    assert signoz.data(f"{NOTIFICATION_CHANNELS_BASE_URL}/{channel_id}")["config"]["spec"]["sendResolved"] is False


def test_an_unparseable_json_spec_is_terminal(
    kubectl: Kubectl,
    provider_config: str,
    notification_channel_name: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
) -> None:
    apply(
        {
            "apiVersion": API_VERSION,
            "kind": "NotificationChannel",
            "metadata": {"name": notification_channel_name},
            "spec": {
                "providerConfigRef": {"name": provider_config},
                "interval": RESOURCE_INTERVAL,
                "objectTemplate": {"jsonSpec": "{not json"},
            },
        }
    )

    await_condition(kubectl, "notificationchannel", notification_channel_name, "Terminal", "True", "InvalidSpec")
    await_condition(kubectl, "notificationchannel", notification_channel_name, "Ready", "False", "InvalidSpec")
