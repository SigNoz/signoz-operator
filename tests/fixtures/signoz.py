import logging
import time
from dataclasses import dataclass
from typing import Any

import pytest
import requests

logger = logging.getLogger(__name__)

# requests has no timeout of its own, so every call passes one.
TIMEOUT = 30.0

# The SigNoz API as seen from the host. The port pairs kind.yaml's
# extraPortMappings with the nodePort e2e/signoz/service.yaml pins; changing one
# means changing all three.
SIGNOZ_URL = "http://127.0.0.1:30080"

# Open access, so it answers before the suite holds a credential. SigNoz serves
# its web app on anything it does not route, so an unknown path answers 200 with
# HTML; the check reads the body rather than the status.
HEALTH_PATH = "/api/v2/healthz"

# The same backend as SIGNOZ_URL, addressed the way the operator addresses it
# from inside the cluster. A provider config's endpoint carries this one.
SIGNOZ_CLUSTER_URL = "http://signoz-signoz.signoz.svc.cluster.local:8080"

# The root user SigNoz provisions at boot. These must match the
# SIGNOZ_USER_ROOT_* settings in casting.yaml; SigNoz reconciles them on every
# restart, so they hold across a --reuse run. The password only ever reaches a
# throwaway cluster.
ROOT_EMAIL = "admin@e2e.test"
ROOT_PASSWORD = "password123Z$"

# The suite authenticates as a service account rather than as the admin user,
# which is how an operator is meant to be given access. A key carries no
# permissions of its own, so the account is granted a role before one is issued.
SERVICE_ACCOUNT_NAME = "signoz-operator-e2e"
ADMIN_ROLE_NAME = "signoz-admin"


@dataclass(frozen=True)
class SigNoz:
    """The SigNoz API, authenticated by the same service account key the operator uses."""

    client: requests.Session
    api_key: str

    def request(self, method: str, path: str, body: Any = None) -> requests.Response:
        response = self.client.request(
            method,
            SIGNOZ_URL + path,
            json=body,
            headers={"SIGNOZ-API-KEY": self.api_key},
            timeout=TIMEOUT,
        )
        logger.info("signoz: %s %s -> %s", method, path, response.status_code)
        return response

    def get(self, path: str) -> requests.Response:
        return self.request("GET", path)

    def post(self, path: str, body: Any) -> requests.Response:
        return self.request("POST", path, body)

    def put(self, path: str, body: Any) -> requests.Response:
        return self.request("PUT", path, body)

    def delete(self, path: str) -> requests.Response:
        return self.request("DELETE", path)

    def data(self, path: str) -> Any:
        """The data envelope of a successful GET, raising with the body on any other status."""
        response = self.get(path)
        if response.status_code != 200:
            raise RuntimeError(f"GET {path} returned {response.status_code}: {response.text}")
        return response.json()["data"]


