FROM golang:1.25.6-alpine AS builder
WORKDIR /build
COPY . .
RUN \
  CGO_ENABLED=0 \
  go build -o main cmd/main.go

FROM busybox:1.36.1
COPY --from=builder /build/main /main
ENTRYPOINT ["/main"]
