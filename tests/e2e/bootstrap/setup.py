from fixtures.environment import Environment


def test_setup(environment: Environment) -> None:
    environment.assert_ready()
