# syntax=docker/dockerfile:1.6

# -----------------------------------------------------------------------------
# Stage 1: Frontend assets
#
# Vite + Dart Sass + TypeScript 5 — pure JS toolchain, no native compile step.
# Alpine keeps the image slim; .npmrc pins legacy-peer-deps + ignore-scripts so
# bootstrap-switch/summernote quirks don't break the install.
# -----------------------------------------------------------------------------
FROM node:20-alpine AS assets

WORKDIR /src/cmd/server-manager/typescript

COPY cmd/server-manager/typescript/package.json \
     cmd/server-manager/typescript/package-lock.json \
     cmd/server-manager/typescript/.npmrc \
     ./
RUN npm ci

COPY cmd/server-manager/typescript/ ./
RUN npm run build


# -----------------------------------------------------------------------------
# Stage 2: Go binary
#
# Compiles the server-manager with embedded assets (//go:embed — no generate
# step, no external tooling).
# -----------------------------------------------------------------------------
FROM golang:1.23-bookworm AS gobuilder

ARG SM_VERSION=dev
ENV CGO_ENABLED=0

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Overlay the frontend bundle/css generated in stage 1 on top of the static
# directory — only the generated files are copied; favicon, img/ and static.go
# remain from the source tree.
COPY --from=assets /src/cmd/server-manager/static/js  ./cmd/server-manager/static/js
COPY --from=assets /src/cmd/server-manager/static/css ./cmd/server-manager/static/css

RUN go build -trimpath \
      -ldflags="-s -w -X github.com/JustaPenguin/assetto-server-manager.BuildVersion=${SM_VERSION}" \
      -o /out/server-manager \
      ./cmd/server-manager


# -----------------------------------------------------------------------------
# Stage 3: Runtime
#
# debian:12-slim with 32-bit libs for SteamCMD (dedicated server binary is 32
# bit). lib32gcc-s1 replaces the EOL lib32gcc1 name used on Ubuntu 18.04.
# -----------------------------------------------------------------------------
FROM debian:12-slim AS runtime

ENV DEBIAN_FRONTEND=noninteractive \
    LANG=C.UTF-8 \
    STEAMROOT=/opt/steamcmd \
    SERVER_USER=assetto \
    SERVER_MANAGER_DIR=/home/assetto/server-manager \
    SERVER_INSTALL_DIR=/home/assetto/server-manager/assetto

RUN dpkg --add-architecture i386 \
 && apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates curl tzdata \
      lib32gcc-s1 lib32stdc++6 \
      libc6:i386 libstdc++6:i386 zlib1g:i386 \
 && rm -rf /var/lib/apt/lists/*

RUN mkdir -p ${STEAMROOT} \
 && curl -fsSL https://media.steampowered.com/installer/steamcmd_linux.tar.gz \
    | tar -xz -C ${STEAMROOT}
ENV PATH="${STEAMROOT}:${PATH}"

RUN useradd -ms /bin/bash ${SERVER_USER} \
 && mkdir -p ${SERVER_MANAGER_DIR} ${SERVER_INSTALL_DIR} \
 && chown -R ${SERVER_USER}:${SERVER_USER} /home/${SERVER_USER}

COPY --from=gobuilder /out/server-manager /usr/local/bin/server-manager

USER ${SERVER_USER}
WORKDIR ${SERVER_MANAGER_DIR}

VOLUME ["${SERVER_MANAGER_DIR}"]

EXPOSE 8772 9600/udp 8081

HEALTHCHECK --interval=30s --timeout=5s --start-period=60s --retries=3 \
  CMD curl -fsS http://localhost:8772/healthcheck.json || exit 1

ENTRYPOINT ["server-manager"]
