FROM reg.c5h.io/golang AS build
WORKDIR /go/src/app
COPY . .

RUN set -eux; \
    apk add git build-base; \
    CGO_ENABLED=0 GO111MODULE=on go install -ldflags "-X main.GitSHA=$(git rev-parse --short HEAD)" ./cmd/dotege; \
    go run github.com/google/go-licenses@latest save ./... --save_path=/notices;

FROM reg.c5h.io/base
COPY --from=build /go/bin/dotege /dotege
COPY --from=build /notices /notices
COPY templates /templates
VOLUME /data/config
VOLUME /data/certs
VOLUME /data/output
ENTRYPOINT ["/dotege"]
