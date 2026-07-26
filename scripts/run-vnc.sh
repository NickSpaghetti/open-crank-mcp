#!/usr/bin/env bash
set -euo pipefail

export DISPLAY=:99
export PULSE_RUNTIME_PATH=/tmp/pulse-vnc
export PULSE_SERVER="unix:${PULSE_RUNTIME_PATH}/native"
export SDL_AUDIODRIVER=pulseaudio

Xvfb "$DISPLAY" -screen 0 1280x800x24 &
sleep 1

# A per-container daemon with its own null sink, not a bridge to any host
# audio server - this profile has to work identically with no host audio
# infrastructure at all (that's the whole point of the VNC fallback).
mkdir -p "$PULSE_RUNTIME_PATH"
pulseaudio -D --exit-idle-time=-1 --disallow-exit --system=false
pactl load-module module-null-sink sink_name=vnc_sink sink_properties=device.description=vnc_sink
pactl set-default-sink vnc_sink

x11vnc -display "$DISPLAY" -forever -shared -nopw -rfbport 5900 -quiet &
websockify --web=/usr/share/novnc 6080 localhost:5900 &

# ffmpeg's -listen HTTP server serves exactly one client per invocation and
# exits when that client disconnects, so it's wrapped in a restart loop to
# survive reconnects (e.g. re-opening the stream in a browser).
( set +e; while true; do
  ffmpeg -loglevel error -f pulse -i vnc_sink.monitor -c:a libmp3lame -f mp3 \
    -listen 1 http://0.0.0.0:8000/stream.mp3
  sleep 0.2
done ) &

echo "video: http://localhost:6080/vnc.html"
echo "audio: http://localhost:8000/stream.mp3"

exec bash
