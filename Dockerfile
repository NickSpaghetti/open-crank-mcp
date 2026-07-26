FROM ubuntu:24.04

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
    && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tar.gz \
    && tar -C /usr/local -xzf /tmp/go.tar.gz \
    && rm /tmp/go.tar.gz
ENV PATH="/usr/local/go/bin:${PATH}"

# Fetched from Panic's own server during each user's local build, never baked
# into a published image - see README.md.
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

ENV DISPLAY=:99
# No real audio device inside the container; SDL2 (used by PlaydateSimulator)
# needs an explicit driver or it refuses to start entirely.
ENV SDL_AUDIODRIVER=dummy
CMD ["bash"]
