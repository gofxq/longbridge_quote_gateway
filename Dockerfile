# Keep the build toolchain on a patched, minimal base; CGO is disabled below.
FROM golang:1.27.4-alpine3.24 AS builder
WORKDIR /src
COPY go.mod go.sum ./
COPY third_party/openapi-go ./third_party/openapi-go
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/quote-gateway ./cmd/server
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /out/quote-gateway /quote-gateway
USER nonroot:nonroot
EXPOSE 12280
ENTRYPOINT ["/quote-gateway"]
