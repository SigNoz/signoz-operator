import copy
import time
from collections.abc import Callable
from http import HTTPStatus
from pathlib import Path
from typing import Any

from fixtures.dashboards import DASHBOARDS_BASE_URL, dashboard_body, dashboard_manifest
from fixtures.environment import TEST_NAMESPACE
from fixtures.kubectl import Kubectl
from fixtures.resources import (
    ANNOTATION_SIGNOZ_RESOURCE_ID,
    API_VERSION,
    RESOURCE_FINALIZER,
    RESOURCE_INTERVAL,
    await_condition,
    eventually,
)
from fixtures.signoz import SigNoz

DASHBOARD_TESTDATA = Path(__file__).resolve().parents[3] / "testdata" / "dashboard.json"


def test_creates_the_dashboard_and_records_its_id(
    kubectl: Kubectl,
    signoz: SigNoz,
    provider_config: str,
    dashboard_name: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
) -> None:
    body = dashboard_body(DASHBOARD_TESTDATA, dashboard_name)
    apply(dashboard_manifest(dashboard_name, provider_config, body))

    await_condition(kubectl, "dashboard", dashboard_name, "Ready", "True")
    await_condition(kubectl, "dashboard", dashboard_name, "Synced", "True")

    resource = kubectl.get("dashboard", dashboard_name, TEST_NAMESPACE)
    remote = signoz.data(f"{DASHBOARDS_BASE_URL}/{resource['status']['signozResource']['id']}")
    assert remote["name"] == dashboard_name
    assert remote["spec"]["display"]["name"] == body["spec"]["display"]["name"]
    assert remote["tags"] == body["tags"]
    assert RESOURCE_FINALIZER in resource["metadata"]["finalizers"]
    assert resource["status"]["observedHash"]


def test_an_edited_spec_reaches_signoz(
    kubectl: Kubectl,
    signoz: SigNoz,
    provider_config: str,
    dashboard_name: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
) -> None:
    apply(dashboard_manifest(dashboard_name, provider_config, dashboard_body(DASHBOARD_TESTDATA, dashboard_name)))
    await_condition(kubectl, "dashboard", dashboard_name, "Synced", "True")
    dashboard_id = kubectl.get("dashboard", dashboard_name, TEST_NAMESPACE)["status"]["signozResource"]["id"]

    edited = dashboard_body(
        DASHBOARD_TESTDATA, dashboard_name, display_name="Renamed by the suite", tags=[{"key": "owner", "value": "renamed"}]
    )
    apply(dashboard_manifest(dashboard_name, provider_config, edited))

    eventually(
        lambda: signoz.data(f"{DASHBOARDS_BASE_URL}/{dashboard_id}")["spec"]["display"]["name"] == "Renamed by the suite",
        f"dashboard {dashboard_id} to carry the edited display name",
    )
    assert kubectl.get("dashboard", dashboard_name, TEST_NAMESPACE)["status"]["signozResource"]["id"] == dashboard_id
    assert signoz.data(f"{DASHBOARDS_BASE_URL}/{dashboard_id}")["tags"] == [{"key": "owner", "value": "renamed"}]


def test_a_change_made_in_signoz_is_reverted(
    kubectl: Kubectl,
    signoz: SigNoz,
    provider_config: str,
    dashboard_name: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
) -> None:
    body = dashboard_body(DASHBOARD_TESTDATA, dashboard_name)
    apply(dashboard_manifest(dashboard_name, provider_config, body))
    await_condition(kubectl, "dashboard", dashboard_name, "Synced", "True")
    dashboard_id = kubectl.get("dashboard", dashboard_name, TEST_NAMESPACE)["status"]["signozResource"]["id"]

    drifted = copy.deepcopy(body)
    drifted["spec"]["display"]["description"] = "edited outside the operator"
    assert signoz.put(f"{DASHBOARDS_BASE_URL}/{dashboard_id}", drifted).status_code == HTTPStatus.OK

    eventually(
        lambda: (
            signoz.data(f"{DASHBOARDS_BASE_URL}/{dashboard_id}")["spec"]["display"]["description"] == body["spec"]["display"]["description"]
        ),
        f"dashboard {dashboard_id} to be restored to the spec",
    )


def test_a_suspended_resource_is_left_alone(
    kubectl: Kubectl,
    signoz: SigNoz,
    provider_config: str,
    dashboard_name: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
) -> None:
    body = dashboard_body(DASHBOARD_TESTDATA, dashboard_name)
    apply(dashboard_manifest(dashboard_name, provider_config, body))
    await_condition(kubectl, "dashboard", dashboard_name, "Synced", "True")
    dashboard_id = kubectl.get("dashboard", dashboard_name, TEST_NAMESPACE)["status"]["signozResource"]["id"]

    apply(dashboard_manifest(dashboard_name, provider_config, body, suspend=True))
    await_condition(kubectl, "dashboard", dashboard_name, "Suspended", "True", "Suspended")
    await_condition(kubectl, "dashboard", dashboard_name, "Ready", "False", "Suspended")

    drifted = copy.deepcopy(body)
    drifted["spec"]["display"]["description"] = "changed while suspended"
    assert signoz.put(f"{DASHBOARDS_BASE_URL}/{dashboard_id}", drifted).status_code == HTTPStatus.OK

    # Long enough for several resyncs, so an uncorrected drift means suspend held.
    time.sleep(4 * int(RESOURCE_INTERVAL.removesuffix("s")))
    assert signoz.data(f"{DASHBOARDS_BASE_URL}/{dashboard_id}")["spec"]["display"]["description"] == "changed while suspended"


