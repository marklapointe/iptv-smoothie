# Multi-stage Smoothie image (Linux OCI). FreeBSD uses bare-metal build.
FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/smoothie ./cmd/smoothie

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/smoothie /app/smoothie
ENV SMOOTHIE_LISTEN=0.0.0.0:8787 \
    SMOOTHIE_DATA_DIR=/data \
    SMOOTHIE_DB=/data/smoothie.db
VOLUME ["/data"]
EXPOSE 8787
USER nonroot:nonroot
ENTRYPOINT ["/app/smoothie"]
