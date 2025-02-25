# Copyright 2022 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

export REPO_ROOT := $(shell git rev-parse --show-toplevel)

include $(REPO_ROOT)/common.mk

#
# Kubebuilder.
#
export KUBEBUILDER_ENVTEST_KUBERNETES_VERSION ?= 1.31.0
export KUBEBUILDER_CONTROLPLANE_START_TIMEOUT ?= 60s
export KUBEBUILDER_CONTROLPLANE_STOP_TIMEOUT ?=

# Directories
TOOLS_DIR := $(REPO_ROOT)/hack/tools
TOOLS_BIN_DIR := $(TOOLS_DIR)/bin
BIN_DIR ?= bin
RELEASE_DIR ?= out
GO_INSTALL := ./hack/go_install.sh

GH_REPO ?= kubernetes-sigs/cluster-api-provider-cloudstack

# Helper function to get dependency version from go.mod
get_go_version = $(shell go list -m $1 | awk '{print $$2}')

# Set build time variables including version details
LDFLAGS := $(shell source ./hack/version.sh; version::ldflags)

# Binaries
KUSTOMIZE_VER := v5.3.0
KUSTOMIZE_BIN := kustomize
KUSTOMIZE := $(abspath $(TOOLS_BIN_DIR)/$(KUSTOMIZE_BIN)-$(KUSTOMIZE_VER))
KUSTOMIZE_PKG := sigs.k8s.io/kustomize/kustomize/v5

# This is a commit from CR main (22.05.2024).
# Intentionally using a commit from main to use a setup-envtest version
# that uses binaries from controller-tools, not GCS.
# CR PR: https://github.com/kubernetes-sigs/controller-runtime/pull/2811
SETUP_ENVTEST_VER := v0.0.0-20240522175850-2e9781e9fc60
SETUP_ENVTEST_BIN := setup-envtest
SETUP_ENVTEST := $(abspath $(TOOLS_BIN_DIR)/$(SETUP_ENVTEST_BIN)-$(SETUP_ENVTEST_VER))
SETUP_ENVTEST_PKG := sigs.k8s.io/controller-runtime/tools/setup-envtest

CONTROLLER_GEN_VER := v0.15.0
CONTROLLER_GEN_BIN := controller-gen
CONTROLLER_GEN := $(abspath $(TOOLS_BIN_DIR)/$(CONTROLLER_GEN_BIN)-$(CONTROLLER_GEN_VER))
CONTROLLER_GEN_PKG := sigs.k8s.io/controller-tools/cmd/controller-gen

GOTESTSUM_VER := v1.11.0
GOTESTSUM_BIN := gotestsum
GOTESTSUM := $(abspath $(TOOLS_BIN_DIR)/$(GOTESTSUM_BIN)-$(GOTESTSUM_VER))
GOTESTSUM_PKG := gotest.tools/gotestsum

CONVERSION_GEN_VER := v0.30.0
CONVERSION_GEN_BIN := conversion-gen
# We are intentionally using the binary without version suffix, to avoid the version
# in generated files.
CONVERSION_GEN := $(abspath $(TOOLS_BIN_DIR)/$(CONVERSION_GEN_BIN))
CONVERSION_GEN_PKG := k8s.io/code-generator/cmd/conversion-gen

ENVSUBST_BIN := envsubst
ENVSUBST_VER := $(call get_go_version,github.com/drone/envsubst/v2)
ENVSUBST := $(abspath $(TOOLS_BIN_DIR)/$(ENVSUBST_BIN)-$(ENVSUBST_VER))
ENVSUBST_PKG := github.com/drone/envsubst/v2/cmd/envsubst

GO_APIDIFF_VER := v0.8.2
GO_APIDIFF_BIN := go-apidiff
GO_APIDIFF := $(abspath $(TOOLS_BIN_DIR)/$(GO_APIDIFF_BIN)-$(GO_APIDIFF_VER))
GO_APIDIFF_PKG := github.com/joelanford/go-apidiff

GINKGO_BIN := ginkgo
GINGKO_VER := $(call get_go_version,github.com/onsi/ginkgo/v2)
GINKGO := $(abspath $(TOOLS_BIN_DIR)/$(GINKGO_BIN)-$(GINGKO_VER))
GINKGO_PKG := github.com/onsi/ginkgo/v2/ginkgo

GOLANGCI_LINT_BIN := golangci-lint
GOLANGCI_LINT_VER := v1.60.3
GOLANGCI_LINT := $(abspath $(TOOLS_BIN_DIR)/$(GOLANGCI_LINT_BIN)-$(GOLANGCI_LINT_VER))
GOLANGCI_LINT_PKG := github.com/golangci/golangci-lint/cmd/golangci-lint

