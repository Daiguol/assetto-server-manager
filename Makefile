VERSION?=unstable
ifeq ($(VERSION),)
VERSION := "unstable"
endif

# enable go modules
export GO111MODULE=on

all: clean vet test assets build

install-linter:
	which golangci-lint || curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin v1.27.0

clean:
	$(MAKE) -C cmd/server-manager clean

test:
	mkdir -p cmd/server-manager/assetto/cfg
	mkdir -p cmd/server-manager/assetto/results
	cp -R fixtures/results/*.json cmd/server-manager/assetto/results
	go test -race

vet: install-linter
	go vet ./...
	golangci-lint -E bodyclose,misspell,gofmt,golint,unconvert,goimports,depguard,interfacer run --timeout 30m --skip-files content_cars_skins.go,plugin_kissmyrank_config.go,plugin_realpenalty_config.go

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
