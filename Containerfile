# Headless mahpastes server. Build:  podman build -t mahpastesd -f Containerfile .
# Run:  podman run -d -p 127.0.0.1:44557:44557 -v mahpastesd-data:/data mahpastesd
# Keep in step with GO_VERSION in .github/workflows/build.yml and go.mod's toolchain.
FROM golang:1.26.6 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/mahpastesd ./cmd/mahpastesd \
 && mkdir /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/mahpastesd /usr/local/bin/mahpastesd
# Pre-created so a fresh volume is writable by the nonroot user.
COPY --from=build --chown=65532:65532 /out/data /data
# Must be reachable from outside the container, so bind all interfaces. Publish
# the port on loopback only (or behind a TLS proxy and set MAHPASTESD_TRUST_PROXY=1).
ENV MAHPASTES_DATA_DIR=/data \
    MAHPASTESD_PORT=44557 \
    MAHPASTESD_BIND_ALL=1
VOLUME /data
EXPOSE 44557
USER nonroot
ENTRYPOINT ["/usr/local/bin/mahpastesd"]
