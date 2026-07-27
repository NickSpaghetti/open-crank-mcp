FROM ubuntu:24.04 AS c-harness-test

ARG PLAYDATE_SDK_VERSION=3.1.1
ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    tar \
    gcc \
    libc6-dev \
    make \
    && rm -rf /var/lib/apt/lists/*

ENV PLAYDATE_SDK_PATH=/opt/playdate-sdk
RUN curl -fsSL "https://download.panic.com/playdate_sdk/Linux/PlaydateSDK-${PLAYDATE_SDK_VERSION}.tar.gz" -o /tmp/playdate-sdk.tar.gz \
    && mkdir -p "${PLAYDATE_SDK_PATH}" \
    && tar -xzf /tmp/playdate-sdk.tar.gz --strip-components=1 -C "${PLAYDATE_SDK_PATH}" \
       "PlaydateSDK-${PLAYDATE_SDK_VERSION}/C_API" \
    && rm /tmp/playdate-sdk.tar.gz

WORKDIR /workspace
COPY c-harness/ ./c-harness/
COPY scripts/run-c-harness-tests.sh ./scripts/run-c-harness-tests.sh

FROM ubuntu:24.04 AS simulator

ARG PLAYDATE_SDK_VERSION=3.1.1
ARG GO_VERSION=1.26.5
ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    tar \
    xz-utils \
    xvfb \
    libwebkit2gtk-4.1-0 \
    libjavascriptcoregtk-4.1-0 \
    libgtk-3-0 \
    libnotify4 \
    libnss3 \
    libxss1 \
    libxtst6 \
    at-spi2-core \
    libsecret-1-0 \
    libpulse0 \
    x11vnc \
    novnc \
    websockify \
    pulseaudio \
    pulseaudio-utils \
    ffmpeg \
    build-essential \
    cmake \
    && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tar.gz \
    && tar -C /usr/local -xzf /tmp/go.tar.gz \
    && rm /tmp/go.tar.gz
ENV PATH="/usr/local/go/bin:${PATH}"

# Fetched from Panic's server on each user's own build. Never baked into a
# published image, see README.
ENV PLAYDATE_SDK_PATH=/opt/playdate-sdk
RUN curl -fsSL "https://download.panic.com/playdate_sdk/Linux/PlaydateSDK-${PLAYDATE_SDK_VERSION}.tar.gz" -o /tmp/playdate-sdk.tar.gz \
    && mkdir -p "${PLAYDATE_SDK_PATH}" \
    && tar -xzf /tmp/playdate-sdk.tar.gz --strip-components=1 -C "${PLAYDATE_SDK_PATH}" \
    && rm /tmp/playdate-sdk.tar.gz
ENV PATH="${PLAYDATE_SDK_PATH}/bin:${PATH}"

WORKDIR /workspace
COPY go.mod ./
RUN go mod download || true
COPY . .

# Built once here rather than left to `go run .` at connection time - a
# fresh `--rm` container has no persistent build cache, so `go run .`
# recompiles from scratch (~7-8s) on every single connection, which is
# real, noticeable latency/flakiness for anything driving this as an MCP
# server over stdio (e.g. another project's .mcp.json).
RUN go build -o /usr/local/bin/open-crank-mcp .

ENV DISPLAY=:99
# No audio device inside the container. SDL2 (used by PlaydateSimulator)
# needs an explicit driver or it refuses to start.
ENV SDL_AUDIODRIVER=dummy
CMD ["bash"]
