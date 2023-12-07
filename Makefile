PKG=github.com/nifcloud/nifcloud-additional-storage-csi-driver
IMAGE?=ghcr.io/nifcloud/nifcloud-additional-storage-csi-driver
VERSION=$(shell git describe --tags --dirty --match="v*")
CHART_VERSION=${VERSION:v%=%}
GIT_COMMIT?=$(shell git rev-parse HEAD)
BUILD_DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS?="-X ${PKG}/pkg/driver.driverVersion=${VERSION} -X ${PKG}/pkg/driver.gitCommit=${GIT_COMMIT} -X ${PKG}/pkg/driver.buildDate=${BUILD_DATE} -s -w"

lint:
	golangci-lint run

build:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux go build -ldflags ${LDFLAGS} -o bin/nifcloud-additional-storage-csi-driver ./cmd/

test:
	ginkgo run --cover -coverprofile=cover.out -covermode=count --junit-report junit-report.xml ./...

image:
	docker build -t $(IMAGE):$(VERSION) .

push:
	docker push $(IMAGE):$(VERSION)

helm-package:
	cd charts; helm package nifcloud-additional-storage-csi-driver -d ../.cr-release-packages --version=${CHART_VERSION} --app-version=${VERSION}
