"""The mirrored-resource contract, exercised through Dashboard: create, adopt, drift, suspend, reclaim.

Dashboard is one kind, but the reconcile engine, the condition vocabulary and the
identity rules under test are shared by every mirrored kind.
"""

import copy
import json
import time
from collections.abc import Callable
from typing import Any

from fixtures.kubectl import Kubectl
from fixtures.resources import ANNOTATION_SIGNOZ_RESOURCE_ID, API_VERSION, RESOURCE_FINALIZER, RESOURCE_INTERVAL
from fixtures.signoz import SigNoz

DASHBOARDS = "/api/v2/dashboards"

RESOURCE_INTERVAL_SECONDS = int(RESOURCE_INTERVAL.removesuffix("s"))


def test_creates_the_dashboard_and_records_its_id(
    kubectl: Kubectl,
    namespace: str,
    signoz: SigNoz,
    dashboard_name: str,
    apply_dashboard: Callable[..., dict[str, Any]],
    await_condition: Callable[..., dict[str, Any]],
    reclaim_dashboards: None,
) -> None:
    sent = apply_dashboard(dashboard_name)

    await_condition("dashboard", dashboard_name, "Ready", "True")
    await_condition("dashboard", dashboard_name, "Synced", "True")

    resource = kubectl.get("dashboard", dashboard_name, namespace)
    dashboard_id = resource["status"]["signozResource"]["id"]

    remote = signoz.data(f"{DASHBOARDS}/{dashboard_id}")
    assert remote["name"] == dashboard_name
    assert remote["spec"]["display"]["name"] == sent["spec"]["display"]["name"]
    assert remote["tags"] == sent["tags"]

    # The finalizer is in place before the remote object exists, so a reclaim
    # policy can always be applied.
    assert RESOURCE_FINALIZER in resource["metadata"]["finalizers"]
    assert resource["status"]["observedHash"]


def test_an_edited_spec_reaches_signoz(
    kubectl: Kubectl,
    namespace: str,
    signoz: SigNoz,
    dashboard_name: str,
    apply_dashboard: Callable[..., dict[str, Any]],
    await_condition: Callable[..., dict[str, Any]],
    eventually: Callable[..., Any],
    reclaim_dashboards: None,
) -> None:
    apply_dashboard(dashboard_name)
    await_condition("dashboard", dashboard_name, "Synced", "True")
    dashboard_id = kubectl.get("dashboard", dashboard_name, namespace)["status"]["signozResource"]["id"]

    apply_dashboard(dashboard_name, display_name="Renamed by the suite", tags=[{"key": "owner", "value": "renamed"}])

    eventually(
        lambda: signoz.data(f"{DASHBOARDS}/{dashboard_id}")["spec"]["display"]["name"] == "Renamed by the suite",
        f"dashboard {dashboard_id} to carry the edited display name",
    )
    # The identity is unchanged, so the edit updates in place rather than
    # creating a second dashboard.
    assert kubectl.get("dashboard", dashboard_name, namespace)["status"]["signozResource"]["id"] == dashboard_id
    assert signoz.data(f"{DASHBOARDS}/{dashboard_id}")["tags"] == [{"key": "owner", "value": "renamed"}]


def test_a_change_made_in_signoz_is_reverted(
    kubectl: Kubectl,
    namespace: str,
    signoz: SigNoz,
    dashboard_name: str,
    apply_dashboard: Callable[..., dict[str, Any]],
    await_condition: Callable[..., dict[str, Any]],
    eventually: Callable[..., Any],
    reclaim_dashboards: None,
) -> None:
    """Desired state is the custom resource, so an edit made directly in SigNoz is drift."""
    sent = apply_dashboard(dashboard_name)
    await_condition("dashboard", dashboard_name, "Synced", "True")
    dashboard_id = kubectl.get("dashboard", dashboard_name, namespace)["status"]["signozResource"]["id"]

    drifted = copy.deepcopy(sent)
    drifted["spec"]["display"]["description"] = "edited outside the operator"
    assert signoz.put(f"{DASHBOARDS}/{dashboard_id}", drifted).status_code == 200

    eventually(
        lambda: signoz.data(f"{DASHBOARDS}/{dashboard_id}")["spec"]["display"]["description"] == sent["spec"]["display"]["description"],
        f"dashboard {dashboard_id} to be restored to the spec",
    )


def test_a_suspended_resource_is_left_alone(
    kubectl: Kubectl,
    namespace: str,
    signoz: SigNoz,
    dashboard_name: str,
    apply_dashboard: Callable[..., dict[str, Any]],
    await_condition: Callable[..., dict[str, Any]],
    reclaim_dashboards: None,
) -> None:
    sent = apply_dashboard(dashboard_name)
    await_condition("dashboard", dashboard_name, "Synced", "True")
    dashboard_id = kubectl.get("dashboard", dashboard_name, namespace)["status"]["signozResource"]["id"]

    apply_dashboard(dashboard_name, suspend=True)
    await_condition("dashboard", dashboard_name, "Suspended", "True", "Suspended")
    await_condition("dashboard", dashboard_name, "Ready", "False", "Suspended")

    drifted = copy.deepcopy(sent)
    drifted["spec"]["display"]["description"] = "changed while suspended"
    assert signoz.put(f"{DASHBOARDS}/{dashboard_id}", drifted).status_code == 200

    # Several intervals' worth of opportunity to correct the drift, so that
    # finding it uncorrected means suspend held rather than that the check was
    # early.
    time.sleep(4 * RESOURCE_INTERVAL_SECONDS)
    assert signoz.data(f"{DASHBOARDS}/{dashboard_id}")["spec"]["display"]["description"] == "changed while suspended"


