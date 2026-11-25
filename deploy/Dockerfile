# Web stage: builds the app's pages from this commit's source and lockfile,
# so an image from any commit always shows that commit's pages. It runs on
# the build machine for every target; Node never reaches the final image.
FROM --platform=$BUILDPLATFORM node:24.22.1-trixie-slim@sha256:43b9ba1142b8dcf33fa8f922ca1be85714adc444e0586c7cf7726daaeabc1bea AS web
WORKDIR /src/app/frontend
COPY app/frontend/package.json app/frontend/package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY app/frontend/ ./
# Only English is built in; the other languages are signed language packs.
RUN npm run build

# Build stage: runs on the build machine and cross-compiles for the target,
# so multi-arch builds (amd64, arm64, armv7) need no emulation.
FROM --platform=$BUILDPLATFORM golang:1.27.4@sha256:08bcb99a38f85a265f9a93d1d3323e2440eec333380f3ea26ee0dec9264bb41c AS build
ARG TARGETOS=linux
ARG TARGETARCH
ARG TARGETVARIANT
ARG VERSION
# Optional build-only resource controls; omitted values keep Go's defaults.
ARG GOMAXPROCS
ARG GOFLAGS
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
# The pages just built are what the program embeds.
RUN rm -rf app/frontend/dist
COPY --from=web /src/app/frontend/dist ./app/frontend/dist
# Inlining is off in HolyArrs's own packages and in the parts of Go's library
# and the sign-in libraries that are not speed-critical (about 1.3 MB less to
# download, same measured speed). The database, text handling, unpacking and
# database/sql keep it, where it matters for speed.
RUN set -eu; \
    ldflags="-s -w"; \
    if [ -n "${VERSION:-}" ]; then ldflags="$ldflags -X main.version=$VERSION"; fi; \
    noinline=""; \
    for p in github.com/HolyArrs/holyarrs/... \
        net/http/... crypto/tls/... crypto/x509/... net net/url net/textproto net/mail net/netip net/smtp mime/... \
        os os/exec os/signal os/user log/... encoding/asn1 encoding/pem encoding/csv archive/... vendor/... \
        io/fs path/filepath html/... text/... flag embed \
        github.com/go-jose/... github.com/coreos/... golang.org/x/oauth2/... github.com/spf13/... go4.org/... \
        github.com/google/... github.com/dustin/... github.com/hashicorp/...; do \
        noinline="$noinline -gcflags=$p=-l"; \
    done; \
    GOARM="${TARGETVARIANT#v}" CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath $noinline -ldflags="$ldflags" -o /out/holyarrs ./app/backend/cmd/holyarrs
# The folders the bare image needs: /config for the owner (1000:1000) and a
# /tmp anyone may write (Move's working copies, temporary files).
RUN mkdir -p /out/config /out/tmp && chown 1000:1000 /out/config && chmod 1777 /out/tmp

# yt-dlp stage: fetches the pinned yt-dlp (the platform-free Python build,
# so amd64, arm64 and armv7 share one file) and checks it against the hash
# yt-dlp published for that release. A newer one can be dropped into
# /config/yt-dlp/yt-dlp at any time; HolyArrs prefers it.
FROM --platform=$BUILDPLATFORM debian:trixie-slim@sha256:8aa5a7bf5a1955bddbe7d74fcdd0b91ab30e74362a8b73af672b0481b35b44a0 AS ytdlp
ARG TARGETARCH
ARG YTDLP_VERSION=2026.10.26
ARG YTDLP_SHA256=e51e69e89489f199530f13d32a4342da3ddb2cf34842f364603a066db05afbf3
# YouTube needs a JavaScript runtime for its full list of formats; yt-dlp
# uses deno. deno has no armv7 build, so armv7 images go without it.
ARG DENO_VERSION=v2.9.8
ARG DENO_SHA256_AMD64=e890975bc2549807bbcc0b584ffafda72cc7c93349dd99c80483dd784f6c4609
ARG DENO_SHA256_ARM64=d49596c20b9788cf8f5d8ddf644103d4bde56ed239753f0bdb92ba9361d51905
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl unzip && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL -o /yt-dlp "https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}/yt-dlp" \
    && echo "${YTDLP_SHA256}  /yt-dlp" | sha256sum -c - \
    && chmod 0755 /yt-dlp
RUN set -eu; mkdir /js; \
    case "$TARGETARCH" in \
        amd64) name=x86_64; sum="$DENO_SHA256_AMD64" ;; \
        arm64) name=aarch64; sum="$DENO_SHA256_ARM64" ;; \
        *) exit 0 ;; \
    esac; \
    curl -fsSL -o /deno.zip "https://github.com/denoland/deno/releases/download/${DENO_VERSION}/deno-${name}-unknown-linux-gnu.zip"; \
    echo "${sum}  /deno.zip" | sha256sum -c -; \
    unzip -q /deno.zip -d /js; chmod 0755 /js/deno; rm /deno.zip

# Final stage: a slim Debian with HolyArrs, plus Python, ffmpeg and deno for
# the built-in yt-dlp Downloader. HolyArrs itself is still one static Go program
# with its own time zone database, RAR/7z/zip reader and healthcheck.
FROM debian:trixie-slim@sha256:8aa5a7bf5a1955bddbe7d74fcdd0b91ab30e74362a8b73af672b0481b35b44a0
ARG VERSION=dev
LABEL org.opencontainers.image.title="HolyArrs" \
      org.opencontainers.image.source="https://github.com/HolyArrs/HolyArrs" \
      org.opencontainers.image.licenses="AGPL-3.0-only" \
      org.opencontainers.image.version="$VERSION"
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates python3 ffmpeg \
    && rm -rf /var/lib/apt/lists/*
COPY --from=ytdlp /yt-dlp /usr/local/bin/yt-dlp
COPY --from=ytdlp /js/ /usr/local/bin/
COPY --from=build /out/holyarrs /holyarrs
COPY --from=build --chown=1000:1000 /out/config /config
COPY LICENSE /LICENSE
# No GOMEMLIMIT here: the program aims at 384 MiB by itself, and on a small
# machine or a container limited to about 2.5 GiB or less it turns on its
# low-memory mode and aims lower (a quarter of the memory, 192-384 MiB; see
# docs/run/parts-safe-mode.md). SQLite's page caches and the driver's own heap
# sit outside that target (measured up to about 69 MiB with a 12,800-Item
# Library), so about 20 percent of a 500 MB limit stays headroom.
# Set GOMEMLIMIT to choose the target.
# No USER line: the image starts as root, gives /config to PUID:PGID (default
# 1000:1000) when its owner differs, then restarts itself as that user and
# never keeps running as root. Run with user: "<uid>:<gid>" to skip the switch.
# container=oci marks this as a container on every runtime: Docker and Podman
# leave marker files, but containerd (Kubernetes, k3s, nerdctl) does not, and
# outside a container a root start without PUID/PGID is refused.
ENV container=oci
EXPOSE 6262
VOLUME ["/config"]
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 CMD ["/holyarrs", "-healthcheck"]
ENTRYPOINT ["/holyarrs"]
# No -addr here, so HOLYARRS_ADDR or HOLYARRS_PORT can move the port (default 6262).
CMD ["-config", "/config"]
