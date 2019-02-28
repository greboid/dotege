FROM golang:alpine

WORKDIR /go/src/app

RUN apk add git build-base

COPY . .
RUN GO111MODULE=on go install -v ./...

CMD ["dotege"]
