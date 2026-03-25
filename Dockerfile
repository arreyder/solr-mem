FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /solr-mem-server ./cmd/solr-mem-server

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /solr-mem-server /usr/local/bin/solr-mem-server
ENTRYPOINT ["solr-mem-server"]
