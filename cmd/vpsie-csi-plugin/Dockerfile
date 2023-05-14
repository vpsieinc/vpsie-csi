FROM amd64/alpine:3.16

RUN apk add --no-cache ca-certificates \
                       e2fsprogs \
                       findmnt \
                       xfsprogs \
                       xfsprogs-extra \
                       util-linux \
                       e2fsprogs-extra

ADD do-csi-plugin /bin/

ENTRYPOINT ["/bin/do-csi-plugin"]

ENTRYPOINT ["/bin/vpsie-csi-plugin"]
