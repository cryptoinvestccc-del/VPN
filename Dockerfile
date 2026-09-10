# Build the binaries statically so the runtime image can be minimal.
FROM golang:1.26-alpine AS build

WORKDIR /src

# Dependencies first, so editing source does not re-download the module
# cache on every build.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO off gives a static binary that runs on an image with no libc.
# Trimpath keeps build machine paths out of the binary.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/obfsserver ./cmd/obfsserver && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/obfsclient ./cmd/obfsclient && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gencert    ./cmd/gencert && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/obfsctl    ./cmd/obfsctl

# The runtime image carries the binaries and nothing else — no shell, no
# package manager, no libc. A VPN server is exposed to the internet by
# design, so anything that would help an attacker after a compromise is
# better left out of the image entirely.
FROM scratch

# Certificate authorities are not needed to serve the tunnel — the client
# pins our certificate rather than validating a chain — but an operator
# pointing fallback_addr at an HTTPS backend would need them, so they are
# carried across.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=build /out/obfsserver /obfsserver
COPY --from=build /out/obfsclient /obfsclient
COPY --from=build /out/gencert /gencert
COPY --from=build /out/obfsctl /obfsctl

# An unprivileged, non-existent-on-the-host uid. Binding 443 needs
# NET_BIND_SERVICE granted to the container, not root inside it; the
# compose file shows how.
USER 65532:65532

# Config and credentials are mounted in; nothing sensitive lives in the
# image.
VOLUME ["/etc/obfsvpn"]

ENTRYPOINT ["/obfsserver"]
CMD ["-config", "/etc/obfsvpn/obfsserver.yaml"]