MOCKGEN_BIN := mockgen
MOCKGEN_VER := v0.5.0
MOCKGEN := $(abspath $(TOOLS_BIN_DIR)/$(MOCKGEN_BIN)-$(MOCKGEN_VER))
MOCKGEN_PKG := go.uber.org/mock/mockgen

KUBECTL := $(TOOLS_BIN_DIR)/kubectl

# Release
STAGING_REGISTRY := ghcr.io/leaseweb
STAGING_BUCKET ?= artifacts.k8s-staging-capi-cloudstack.appspot.com
BUCKET ?= $(STAGING_BUCKET)
PROD_REGISTRY ?= registry.k8s.io/capi-cloudstack
REGISTRY ?= $(STAGING_REGISTRY)
RELEASE_TAG ?= $(shell git describe --abbrev=0 2>/dev/null)
PULL_BASE_REF ?= $(RELEASE_TAG)
RELEASE_ALIAS_TAG ?= $(PULL_BASE_REF)

# Image URL to use all building/pushing image targets
REGISTRY ?= $(STAGING_REGISTRY)
IMAGE_NAME ?= capi-cloudstack-controller
TAG ?= develop
CONTROLLER_IMG ?= $(REGISTRY)/$(IMAGE_NAME)
IMG ?= $(CONTROLLER_IMG):$(TAG)
IMG_LOCAL ?= localhost:5000/$(IMAGE_NAME):$(TAG)
MANIFEST_FILE := infrastructure-components
CONFIG_DIR := config
NAMESPACE := capc-system

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
# SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

export PATH := $(TOOLS_BIN_DIR):$(PATH)

all: build

##@ Binaries
## --------------------------------------
## Binaries
## --------------------------------------

.PHONY: binaries
binaries: $(CONTROLLER_GEN) $(CONVERSION_GEN) $(GOLANGCI_LINT) $(GINKGO) $(MOCKGEN) $(KUSTOMIZE) $(SETUP_ENVTEST) managers # Builds and installs all binaries

.PHONY: managers
managers:
	$(MAKE) manager-cloudstack-infrastructure

.PHONY: manager-cloudstack-infrastructure
manager-cloudstack-infrastructure: ## Build manager binary.
	CGO_ENABLED=0 go build -ldflags "${LDFLAGS} -extldflags '-static'" -o $(BIN_DIR)/manager .

##@ Linting
## --------------------------------------
## Linting
## --------------------------------------

.PHONY: fmt
fmt: ## Run go fmt on the whole project.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet on the whole project.
	go vet ./...

.PHONY: lint
lint: $(GOLANGCI_LINT) generate-mocks ## Run linting for the project.
	$(MAKE) fmt
	$(MAKE) vet
	$(GOLANGCI_LINT) run -v --timeout 360s ./...

##@ Generate
## --------------------------------------
## Generate
## --------------------------------------

.PHONY: modules
modules: ## Runs go mod to ensure proper vendoring.
	go mod tidy -compat=1.23
	cd $(TOOLS_DIR); go mod tidy -compat=1.23

.PHONY: generate-all
generate-all: generate-mocks generate-conversion generate-deepcopy generate-manifests

.PHONY: generate-mocks
generate-mocks: $(MOCKGEN) generate-deepcopy pkg/mocks/mock_client.go $(shell find ./pkg/mocks -type f -name "mock*.go") ## Generate mocks needed for testing. Primarily mocks of the cloud package.
pkg/mocks/mock%.go: $(shell find ./pkg/cloud -type f -name "*test*" -prune -o -print)
	go generate ./...

