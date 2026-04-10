FROM golang:1.24-alpine AS builder

WORKDIR /app
ENV GOTOOLCHAIN=auto
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /solr-mem-server ./cmd/solr-mem-server
RUN CGO_ENABLED=0 GOOS=linux go build -o /solr-mem-indexer ./cmd/solr-mem-indexer

FROM alpine:latest
RUN apk --no-cache add ca-certificates git
COPY --from=builder /solr-mem-server /usr/local/bin/solr-mem-server
COPY --from=builder /solr-mem-indexer /usr/local/bin/solr-mem-indexer
ENTRYPOINT ["solr-mem-server"]
