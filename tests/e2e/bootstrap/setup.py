from fixtures.environment import Environment


def test_setup(environment: Environment) -> None:
    """Bring up SigNoz through Foundry and deploy a local build of the operator."""
    environment.assert_ready()
