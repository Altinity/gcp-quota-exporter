FROM golang:1.15-alpine as alpine

RUN apk add --no-cache git ca-certificates make

ENV GO111MODULE=on
WORKDIR /app
COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . .
RUN make build

FROM alpine:3.12

COPY --from=alpine /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=alpine /app/gcp-quota-exporter /app/

WORKDIR /app
RUN chmod +x /app/gcp-quota-exporter
ENTRYPOINT ["/app/gcp-quota-exporter"]