DEEPCOPY_GEN_TARGETS=$(shell find api -type d -name "v*" -exec echo {}\/zz_generated.deepcopy.go \;)
DEEPCOPY_GEN_INPUTS=$(shell find ./api -name "*test*" -prune -o -name "*zz_generated*" -prune -o -type f -print)
.PHONY: generate-deepcopy
generate-deepcopy: $(DEEPCOPY_GEN_TARGETS) ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
api/%/zz_generated.deepcopy.go: $(CONTROLLER_GEN) $(DEEPCOPY_GEN_INPUTS)
	CGO_ENABLED=0 $(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./api/..."

MANIFEST_GEN_INPUTS=$(shell find ./api ./internal/controllers -type f -name "*test*" -prune -o -name "*zz_generated*" -prune -o -print)
# Using a flag file here as config output is too complicated to be a target.
# The following triggers manifest building if $(IMG) differs from that found in config/default/manager_image_patch.yaml.
$(shell	grep -qs "$(IMG)" config/default/manager_image_patch_edited.yaml || rm -f config/.flag.mk)
.PHONY: generate-manifests
generate-manifests: config/.flag.mk ## Generates crd, webhook, rbac, and other configuration manifests from kubebuilder instructions in go comments.
config/.flag.mk: $(CONTROLLER_GEN) $(MANIFEST_GEN_INPUTS)
	sed -e 's@image: .*@image: '"$(IMG)"'@' config/default/manager_image_patch.yaml > config/default/manager_image_patch_edited.yaml
	$(CONTROLLER_GEN) crd:crdVersions=v1 rbac:roleName=manager-role webhook paths="{./,./api/...,./internal/controllers/...}" output:crd:artifacts:config=config/crd/bases
	@touch config/.flag.mk

.PHONY: generate-conversion
generate-conversion: $(CONVERSION_GEN) ## Generate code to convert api/v1beta1 and api/v1beta2 to api/v1beta3
	$(CONVERSION_GEN) \
		--output-file=zz_generated.conversion.go \
		--go-header-file=./hack/boilerplate.go.txt \
		./api/v1beta1
	$(CONVERSION_GEN) \
		--output-file=zz_generated.conversion.go \
		--go-header-file=./hack/boilerplate.go.txt \
		./api/v1beta2

##@ Build
## --------------------------------------
## Build
## --------------------------------------

MANAGER_BIN_INPUTS=$(shell find ./internal/controllers ./api ./pkg -name "*mock*" -prune -o -name "*test*" -prune -o -type f -print) main.go go.mod go.sum
.PHONY: build
build: binaries generate-deepcopy generate-manifests release-manifests ## Build manager binary.
$(BIN_DIR)/manager: $(MANAGER_BIN_INPUTS)
	go build -ldflags "${LDFLAGS}" -o $(BIN_DIR)/manager main.go

.PHONY: build-for-docker
build-for-docker: $(BIN_DIR)/manager-linux-amd64 ## Build manager binary for docker image building.
$(BIN_DIR)/manager-linux-amd64: $(MANAGER_BIN_INPUTS)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    	go build -a -ldflags "${LDFLAGS} -extldflags '-static'" \
    	-o $(BIN_DIR)/manager-linux-amd64 main.go

.PHONY: run
run: generate-deepcopy generate-conversion ## Run a controller from your host.
	go run ./main.go

##@ Deploy
## --------------------------------------
## Deploy
## --------------------------------------

.PHONY: deploy
deploy: generate-deepcopy generate-manifests $(KUSTOMIZE) ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	cd $(REPO_ROOT)
	$(KUSTOMIZE) build config/default | kubectl apply -f -

undeploy: $(KUSTOMIZE) ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | kubectl delete -f -

##@ Docker
## --------------------------------------
## Docker
## --------------------------------------

# Using a flag file here as docker build doesn't produce a target file.
DOCKER_BUILD_INPUTS=$(MANAGER_BIN_INPUTS) Dockerfile
.PHONY: docker-build
docker-build: generate-deepcopy generate-conversion build-for-docker .dockerflag.mk ## Build docker image containing the controller manager.
.dockerflag.mk: $(DOCKER_BUILD_INPUTS)
	docker build -t ${IMG} .
	@touch .dockerflag.mk

.PHONY: docker-push
docker-push: .dockerflag.mk ## Push docker image with the manager.
	docker push ${IMG}

##@ Tilt
## --------------------------------------
## Tilt Development
## --------------------------------------

.PHONY: tilt-up
tilt-up: cluster-api create-kind-cluster cluster-api/tilt-settings.json generate-manifests ## Setup and run tilt for development.
	cd cluster-api && tilt up

KIND_CLUSTER_NAME := $(shell cat ./hack/tilt-settings.json | grep kind_cluster_name | cut -d: -f2 | xargs)

.PHONY: create-kind-cluster
create-kind-cluster: cluster-api cluster-api/tilt-settings.json ## Create a kind cluster with a local Docker repository.
	@if [ -z "$$(kind get clusters | grep $(KIND_CLUSTER_NAME))" ]; then \
		CAPI_KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) ./cluster-api/hack/kind-install-for-capd.sh; \
	fi;

.PHONY: delete-kind-cluster
delete-kind-cluster:
	kind delete cluster --name $(KIND_CLUSTER_NAME)

cluster-api: ## Clone cluster-api repository for tilt use.
	git clone --branch v1.8.10 --depth 1 https://github.com/kubernetes-sigs/cluster-api.git

cluster-api/tilt-settings.json: hack/tilt-settings.json cluster-api
	cp ./hack/tilt-settings.json cluster-api

