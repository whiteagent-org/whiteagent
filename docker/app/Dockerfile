# Build stage
FROM golang:1.24-bookworm AS builder
ARG VERSION=dev
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN make build VERSION=$VERSION

# Runtime stage
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /build/bin/whiteagent .
COPY --from=builder /build/bin/plugins/*.so ./plugins/
EXPOSE 8080
ENTRYPOINT ["./whiteagent"]
CMD ["serve"]
