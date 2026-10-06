APP      := gubexplorer
IMAGE    ?= $(APP)
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || cat VERSION 2>/dev/null || echo "dev")
GOOS     ?= $(shell go env GOOS)
GOARCH   ?= $(shell go env GOARCH)

KUBECONFIG ?= $(HOME)/.kube/config
NAMESPACE  ?= default
AUTH_USER  ?= toto
AUTH_PASS  ?= toto

.PHONY: build run dev docker docker-run helm-lint helm-install helm-dev test clean

## build: compile binary into ./bin/
build:
	go build -ldflags="-s -w -X main.version=$(VERSION)" -o bin/$(APP) .

## run: build then run (AUTH_USER and AUTH_PASS must be set)
run: build
	AUTH_USERNAME=$(AUTH_USER) AUTH_PASSWORD=$(AUTH_PASS) ./bin/$(APP)

## dev: run locally using specified kubeconfig (AUTH_USER and AUTH_PASS must be set)
dev: build
	AUTH_USERNAME=$(AUTH_USER) AUTH_PASSWORD=$(AUTH_PASS) \
	./bin/$(APP) --kubeconfig=$(KUBECONFIG) --namespace=$(NAMESPACE)

## test: run unit tests
test:
	go test ./...

## docker: build Docker image
docker:
	docker build -t $(IMAGE):$(VERSION) .

## docker-run: build image and run it locally with mounted kubeconfig
docker-run: docker
	docker run --rm -p 8080:8080 \
		-v $(KUBECONFIG):/kubeconfig:ro \
		-e KUBECONFIG=/kubeconfig \
		$(IMAGE):$(VERSION)

## helm-lint: lint the Helm chart
helm-lint:
	helm lint charts/$(APP)

## helm-install: install/upgrade the chart in-cluster (in-cluster RBAC auth)
helm-install:
	helm upgrade --install $(APP) charts/$(APP) \
		--namespace $(NAMESPACE) \
		--create-namespace \
		--set image.tag=$(VERSION) \
		$(if $(AUTH_USER),--set auth.enabled=true --set auth.username=$(AUTH_USER) --set auth.password=$(AUTH_PASS),)

## helm-dev: install/upgrade with a mounted kubeconfig (for dev clusters or external auth)
helm-dev:
	helm upgrade --install $(APP) charts/$(APP) \
		--namespace $(NAMESPACE) \
		--create-namespace \
		--set image.tag=$(VERSION) \
		--set kubeconfig.enabled=true \
		--set-file kubeconfig.value=$(KUBECONFIG)

## clean: remove build artefacts
clean:
	rm -rf bin/

help:
	@grep -E '^##' Makefile | sed 's/## /  /'
