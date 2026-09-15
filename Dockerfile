FROM golang:1.25-alpine AS build
ARG BUILD_VERSION=dev
ARG BUILD_DATE=unknown
WORKDIR /src
COPY src/go.mod src/go.sum* ./
RUN go mod download
COPY src/ .
RUN if [ "$BUILD_DATE" = "unknown" ]; then BUILD_DATE=$(date -u +%F); fi; \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.buildVersion=${BUILD_VERSION} -X main.buildDate=${BUILD_DATE}" -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
EXPOSE 8080
VOLUME ["/library", "/data"]
USER nonroot:nonroot
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s CMD ["/server", "-healthcheck"]
ENTRYPOINT ["/server"]