def test_reclaim_delete_removes_the_dashboard(
    kubectl: Kubectl,
    namespace: str,
    signoz: SigNoz,
    dashboard_name: str,
    apply_dashboard: Callable[..., dict[str, Any]],
    await_condition: Callable[..., dict[str, Any]],
    eventually: Callable[..., Any],
) -> None:
    apply_dashboard(dashboard_name, reclaim_policy="Delete")
    await_condition("dashboard", dashboard_name, "Synced", "True")
    dashboard_id = kubectl.get("dashboard", dashboard_name, namespace)["status"]["signozResource"]["id"]

    kubectl.run("delete", "dashboard", dashboard_name, "--namespace", namespace)

    assert not kubectl.exists("dashboard", dashboard_name, namespace)
    eventually(
        lambda: signoz.get(f"{DASHBOARDS}/{dashboard_id}").status_code == 404,
        f"dashboard {dashboard_id} to be deleted in SigNoz",
    )


def test_reclaim_orphan_leaves_the_dashboard(
    kubectl: Kubectl,
    namespace: str,
    signoz: SigNoz,
    dashboard_name: str,
    apply_dashboard: Callable[..., dict[str, Any]],
    await_condition: Callable[..., dict[str, Any]],
    reclaim_dashboards: None,
) -> None:
    apply_dashboard(dashboard_name, reclaim_policy="Orphan")
    await_condition("dashboard", dashboard_name, "Synced", "True")
    dashboard_id = kubectl.get("dashboard", dashboard_name, namespace)["status"]["signozResource"]["id"]

    kubectl.run("delete", "dashboard", dashboard_name, "--namespace", namespace)

    assert not kubectl.exists("dashboard", dashboard_name, namespace)
    assert signoz.get(f"{DASHBOARDS}/{dashboard_id}").status_code == 200


def test_an_existing_dashboard_is_adopted(
    kubectl: Kubectl,
    namespace: str,
    signoz: SigNoz,
    dashboard_name: str,
    apply_dashboard: Callable[..., dict[str, Any]],
    await_condition: Callable[..., dict[str, Any]],
    testdata_dir: Any,
    reclaim_dashboards: None,
) -> None:
    """A single dashboard matching the resource's identity is taken over, not duplicated."""
    body = json.loads((testdata_dir / "dashboard.json").read_text())
    body["name"] = dashboard_name
    created = signoz.post(DASHBOARDS, body)
    assert created.status_code == 201
    existing_id = created.json()["data"]["id"]

    apply_dashboard(dashboard_name)
    await_condition("dashboard", dashboard_name, "Synced", "True")

    assert kubectl.get("dashboard", dashboard_name, namespace)["status"]["signozResource"]["id"] == existing_id


def test_an_annotation_pins_the_dashboard_to_adopt(
    kubectl: Kubectl,
    namespace: str,
    signoz: SigNoz,
    dashboard_name: str,
    apply_dashboard: Callable[..., dict[str, Any]],
    await_condition: Callable[..., dict[str, Any]],
    testdata_dir: Any,
    reclaim_dashboards: None,
) -> None:
    """A pinned id must be among the objects matching the identity, and is then adopted.

    SigNoz rejects a second dashboard under a name it already holds, so a
    Dashboard's identity never resolves to more than one candidate and the
    reconciler's ambiguity outcome cannot arise for this kind. The annotation is
    still what decides, so it is what this asserts.
    """
    body = json.loads((testdata_dir / "dashboard.json").read_text())
    body["name"] = dashboard_name
    created = signoz.post(DASHBOARDS, body)
    assert created.status_code == 201
    pinned_id = created.json()["data"]["id"]

    apply_dashboard(dashboard_name, annotations={ANNOTATION_SIGNOZ_RESOURCE_ID: pinned_id})
    await_condition("dashboard", dashboard_name, "Synced", "True")

    assert kubectl.get("dashboard", dashboard_name, namespace)["status"]["signozResource"]["id"] == pinned_id


def test_a_pin_that_matches_nothing_is_terminal(
    namespace: str,
    dashboard_name: str,
    apply_dashboard: Callable[..., dict[str, Any]],
    await_condition: Callable[..., dict[str, Any]],
    reclaim_dashboards: None,
) -> None:
    apply_dashboard(dashboard_name, annotations={ANNOTATION_SIGNOZ_RESOURCE_ID: "01a00000-0000-7000-0000-000000000000"})

    condition = await_condition("dashboard", dashboard_name, "Terminal", "True", "SigNozResourceIDMismatch")
    assert "candidates: none" in condition["message"]


def test_an_unparseable_json_spec_is_terminal(
    provider_config: str,
    dashboard_name: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
    await_condition: Callable[..., dict[str, Any]],
) -> None:
    """A body the operator cannot even render is the resource's own fault, and no retry fixes it."""
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

    await_condition("dashboard", dashboard_name, "Terminal", "True", "InvalidSpec")
    await_condition("dashboard", dashboard_name, "Ready", "False", "InvalidSpec")