def test_reclaim_delete_removes_the_dashboard(
    kubectl: Kubectl,
    signoz: SigNoz,
    provider_config: str,
    dashboard_name: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
) -> None:
    apply(dashboard_manifest(dashboard_name, provider_config, dashboard_body(DASHBOARD_TESTDATA, dashboard_name), reclaim_policy="Delete"))
    await_condition(kubectl, "dashboard", dashboard_name, "Synced", "True")
    dashboard_id = kubectl.get("dashboard", dashboard_name, TEST_NAMESPACE)["status"]["signozResource"]["id"]

    kubectl.run("delete", "dashboard", dashboard_name, "--namespace", TEST_NAMESPACE)

    assert not kubectl.exists("dashboard", dashboard_name, TEST_NAMESPACE)
    eventually(
        lambda: signoz.get(f"{DASHBOARDS_BASE_URL}/{dashboard_id}").status_code == HTTPStatus.NOT_FOUND,
        f"dashboard {dashboard_id} to be deleted in SigNoz",
    )


def test_reclaim_orphan_leaves_the_dashboard(
    kubectl: Kubectl,
    signoz: SigNoz,
    provider_config: str,
    dashboard_name: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
) -> None:
    apply(dashboard_manifest(dashboard_name, provider_config, dashboard_body(DASHBOARD_TESTDATA, dashboard_name), reclaim_policy="Orphan"))
    await_condition(kubectl, "dashboard", dashboard_name, "Synced", "True")
    dashboard_id = kubectl.get("dashboard", dashboard_name, TEST_NAMESPACE)["status"]["signozResource"]["id"]

    kubectl.run("delete", "dashboard", dashboard_name, "--namespace", TEST_NAMESPACE)

    assert not kubectl.exists("dashboard", dashboard_name, TEST_NAMESPACE)
    assert signoz.get(f"{DASHBOARDS_BASE_URL}/{dashboard_id}").status_code == HTTPStatus.OK


def test_an_existing_dashboard_is_adopted(
    kubectl: Kubectl,
    signoz: SigNoz,
    provider_config: str,
    dashboard_name: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
) -> None:
    body = dashboard_body(DASHBOARD_TESTDATA, dashboard_name)
    created = signoz.post(DASHBOARDS_BASE_URL, body)
    assert created.status_code == HTTPStatus.CREATED

    apply(dashboard_manifest(dashboard_name, provider_config, body))
    await_condition(kubectl, "dashboard", dashboard_name, "Synced", "True")

    assert kubectl.get("dashboard", dashboard_name, TEST_NAMESPACE)["status"]["signozResource"]["id"] == created.json()["data"]["id"]


def test_an_annotation_pins_the_dashboard_to_adopt(
    kubectl: Kubectl,
    signoz: SigNoz,
    provider_config: str,
    dashboard_name: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
) -> None:
    body = dashboard_body(DASHBOARD_TESTDATA, dashboard_name)
    created = signoz.post(DASHBOARDS_BASE_URL, body)
    assert created.status_code == HTTPStatus.CREATED
    pinned_id = created.json()["data"]["id"]

    apply(dashboard_manifest(dashboard_name, provider_config, body, annotations={ANNOTATION_SIGNOZ_RESOURCE_ID: pinned_id}))
    await_condition(kubectl, "dashboard", dashboard_name, "Synced", "True")

    assert kubectl.get("dashboard", dashboard_name, TEST_NAMESPACE)["status"]["signozResource"]["id"] == pinned_id


def test_a_pin_that_matches_nothing_is_terminal(
    kubectl: Kubectl,
    provider_config: str,
    dashboard_name: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
) -> None:
    annotations = {ANNOTATION_SIGNOZ_RESOURCE_ID: "01a00000-0000-7000-0000-000000000000"}
    apply(dashboard_manifest(dashboard_name, provider_config, dashboard_body(DASHBOARD_TESTDATA, dashboard_name), annotations=annotations))

    condition = await_condition(kubectl, "dashboard", dashboard_name, "Terminal", "True", "SigNozResourceIDMismatch")
    assert "candidates: none" in condition["message"]


def test_an_unparseable_json_spec_is_terminal(
    kubectl: Kubectl,
    provider_config: str,
    dashboard_name: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
) -> None:
    apply(
        {
            "apiVersion": API_VERSION,
            "kind": "Dashboard",
            "metadata": {"name": dashboard_name},
            "spec": {
                "providerConfigRef": {"name": provider_config},
                "interval": RESOURCE_INTERVAL,
                "objectTemplate": {"jsonSpec": "{not json"},
            },
        }
    )

    await_condition(kubectl, "dashboard", dashboard_name, "Terminal", "True", "InvalidSpec")
    await_condition(kubectl, "dashboard", dashboard_name, "Ready", "False", "InvalidSpec")
