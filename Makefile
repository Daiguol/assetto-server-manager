VERSION?=unstable
ifeq ($(VERSION),)
VERSION := "unstable"
endif

# enable go modules
export GO111MODULE=on

all: clean vet test assets build

GOLANGCI_LINT_VERSION := v2.11.4

install-linter:
	@if ! golangci-lint version 2>/dev/null | grep -q "$(patsubst v%,%,$(GOLANGCI_LINT_VERSION))"; then \
		go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION); \
	fi

install-hooks:
	git config core.hooksPath .githooks
	@echo "pre-commit hook active. Bypass with 'git commit --no-verify'."

clean:
	$(MAKE) -C cmd/server-manager clean

test:
	mkdir -p cmd/server-manager/assetto/cfg
	mkdir -p cmd/server-manager/assetto/results
	cp -R fixtures/results/*.json cmd/server-manager/assetto/results
	go test -race

coverage:
	mkdir -p cmd/server-manager/assetto/cfg
	mkdir -p cmd/server-manager/assetto/results
	cp -R fixtures/results/*.json cmd/server-manager/assetto/results
	go test ./... -coverprofile=coverage.out -covermode=atomic
	@go tool cover -func=coverage.out | tail -1

vet: install-linter
	go vet ./...
	golangci-lint run ./...

assets:
	$(MAKE) -C cmd/server-manager assets

build:
	$(MAKE) -C cmd/server-manager build

deploy: clean vet test
	$(MAKE) -C cmd/server-manager deploy

run:
	$(MAKE) -C cmd/server-manager run

# Build a local Docker image. Override IMAGE=your/repo to push elsewhere.
IMAGE?=acsm

docker:
	docker build --build-arg SM_VERSION=${VERSION} -t ${IMAGE}:${VERSION} .
ifneq ("${VERSION}", "unstable")
	docker tag ${IMAGE}:${VERSION} ${IMAGE}:latest
endif

docker-push:
	docker push ${IMAGE}:${VERSION}
ifneq ("${VERSION}", "unstable")
	docker push ${IMAGE}:latest
endif

compose:
	docker compose build
	docker compose up -d
