PKG=github.com/aokumasan/nifcloud-additional-storage-csi-driver
IMAGE?=aokumasan/nifcloud-additional-storage-csi-driver
VERSION=v0.0.3
GIT_COMMIT?=$(shell git rev-parse HEAD)
BUILD_DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS?="-X ${PKG}/pkg/driver.driverVersion=${VERSION} -X ${PKG}/pkg/driver.gitCommit=${GIT_COMMIT} -X ${PKG}/pkg/driver.buildDate=${BUILD_DATE} -s -w"

build:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux go build -ldflags ${LDFLAGS} -o bin/nifcloud-additional-storage-csi-driver ./cmd/

image:
	docker build -t $(IMAGE):latest .

push:
	docker push $(IMAGE):latest
