from collections.abc import Callable
from pathlib import Path
from typing import Any

from fixtures.dashboards import DASHBOARDS_BASE_URL, dashboard_body, dashboard_manifest
from fixtures.environment import TEST_NAMESPACE
from fixtures.kubectl import Kubectl
from fixtures.resources import await_condition
from fixtures.signoz import SigNoz

DASHBOARD_TESTDATA = Path(__file__).resolve().parents[3] / "testdata" / "dashboard_zero_values.json"


def test_a_zero_axis_bound_reaches_signoz(
    kubectl: Kubectl,
    signoz: SigNoz,
    provider_config: str,
    dashboard_name: str,
    apply: Callable[[dict[str, Any]], dict[str, Any]],
) -> None:
    apply(dashboard_manifest(dashboard_name, provider_config, dashboard_body(DASHBOARD_TESTDATA, dashboard_name)))
    await_condition(kubectl, "dashboard", dashboard_name, "Synced", "True")

    dashboard_id = kubectl.get("dashboard", dashboard_name, TEST_NAMESPACE)["status"]["signozResource"]["id"]
    remote = signoz.data(f"{DASHBOARDS_BASE_URL}/{dashboard_id}")
    assert remote["spec"]["panels"]["requests"]["spec"]["plugin"]["spec"]["axes"]["softMin"] == 0
