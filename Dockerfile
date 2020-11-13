FROM golang:alpine AS build
WORKDIR /go/src/app
RUN apk add git build-base
COPY . .
RUN CGO_ENABLED=0 GO111MODULE=on go install -ldflags "-X main.GitSHA=$(git rev-parse --short HEAD)" .
RUN go get github.com/google/go-licenses && go-licenses save ./... --save_path=/notices

FROM scratch
COPY --from=build /go/bin/dotege /dotege
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /notices /notices
COPY templates /templates
VOLUME /data/config
VOLUME /data/certs
VOLUME /data/output
ENTRYPOINT ["/dotege"]