##@ Tests
## --------------------------------------
## Tests
## --------------------------------------

KUBEBUILDER_ASSETS ?= $(shell $(SETUP_ENVTEST) use --use-env -p path $(KUBEBUILDER_ENVTEST_KUBERNETES_VERSION))

.PHONY: setup-envtest
setup-envtest: $(SETUP_ENVTEST) ## Set up envtest (download kubebuilder assets)
	@echo KUBEBUILDER_ASSETS=$(KUBEBUILDER_ASSETS)

.PHONY: test
test: generate-mocks setup-envtest $(GINKGO)
	KUBEBUILDER_ASSETS="$(KUBEBUILDER_ASSETS)" $(GINKGO) --label-filter="!integ" --cover -coverprofile cover.out --covermode=atomic -v ./api/... ./pkg/...
	KUBEBUILDER_ASSETS="$(KUBEBUILDER_ASSETS)" go test -v -coverprofile cover.out ./internal/controllers/...

.PHONY: test-pkg
test-pkg: $(GINKGO)  ## Run pkg tests.
	@$(GINKGO) --label-filter="!integ" --cover -coverprofile cover.out --covermode=atomic -v ./pkg/...

CLUSTER_TEMPLATES_INPUT_FILES=$(shell find test/e2e/data/infrastructure-cloudstack/v1beta*/cluster-template* test/e2e/data/infrastructure-cloudstack/*/bases/* -type f)
CLUSTER_TEMPLATES_OUTPUT_FILES=$(shell find test/e2e/data/infrastructure-cloudstack -type d -name "cluster-template*" -exec echo {}.yaml \;)
.PHONY: e2e-cluster-templates
e2e-cluster-templates: $(CLUSTER_TEMPLATES_OUTPUT_FILES) ## Generate cluster template files for e2e testing.
cluster-template%yaml: $(KUSTOMIZE) $(CLUSTER_TEMPLATES_INPUT_FILES)
	$(KUSTOMIZE) build --load-restrictor LoadRestrictionsNone $(basename $@) > $@

e2e-essentials: $(GINKGO) setup-envtest e2e-cluster-templates create-kind-cluster ## Fulfill essential tasks for e2e testing.
	IMG=$(IMG_LOCAL) make generate-manifests docker-build docker-push

JOB ?= .*
E2E_CONFIG ?= ${REPO_ROOT}/test/e2e/config/cloudstack.yaml
run-e2e: e2e-essentials ## Run e2e testing. JOB is an optional REGEXP to select certainn test cases to run. e.g. JOB=PR-Blocking, JOB=Conformance
	$(KUBECTL) apply -f cloud-config.yaml && \
	cd test/e2e && \
	$(GINKGO) -v --trace --tags=e2e --focus=$(JOB) --skip=Conformance --skip-package=kubeconfig_helper --nodes=1 --no-color=false ./... -- \
	    -e2e.artifacts-folder=${REPO_ROOT}/_artifacts \
	    -e2e.config=${E2E_CONFIG} \
	    -e2e.skip-resource-cleanup=false -e2e.use-existing-cluster=true
	EXIT_STATUS=$$?
	kind delete clusters capi-test
	exit $$EXIT_STATUS

run-e2e-smoke:
	./hack/ensure-kind.sh
	./hack/ensure-cloud-config-yaml.sh
	JOB="\"CAPC E2E SMOKE TEST\"" $(MAKE) run-e2e

##@ Cleanup
## --------------------------------------
## Cleanup
## --------------------------------------

.PHONY: clean
clean: ## Cleans up everything.
	rm -rf $(RELEASE_DIR)
	rm -rf bin
	rm -rf $(TOOLS_BIN_DIR)
	rm -rf cluster-api
	rm -rf test/e2e/data/infrastructure-cloudstack/*/*yaml
	rm -rf config/.flag.mk .dockerflag.mk

##@ Release
## --------------------------------------
## Release
## --------------------------------------

.PHONY: release-manifests
RELEASE_MANIFEST_TARGETS=$(RELEASE_DIR)/infrastructure-components.yaml $(RELEASE_DIR)/metadata.yaml
RELEASE_MANIFEST_INPUTS=$(KUSTOMIZE) config/.flag.mk $(shell find config)
RELEASE_MANIFEST_SOURCE_BASE ?= config/default
release-manifests: $(RELEASE_MANIFEST_TARGETS) ## Create kustomized release manifest in $RELEASE_DIR (defaults to out).
$(RELEASE_DIR)/%: $(RELEASE_MANIFEST_INPUTS)
	@mkdir -p $(RELEASE_DIR)
	cp metadata.yaml $(RELEASE_DIR)/metadata.yaml
	$(KUSTOMIZE) build $(RELEASE_MANIFEST_SOURCE_BASE) > $(RELEASE_DIR)/infrastructure-components.yaml

.PHONY: release-manifests-metrics-port
release-manifests-metrics-port:
	make release-manifests RELEASE_MANIFEST_SOURCE_BASE=config/default

.PHONY: release-staging
release-staging: ## Builds and push container images and manifests to the staging bucket.
	$(MAKE) docker-build
	$(MAKE) docker-push
	$(MAKE) release-alias-tag
	$(MAKE) release-templates
	$(MAKE) release-manifests TAG=$(RELEASE_ALIAS_TAG)
	$(MAKE) upload-staging-artifacts

.PHONY: release-alias-tag
release-alias-tag: # Adds the tag to the last build tag.
	gcloud container images add-tag -q $(CONTROLLER_IMG):$(TAG) $(CONTROLLER_IMG):$(RELEASE_ALIAS_TAG)

.PHONY: release-templates
release-templates: ## Generate release templates
	@mkdir -p $(RELEASE_DIR)
	cp templates/cluster-template*.yaml $(RELEASE_DIR)/

.PHONY: upload-staging-artifacts
upload-staging-artifacts: ## Upload release artifacts to the staging bucket
	gsutil cp $(RELEASE_DIR)/* gs://$(STAGING_BUCKET)/components/$(RELEASE_ALIAS_TAG)/

##@ hack/tools:

.PHONY: $(CONTROLLER_GEN_BIN)
$(CONTROLLER_GEN_BIN): $(CONTROLLER_GEN) ## Build a local copy of controller-gen.

.PHONY: $(CONVERSION_GEN_BIN)
$(CONVERSION_GEN_BIN): $(CONVERSION_GEN) ## Build a local copy of conversion-gen.

.PHONY: $(ENVSUBST_BIN)
$(ENVSUBST_BIN): $(ENVSUBST) ## Build a local copy of envsubst.

.PHONY: $(KUSTOMIZE_BIN)
$(KUSTOMIZE_BIN): $(KUSTOMIZE) ## Build a local copy of kustomize.

.PHONY: $(SETUP_ENVTEST_BIN)
$(SETUP_ENVTEST_BIN): $(SETUP_ENVTEST) ## Build a local copy of setup-envtest.

.PHONY: $(GINKGO_BIN)
$(GINKGO_BIN): $(GINKGO) ## Build a local copy of ginkgo.

.PHONY: $(GOLANGCI_LINT_BIN)
$(GOLANGCI_LINT_BIN): $(GOLANGCI_LINT) ## Build a local copy of golangci-lint.

.PHONY: $(MOCKGEN_BIN)
$(MOCKGEN_BIN): $(MOCKGEN) ## Build a local copy of mockgen.

$(CONTROLLER_GEN): # Build controller-gen from tools folder.
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) $(CONTROLLER_GEN_PKG) $(CONTROLLER_GEN_BIN) $(CONTROLLER_GEN_VER)

## We are forcing a rebuilt of conversion-gen via PHONY so that we're always using an up-to-date version.
## We can't use a versioned name for the binary, because that would be reflected in generated files.
.PHONY: $(CONVERSION_GEN)
$(CONVERSION_GEN): # Build conversion-gen from tools folder.
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) $(CONVERSION_GEN_PKG) $(CONVERSION_GEN_BIN) $(CONVERSION_GEN_VER)

$(ENVSUBST): # Build gotestsum from tools folder.
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) $(ENVSUBST_PKG) $(ENVSUBST_BIN) $(ENVSUBST_VER)

$(KUSTOMIZE): # Build kustomize from tools folder.
	CGO_ENABLED=0 GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) $(KUSTOMIZE_PKG) $(KUSTOMIZE_BIN) $(KUSTOMIZE_VER)

$(SETUP_ENVTEST): # Build setup-envtest from tools folder.
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) $(SETUP_ENVTEST_PKG) $(SETUP_ENVTEST_BIN) $(SETUP_ENVTEST_VER)

$(GINKGO): # Build ginkgo from tools folder.
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) $(GINKGO_PKG) $(GINKGO_BIN) $(GINGKO_VER)

$(GOLANGCI_LINT): # Build golangci-lint from tools folder.
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) $(GOLANGCI_LINT_PKG) $(GOLANGCI_LINT_BIN) $(GOLANGCI_LINT_VER)

$(MOCKGEN): # Build mockgen from tools folder.
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) $(MOCKGEN_PKG) $(MOCKGEN_BIN) $(MOCKGEN_VER)
