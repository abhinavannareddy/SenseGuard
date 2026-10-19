FROM golang:1.26.9-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
RUN mkdir -p /out /data && CGO_ENABLED=0 GOOS=linux go build -mod=readonly -trimpath -buildvcs=false -ldflags="-s -w" -o /out/senseguard ./cmd/senseguard

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build --chown=65532:65532 /out/senseguard /senseguard
COPY --from=build --chown=65532:65532 /data /data
EXPOSE 8080
USER nonroot:nonroot
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 CMD ["/senseguard", "healthcheck"]
ENTRYPOINT ["/senseguard"]
