FROM golang:alpine

WORKDIR /go/src/app
COPY . .

RUN apk add git build-base

RUN GO111MODULE=on go install -v ./...

CMD ["dotege"]
