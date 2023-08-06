FROM golang:1.20.7-alpine as builder

WORKDIR /go/src/github.com/aokumasan/nifcloud-additional-storage-csi-driver
RUN apk add --no-cache make git
ADD . .
RUN make build

FROM alpine:3.10.3

RUN apk add --no-cache util-linux e2fsprogs xfsprogs blkid e2fsprogs-extra xfsprogs-extra
COPY --from=builder /go/src/github.com/aokumasan/nifcloud-additional-storage-csi-driver/bin/nifcloud-additional-storage-csi-driver /bin/nifcloud-additional-storage-csi-driver
ENTRYPOINT ["/bin/nifcloud-additional-storage-csi-driver"]
