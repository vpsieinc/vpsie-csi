FROM debian:11

RUN apt-get update && apt-get install -y ca-certificates

ADD vpsie-csi-plugin /usr/local/bin/

ENTRYPOINT ["/usr/local/bin/vpsie-csi-plugin"]
