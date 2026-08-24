FROM golang:1.26-alpine AS build
WORKDIR /src
COPY main.go .
RUN go mod init hello-module && CGO_ENABLED=0 go build -ldflags="-s -w" -o /module .

FROM alpine:3.22
COPY --from=build /module /module
EXPOSE 8080
ENTRYPOINT ["/module"]
