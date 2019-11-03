FROM golang:1.13.3-alpine as builder

WORKDIR /go/src/github.com/aokumasan/nifcloud-additional-storage-csi-driver
RUN apk add --no-cache make git
ADD . .
RUN make build

FROM alpine:3.10.3

RUN apk add --no-cache open-vm-tools util-linux e2fsprogs xfsprogs
COPY --from=builder /go/src/github.com/aokumasan/nifcloud-additional-storage-csi-driver/bin/nifcloud-additional-storage-csi-driver /bin/nifcloud-additional-storage-csi-driver
ENTRYPOINT ["/bin/nifcloud-additional-storage-csi-driver"]
