# syntax=docker/dockerfile:1

FROM golang:1.24-alpine AS build

WORKDIR /src

RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/notification-agent ./cmd/notification-agent

FROM alpine:3.20

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=build /out/notification-agent /app/notification-agent

EXPOSE 8080

ENTRYPOINT ["/app/notification-agent"]
