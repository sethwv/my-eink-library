FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
EXPOSE 8080
VOLUME ["/library", "/data"]
USER nonroot:nonroot
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s CMD ["/server", "-healthcheck"]
ENTRYPOINT ["/server"]
