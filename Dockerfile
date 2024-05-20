FROM golang:1.21-alpine AS build

RUN apk add --no-cache git

WORKDIR /workspace

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build  -o vpsie-csi-plugin -v ./cmd/vpsie-csi-plugin

FROM alpine:latest
RUN apk add --no-cache ca-certificates

COPY --from=build /workspace/vpsie-csi-plugin /usr/local/bin/vpsie-csi-plugin
ENTRYPOINT ["vpsie-csi-plugin"] 