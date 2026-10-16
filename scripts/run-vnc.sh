#!/usr/bin/env bash
set -euo pipefail

# shellcheck source=scripts/shared-lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/shared-lib.sh"

export DISPLAY=:99
# simulator-shared sets PULSE_SERVER for exec'd servers too; follow it so the two can't drift.
export PULSE_SERVER="${PULSE_SERVER:-unix:/tmp/pulse-vnc/native}"
PULSE_RUNTIME_PATH="$(dirname "${PULSE_SERVER#unix:}")"
export PULSE_RUNTIME_PATH
export SDL_AUDIODRIVER=pulseaudio

SIM_ZOOM="${SIM_ZOOM:-1}"

# A workspace larger than the Simulator (see shared_window_size), which is pinned
# top-left so the rest is room for its console and debugging windows.
VNC_GEOMETRY="${VNC_GEOMETRY:-1280x800}"

Xvfb "$DISPLAY" -screen 0 "${VNC_GEOMETRY}x24" &
sleep 1

# A window manager gives drag, resize and keyboard focus; without one, keys go to
# whatever is under the pointer. One desktop, because a stray scroll over the
# background switches desktops and the Simulator appears to vanish.
mkdir -p /root/.config/openbox
# Patched from the shipped config: openbox doesn't merge a partial rc.xml with its
# defaults, so a partial file silently drops every mouse binding. The titlebar keeps
# only its label, since minimize/close can't be undone from inside the VNC view.
cp /etc/xdg/openbox/rc.xml /root/.config/openbox/rc.xml
sed -i 's|<number>4</number>|<number>1</number>|' /root/.config/openbox/rc.xml
sed -i 's|<titleLayout>.*</titleLayout>|<titleLayout>L</titleLayout>|' /root/.config/openbox/rc.xml

# Drops the keybindings that hide or close a window (W-d show-desktop, A-F4).
# Deliberate closes via the window menu stay; restart_simulator recovers from them.
python3 - /root/.config/openbox/rc.xml <<'PY'
import re
import sys

path = sys.argv[1]
config = open(path).read()
config = re.sub(
    r'\s*<keybind key="[^"]*">\s*<action name="(?:ToggleShowDesktop|Iconify|Close)"\s*/>\s*</keybind>',
    '',
    config,
)
open(path, 'w').write(config)
PY

openbox --sm-disable &
sleep 1

# The Simulator reads this INI only at launch. ShowDeviceControls keeps the crank
# panel; ShowPerfWarning and ShowElist suppress first-run modals. Key names come
# from the binary's strings; it ignores a Volume key, so volume can't be seeded.
SIM_CONFIG_DIR="/root/.config/Playdate Simulator"
mkdir -p "$SIM_CONFIG_DIR"
cat > "$SIM_CONFIG_DIR/Playdate Simulator.ini" <<INI
Zoom=${SIM_ZOOM}
ShowDeviceControls=1
ShowPerfWarning=0
ShowElist=0
INI

# A per-container daemon with a null sink, so this works with no host audio at all.
mkdir -p "$PULSE_RUNTIME_PATH"
pulseaudio -D --exit-idle-time=-1 --disallow-exit --system=false
pactl load-module module-null-sink sink_name=vnc_sink sink_properties=device.description=vnc_sink
pactl set-default-sink vnc_sink

x11vnc -display "$DISPLAY" -forever -shared -nopw -rfbport 5900 -quiet &

# The audio player for both pages. An <audio> element with a retry loop, because a
# browser pointed straight at an endless chunked stream often won't play it. On the
# VNC page it follows pd-volume.json, the slider value read off the framebuffer below.
cat > /usr/share/novnc/pd-audio.js <<'JS'
(function () {
  var url = 'http://' + location.hostname + ':8000/stream.mp3';

  // Hidden on the VNC page, where the Playdate's slider is the control; it appears
  // only when the stream needs attention. audio.html sets PD_AUDIO_VISIBLE.
  var visible = window.PD_AUDIO_VISIBLE === true;

  var bar = document.createElement('div');
  bar.style.cssText = 'position:fixed;right:12px;bottom:12px;z-index:99999;' +
    'display:flex;gap:8px;align-items:center;padding:6px 10px;' +
    'border-radius:6px;background:rgba(0,0,0,0.7);color:#fff;' +
    'font:12px sans-serif';
  if (!visible) bar.style.display = 'none';

  var el = document.createElement('audio');
  // noVNC's page ships an <audio id="noVNC_bell">, which a bare "audio" query finds first.
  el.id = 'pd-audio';
  el.controls = true;
  el.src = url;
  // Assigning src alone opens the stream, and each open connection runs an encoder.
  el.preload = 'none';
  el.style.height = '32px';

  var status = document.createElement('span');

  bar.appendChild(el);
  bar.appendChild(status);
  document.body.appendChild(bar);

  var attempts = 0;
  // Whether playback has been asked for, so a reconnect doesn't start audio by itself.
  var wanted = false;
  var reconnectTimer = null;

  function reconnect() {
    // One pending attempt: the element fires several errors per failure, and each
    // connection spawns an encoder.
    if (reconnectTimer) return;

    if (++attempts > 40) {
      status.textContent = 'gave up, reload to retry';
      bar.style.display = 'flex';
      return;
    }

    var delay = Math.min(400 * Math.pow(2, attempts - 1), 5000);
    status.textContent = 'reconnecting';
    bar.style.display = 'flex';
    reconnectTimer = setTimeout(function () {
      reconnectTimer = null;
      el.load();
      if (wanted) resume();
    }, delay);
  }

  function resume() {
    if (!el.paused) return;
    // Pausing keeps the buffer, so resuming without load() replays stale audio.
    el.load();
    el.play().then(function () {
      status.textContent = '';
      attempts = 0;
      if (!visible) bar.style.display = 'none';
    }).catch(function () {
      // An autoplay block, not a stream failure; the next gesture resolves it.
    });
  }

  // A browser that falls behind a live stream (e.g. a backgrounded tab) never
  // catches up. A chunked stream can't be seeked, so reconnect to the live edge.
  var lastResync = 0;
  function resyncIfBehind() {
    if (el.paused || el.buffered.length === 0) return;
    var live = el.buffered.end(el.buffered.length - 1);
    if (live - el.currentTime < 3) return;
    // Rate limited, so a browser that can't keep up doesn't reconnect in a loop.
    if (Date.now() - lastResync < 30000) return;
    lastResync = Date.now();
    el.load();
    resume();
  }
  setInterval(resyncIfBehind, 5000);

  el.addEventListener('error', reconnect);
  el.addEventListener('ended', reconnect);
  el.addEventListener('play', function () { wanted = true; });

  // The Playdate's slider, published once a second, decides when to play.
  var lastVolume = -1;

  function follow(volume) {
    // -1 is a failed scan; acting on it would look identical to silence.
    if (volume < 0) return;
    if (volume === lastVolume) return;
    lastVolume = volume;

    el.volume = Math.max(0, Math.min(1, volume));

    if (volume < 0.02) {
      // Paused rather than muted, so the connection and its encoder are released.
      if (!el.paused) el.pause();
      status.textContent = '';
      if (!visible) bar.style.display = 'none';
      return;
    }

    wanted = true;
    resume();
  }

  function pollVolume() {
    fetch('pd-volume.json', { cache: 'no-store' })
      .then(function (r) { return r.json(); })
      .then(function (j) { follow(typeof j.volume === 'number' ? j.volume : -1); })
      .catch(function () {});
  }
  pollVolume();
  setInterval(pollVolume, 1000);

  // Browsers won't start audio before a user gesture. This only retries once one
  // happens; a click on the game is not a request for sound.
  function unlock() {
    if (lastVolume >= 0.02) resume();
  }
  document.addEventListener('pointerdown', unlock, true);
  document.addEventListener('keydown', unlock, true);
})();
JS

# The short URL and the audio-only page. noVNC defaults to no scaling;
# resize=scale and autoconnect are its own URL parameters.
cat > /usr/share/novnc/index.html <<'HTML'
<!doctype html>
<title>Playdate Simulator</title>
<meta http-equiv="refresh" content="0; url=vnc.html?autoconnect=true&resize=scale">
<p><a href="vnc.html?autoconnect=true&resize=scale">Open the Simulator</a></p>
HTML

cat > /usr/share/novnc/audio.html <<'HTML'
<!doctype html>
<title>Playdate Simulator audio</title>
<body style="font-family: sans-serif; padding: 2rem">
<h1>Simulator audio</h1>
<p>Audio only, with a visible player. For video and audio in one tab use
<a href="vnc.html">vnc.html</a> instead, where the Playdate's own volume
slider is the control and the player stays out of the way.</p>
<p>Several listeners are fine, each gets its own encoder.</p>
<script>window.PD_AUDIO_VISIBLE = true;</script>
<script src="pd-audio.js"></script>
</body>
HTML

cat > /usr/share/novnc/pd-view.js <<'JS'
(function () {
  var params = new URLSearchParams(location.search);
  if (params.has('resize')) return;
  params.set('resize', 'scale');
  if (!params.has('autoconnect')) params.set('autoconnect', 'true');
  location.replace(location.pathname + '?' + params.toString());
})();
JS

# Injected rather than shipping a patched vnc.html, so noVNC updates keep working.
if grep -q '</body>' /usr/share/novnc/vnc.html; then
  awk '
    /<\/body>/ && !injected {
      print "<script src=\"pd-view.js\"></script>"
      print "<script src=\"pd-audio.js\"></script>"
      injected = 1
    }
    { print }
  ' /usr/share/novnc/vnc.html > /tmp/vnc.html && mv /tmp/vnc.html /usr/share/novnc/vnc.html
else
  echo "WARNING: no </body> in noVNC's vnc.html, audio and scaling not injected" >&2
fi

websockify --web=/usr/share/novnc 6080 localhost:5900 &

# The audio stream: socat forks a fresh encoder per listener. ffmpeg opens its
# pulse input before its output, so an encoder waiting for a client queues a
# growing backlog of audio; starting it on connect means there is none.
cat > /usr/local/bin/pd-audio-stream <<'STREAM'
#!/usr/bin/env bash
# Serves one listener: HTTP headers, then MP3 for as long as they stay connected.
# Run per connection by socat, so the capture below always starts now.
printf 'HTTP/1.0 200 OK\r\n'
printf 'Content-Type: audio/mpeg\r\n'
printf 'Cache-Control: no-cache, no-store\r\n'
printf 'Connection: close\r\n'
printf '\r\n'

exec ffmpeg -loglevel quiet -f pulse -i vnc_sink.monitor \
  -c:a libmp3lame -b:a 128k -flush_packets 1 -f mp3 pipe:1
STREAM
chmod +x /usr/local/bin/pd-audio-stream

socat TCP-LISTEN:8000,reuseaddr,fork,bind=0.0.0.0 \
  SYSTEM:/usr/local/bin/pd-audio-stream &

# Pins the Simulator top-left, then once a second publishes its volume slider read
# off the framebuffer (the Simulator's volume can't be read from outside). A failed
# read publishes -1, which the page ignores rather than treating as silence.
( set +eu
  seen=""
  while true; do
    # Pin before reading, so the published layout matches where the window is.
    win=$(xdotool search --name '^Playdate Simulator$' 2>/dev/null | tail -1)
    if [ -n "$win" ] && [ "$win" != "$seen" ]; then
      xdotool windowmove "$win" 0 0 2>/dev/null
      seen="$win"
      sleep 0.5
    fi

    reading=$(shared_read_slider 2>/dev/null)

    # Nine fields, or it isn't a reading: a short line would write broken JSON.
    if [ "$(echo "$reading" | wc -w)" -ne 9 ]; then
      echo '{"volume":-1}' > /usr/share/novnc/pd-volume.json
      echo '{}' > /usr/share/novnc/pd-layout.json
      seen=""
      sleep 1
      continue
    fi

    set -- $reading
    win_x="$1"; win_y="$2"; win_w="$3"; win_h="$4"; trough_x="$5"
    t_top="$6"; t_bottom="$7"; t_mute="$8"; volume="$9"

    printf '{"volume":%s}\n' "$volume" > /usr/share/novnc/pd-volume.json
    cat > /usr/share/novnc/pd-layout.json <<JSON
{
  "window": { "x": $win_x, "y": $win_y, "w": $win_w, "h": $win_h },
  "troughX": $trough_x,
  "troughTop": $(( win_y + t_top )),
  "troughBottom": $(( win_y + t_bottom )),
  "muteY": $(( win_y + t_mute )),
  "volume": $volume
}
JSON

    sleep 1
  done ) &

echo "video + audio: http://localhost:6080/"
echo "audio only:    http://localhost:6080/audio.html"
echo "display:       ${VNC_GEOMETRY}, Simulator zoom ${SIM_ZOOM}x"

# Keeps the container alive as long as the background services, with or without a TTY.
wait