@pytest.fixture(scope="session")
def signoz(signoz_ready: None) -> SigNoz:
    """Issues the service account key the suite authenticates with, as SigNoz's root user.

    The root user is provisioned by casting.yaml rather than registered here, so
    this runs the same way on a fresh cluster and on one a --reuse run left
    behind. The service account is looked up before it is created; a key's value
    is only ever returned at creation, so each session issues its own.
    """
    with requests.Session() as client:
        # Logging in needs the org, which this endpoint answers without a credential.
        context = client.get(
            SIGNOZ_URL + "/api/v2/sessions/context",
            params={"email": ROOT_EMAIL, "ref": "/"},
            timeout=TIMEOUT,
        )
        if context.status_code != 200:
            raise RuntimeError(f"could not read the session context: {context.status_code}: {context.text}")

        orgs = context.json()["data"]["orgs"]
        if not orgs:
            raise RuntimeError(
                f"SigNoz provisioned no organization for {ROOT_EMAIL}; check the SIGNOZ_USER_ROOT_* settings in casting.yaml"
            )

        session = client.post(
            SIGNOZ_URL + "/api/v2/sessions/email_password",
            json={"email": ROOT_EMAIL, "password": ROOT_PASSWORD, "orgId": orgs[0]["id"]},
            timeout=TIMEOUT,
        )
        if session.status_code != 200:
            raise RuntimeError(f"could not log in as {ROOT_EMAIL}: {session.status_code}: {session.text}")

        bearer = {"Authorization": "Bearer " + session.json()["data"]["accessToken"]}

        roles = client.get(SIGNOZ_URL + "/api/v1/roles", headers=bearer, timeout=TIMEOUT)
        if roles.status_code != 200:
            raise RuntimeError(f"could not list roles: {roles.status_code}: {roles.text}")

        admin_role = next(role for role in roles.json()["data"] if role["name"] == ADMIN_ROLE_NAME)

        accounts = client.get(SIGNOZ_URL + "/api/v1/service_accounts", headers=bearer, timeout=TIMEOUT)
        if accounts.status_code != 200:
            raise RuntimeError(f"could not list service accounts: {accounts.status_code}: {accounts.text}")

        account_id = next(
            (account["id"] for account in accounts.json()["data"] if account["name"] == SERVICE_ACCOUNT_NAME),
            None,
        )
        if account_id is None:
            created = client.post(
                SIGNOZ_URL + "/api/v1/service_accounts",
                json={"name": SERVICE_ACCOUNT_NAME},
                headers=bearer,
                timeout=TIMEOUT,
            )
            if created.status_code != 201:
                raise RuntimeError(f"could not create the service account: {created.status_code}: {created.text}")
            account_id = created.json()["data"]["id"]

        # Granted separately from creation, and only when missing, so an account
        # left behind without its role by an interrupted run still ends up usable.
        granted = client.get(f"{SIGNOZ_URL}/api/v1/service_accounts/{account_id}/roles", headers=bearer, timeout=TIMEOUT)
        if granted.status_code != 200:
            raise RuntimeError(f"could not list the service account's roles: {granted.status_code}: {granted.text}")

        if not any(role["id"] == admin_role["id"] for role in granted.json()["data"]):
            attached = client.post(
                SIGNOZ_URL + "/api/v1/service_account_roles",
                json={"serviceAccountId": account_id, "roleId": admin_role["id"]},
                headers=bearer,
                timeout=TIMEOUT,
            )
            if attached.status_code != 201:
                raise RuntimeError(f"could not grant {ADMIN_ROLE_NAME} to the service account: {attached.status_code}: {attached.text}")

        key = client.post(
            f"{SIGNOZ_URL}/api/v1/service_accounts/{account_id}/keys",
            json={"name": f"e2e-{int(time.time())}", "expiresAt": 0},
            headers=bearer,
            timeout=TIMEOUT,
        )
        if key.status_code != 201:
            raise RuntimeError(f"could not create a service account key: {key.status_code}: {key.text}")

        api_key = key.json()["data"]["key"]

    client = requests.Session()
    yield SigNoz(client=client, api_key=api_key)
    client.close()


@pytest.fixture(scope="session")
def signoz_ready(environment) -> None:
    """Blocks until the published SigNoz API answers, which trails its rollout."""
    environment.assert_ready()

    deadline = time.monotonic() + 300
    last = ""
    with requests.Session() as client:
        while time.monotonic() < deadline:
            try:
                response = client.get(SIGNOZ_URL + HEALTH_PATH, timeout=10.0)
            except requests.RequestException as err:
                last = str(err)
            else:
                try:
                    healthy = response.json()["data"]["healthy"]
                except (ValueError, KeyError, TypeError):
                    healthy = None
                if healthy:
                    return
                last = f"{response.status_code}: {response.text[:200]}"
            time.sleep(2)

    raise RuntimeError(f"the SigNoz API at {SIGNOZ_URL}{HEALTH_PATH} did not answer: {last}")
