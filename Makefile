# Set the default goal
.DEFAULT_GOAL := build
MAKEFLAGS += --no-print-directory

DOCKER ?= docker
KIND ?= kind

export KUBECONFIG ?= ${HOME}/.kube/config

# Active module mode, as we use Go modules to manage dependencies
export GO111MODULE=on
GOPATH=$(shell go env GOPATH)
GOBIN=$(GOPATH)/bin
GINKGO=$(GOBIN)/ginkgo

SOURCES := $(shell find . -name '*.go')

IMAGE_TAG := dev
TRIVY_OPERATOR_IMAGE := aquasec/trivy-operator:$(IMAGE_TAG)
TRIVY_OPERATOR_IMAGE_UBI8 := aquasec/trivy-operator:$(IMAGE_TAG)-ubi8

MKDOCS_IMAGE := aquasec/mkdocs-material:trivy-operator
MKDOCS_PORT := 8000

.PHONY: all
all: build

.PHONY: build
build: build-trivy-operator

## Builds the trivy-operator binary
build-trivy-operator: $(SOURCES)
	CGO_ENABLED=0 GOOS=linux go build -o ./bin/trivy-operator ./cmd/trivy-operator/main.go

.PHONY: get-ginkgo
## Installs Ginkgo CLI
get-ginkgo:
	@go install github.com/onsi/ginkgo/v2/ginkgo

.PHONY: get-qtc
## Installs quicktemplate compiler
get-qtc:
	@go install github.com/valyala/quicktemplate/qtc

.PHONY: compile-templates
## Converts quicktemplate files (*.qtpl) into Go code
compile-templates: get-qtc
	$(GOBIN)/qtc

.PHONY: test
## Runs both unit and integration tests
test: unit-tests itests-trivy-operator

.PHONY: unit-tests
## Runs unit tests with code coverage enabled
unit-tests: $(SOURCES)
	go test -v -short -race -timeout 30s -coverprofile=coverage.txt ./...

.PHONY: itests-trivy-operator
## Runs integration tests for Trivy Operator with code coverage enabled
itests-trivy-operator: check-kubeconfig get-ginkgo
	@$(GINKGO) \
	-coverprofile=coverage.txt \
	-coverpkg=github.com/aquasecurity/trivy-operator/pkg/operator,\
	github.com/aquasecurity/trivy-operator/pkg/operator/predicate,\
	github.com/aquasecurity/trivy-operator/pkg/operator/controller,\
	github.com/aquasecurity/trivy-operator/pkg/plugin,\
	github.com/aquasecurity/trivy-operator/pkg/plugin/trivy,\
	github.com/aquasecurity/trivy-operator/pkg/configauditreport,\
	github.com/aquasecurity/trivy-operator/pkg/vulnerabilityreport \
	./itest/trivy-operator

.PHONY: check-kubeconfig
check-kubeconfig:
ifndef KUBECONFIG
	$(error Environment variable KUBECONFIG is not set)
else
	@echo "KUBECONFIG=${KUBECONFIG}"
endif

## Removes build artifacts
clean:
	@rm -r ./bin 2> /dev/null || true
	@rm -r ./dist 2> /dev/null || true

## Builds Docker images for all binaries
docker-build: \
	docker-build-trivy-operator \
	docker-build-trivy-operator-ubi8

## Builds Docker image for trivy-operator
docker-build-trivy-operator: build-trivy-operator
	$(DOCKER) build --no-cache -t $(TRIVY_OPERATOR_IMAGE) -f build/trivy-operator/Dockerfile bin
	
## Builds Docker image for trivy-operator ubi8
docker-build-trivy-operator-ubi8: build-trivy-operator
	$(DOCKER) build --no-cache -f build/trivy-operator/Dockerfile.ubi8 -t $(TRIVY_OPERATOR_IMAGE_UBI8) bin

kind-load-images: \
	docker-build-trivy-operator \
	docker-build-trivy-operator-ubi8
	$(KIND) load docker-image \
		$(TRIVY_OPERATOR_IMAGE) \
		$(TRIVY_OPERATOR_IMAGE_UBI8)

## Runs MkDocs development server to preview the documentation page
mkdocs-serve:
	$(DOCKER) build -t $(MKDOCS_IMAGE) -f build/mkdocs-material/Dockerfile bin
	$(DOCKER) run --name mkdocs-serve --rm -v $(PWD):/docs -p $(MKDOCS_PORT):8000 $(MKDOCS_IMAGE)

$(GOBIN)/labeler:
	go install github.com/knqyf263/labeler@latest

.PHONY: label
label: $(GOBIN)/labeler
	labeler apply misc/triage/labels.yaml -r aquasecurity/trivy-operator -l 5

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## controller-gen version and binary path
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
CONTROLLER_TOOLS_VERSION ?= v0.9.2

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: verify-generated
verify-generated: generate
	./hack/verify-generated.sh

.PHONY: generate
generate: controller-gen
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./pkg/apis/..."

.PHONY: \
	clean \
	docker-build \
	docker-build-trivy-operator \
	docker-build-trivy-operator-ubi8 \
	kind-load-images \
	mkdocs-serve
