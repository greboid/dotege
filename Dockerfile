FROM golang:alpine AS build
WORKDIR /go/src/app
RUN apk add git build-base
COPY . .
RUN CGO_ENABLED=0 GO111MODULE=on go install .

FROM scratch
COPY --from=build /go/bin/dotege /dotege
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY templates /templates
VOLUME /data/config
VOLUME /data/certs
VOLUME /data/output
ENTRYPOINT ["/dotege"]
