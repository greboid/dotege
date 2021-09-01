FROM golang:alpine AS build
WORKDIR /go/src/app
COPY . .

RUN set -eux; \
    apk add git build-base; \
    CGO_ENABLED=0 GO111MODULE=on go install -ldflags "-X main.GitSHA=$(git rev-parse --short HEAD)" .; \
    go install github.com/google/go-licenses && go-licenses save ./... --save_path=/notices;

FROM gcr.io/distroless/base:nonroot@sha256:a74f307185001c69bc362a40dbab7b67d410a872678132b187774fa21718fa13
COPY --from=build /go/bin/dotege /dotege
COPY --from=build /notices /notices
COPY templates /templates
VOLUME /data/config
VOLUME /data/certs
VOLUME /data/output
ENTRYPOINT ["/dotege"]
