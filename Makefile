PKG=github.com/aokumasan/nifcloud-additional-storage-csi-driver
IMAGE?=ghcr.io/aokumasan/nifcloud-additional-storage-csi-driver
VERSION=v0.1.0
GIT_COMMIT?=$(shell git rev-parse HEAD)
BUILD_DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS?="-X ${PKG}/pkg/driver.driverVersion=${VERSION} -X ${PKG}/pkg/driver.gitCommit=${GIT_COMMIT} -X ${PKG}/pkg/driver.buildDate=${BUILD_DATE} -s -w"

build:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux go build -ldflags ${LDFLAGS} -o bin/nifcloud-additional-storage-csi-driver ./cmd/

test:
	go test -cover ./...

image:
	docker build -t $(IMAGE):$(VERSION) .

push:
	docker push $(IMAGE):$(VERSION)

helm-package:
	cd charts; helm package nifcloud-additional-storage-csi-driver
