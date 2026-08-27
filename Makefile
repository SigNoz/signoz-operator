##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: checks
checks: go-checks controllergen-checks kyaml-checks py-checks

.PHONY: controllergen-checks
controllergen-checks: ## Runs controllergen checks.
	@make -f "$$PRIMUS_HOME/src/make/main.mk" controllergen CONTROLLERGEN_ARGS='object paths="./..."'
	@make -f "$$PRIMUS_HOME/src/make/main.mk" controllergen CONTROLLERGEN_ARGS='rbac:roleName=signoz-operator,fileName=clusterrole.generated.yaml crd:allowDangerousTypes=true webhook paths="./..." output:crd:artifacts:config=config/crd/bases'

.PHONY: kyaml-checks
kyaml-checks: ## Runs kyaml checks.
	@make -f "$$PRIMUS_HOME/src/make/main.mk" kyaml-fmt

.PHONY: go-checks
go-checks: ## Runs go checks.
	@make -f "$$PRIMUS_HOME/src/make/main.mk" go-fmt
	@make -f "$$PRIMUS_HOME/src/make/main.mk" go-deps
	@make -f "$$PRIMUS_HOME/src/make/main.mk" go-lint
	@make -f "$$PRIMUS_HOME/src/make/main.mk" go-test

.PHONY: py-checks
py-checks: ## Runs python checks for the e2e suite.
	@make -f "$$PRIMUS_HOME/src/make/main.mk" py-fmt PY_SRC=tests
	@make -f "$$PRIMUS_HOME/src/make/main.mk" py-deps PY_SRC=tests
	@make -f "$$PRIMUS_HOME/src/make/main.mk" py-lint PY_SRC=tests

##@ E2E

# Narrows a run to one file or test, as a path relative to tests/.
E2E_TARGET ?=

.PHONY: test-e2e
test-e2e: ## Runs the e2e suite in a Kind cluster, and removes the cluster afterwards.
	@make -f "$$PRIMUS_HOME/src/make/main.mk" py-test PY_SRC=tests PY_TEST_FLAGS='$(E2E_TARGET)'

.PHONY: test-e2e-reuse
test-e2e-reuse: ## Runs the e2e suite and keeps the cluster for the next --reuse run.
	@make -f "$$PRIMUS_HOME/src/make/main.mk" py-test PY_SRC=tests PY_TEST_FLAGS='--reuse $(E2E_TARGET)'

.PHONY: setup-e2e-env
setup-e2e-env: ## Brings the e2e environment up and keeps it, without running the behaviour tests.
	@make -f "$$PRIMUS_HOME/src/make/main.mk" py-test PY_SRC=tests PY_TEST_FLAGS='--reuse e2e/bootstrap/setup.py'

.PHONY: cleanup-test-e2e
cleanup-test-e2e: ## Deletes the e2e environment a --reuse run left behind.
	@make -f "$$PRIMUS_HOME/src/make/main.mk" py-test PY_SRC=tests PY_TEST_FLAGS='--teardown e2e/bootstrap/setup.py'