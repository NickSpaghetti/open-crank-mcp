#!/usr/bin/env bash
set -euo pipefail

# shellcheck source=scripts/shared-lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/shared-lib.sh"

export DISPLAY=:99
export PULSE_RUNTIME_PATH=/tmp/pulse-vnc
export PULSE_SERVER="unix:${PULSE_RUNTIME_PATH}/native"
export SDL_AUDIODRIVER=pulseaudio

SIM_ZOOM="${SIM_ZOOM:-1}"

# This is a workspace, not a frame around the Simulator. The Simulator's own
# window is 400*zoom+82 by 240*zoom+466 (that 466 is fixed chrome: menu bar,
# d-pad row, toolbar, and the accelerometer/crank panel), and it gets pinned
# to the top-left corner further down, so everything left over is room to
# drag the Simulator's console and other debugging windows into.
VNC_GEOMETRY="${VNC_GEOMETRY:-1280x800}"

Xvfb "$DISPLAY" -screen 0 "${VNC_GEOMETRY}x24" &
sleep 1

# A window manager, so windows can be moved, resized, raised and focused.
# Without one, X maps every window wherever the application asks and nothing
# can be dragged, which also means no keyboard focus: key events go to
# whatever sits under the pointer, and widgets that only respond to focused
# input never see anything at all.
#
# Two of openbox's defaults are actively harmful here, and both of them make
# the Simulator appear to vanish:
#
# - Four desktops. The Simulator opens on one of them, and a stray scroll over
#   the desktop background switches to another, leaving a black screen.
# - Keybindings, including toggle-show-desktop, which unmaps every window at
#   once. They also collide with the Simulator's own shortcuts, which use
#   Ctrl-1/2/3 for zoom and Shift-Ctrl-C for the crank panel.
#
# So: one desktop, and no keybindings at all. Mouse bindings are left at their
# defaults, which is what gives titlebar dragging and resizing.
mkdir -p /root/.config/openbox
# Patched from openbox's shipped config rather than written from scratch.
# openbox does not merge a partial config with its defaults: a file containing
# only the settings you care about silently drops the other 59 mouse bindings,
# which is every drag, resize and menu the window manager has. So start from
# the real thing and change three lines.
#
# The titlebar is left with nothing but its label: no minimize, no maximize,
# no close, and no window icon. All of those are one stray click away from
# losing the Simulator, and none of them can be undone from inside the VNC
# view, because only an MCP client can start a Simulator. Closing it kills the
# process; minimizing it hides it with no taskbar or panel to restore from.
#
# The label still drags, which is the only thing that button row was worth
# here.
cp /etc/xdg/openbox/rc.xml /root/.config/openbox/rc.xml
sed -i 's|<number>4</number>|<number>1</number>|' /root/.config/openbox/rc.xml
sed -i 's|<titleLayout>.*</titleLayout>|<titleLayout>L</titleLayout>|' /root/.config/openbox/rc.xml

# Same reasoning for the keybindings that hide or kill a window: W-d toggles
# show-desktop, which unmaps everything at once, and A-F4 closes. Openbox's
# other defaults stay, so Alt-Tab still works, and so do the mouse bindings
# that give dragging, resizing and the window list on a middle click.
#
# Right-clicking the titlebar still reaches openbox's own window menu, which
# has a Close entry, and the Simulator's File menu can still quit. Those are
# deliberate acts rather than mis-clicks, and both are recoverable: ask the
# agent for restart_simulator, which relaunches the last .pdx even after the
# process is gone.
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

# Seeded before the Simulator ever starts, since it only reads this file at
# launch. Every one of these is otherwise a manual click in the VNC view on
# every fresh container, and an agent has no tool for any of them:
#
#   Zoom                 1x, the Simulator's own default size. SIM_ZOOM
#                        changes it, as do Ctrl-1/Ctrl-2/Ctrl-3 live
#   ShowDeviceControls=1 keeps the accelerometer and crank panel, which is
#                        the only way to work the crank by hand
#   ShowPerfWarning=0    the "test on real hardware" modal
#   ShowElist=0          the "join the developer email list" modal
#
# Key names came from the Simulator binary's own strings. Volume looks like
# it belongs here and doesn't work: the Simulator ignores a Volume key
# entirely, which is why it gets clicked instead, further down.
#
# Written fresh rather than merged: a container starts with no INI at all,
# and the Simulator rewrites the file with its own additions (recent files,
# analytics UUID) as it goes.
SIM_CONFIG_DIR="/root/.config/Playdate Simulator"
mkdir -p "$SIM_CONFIG_DIR"
cat > "$SIM_CONFIG_DIR/Playdate Simulator.ini" <<INI
Zoom=${SIM_ZOOM}
ShowDeviceControls=1
ShowPerfWarning=0
ShowElist=0
INI

# A per-container daemon with its own null sink, not a bridge to any host
# audio server. This profile has to work with no host audio infrastructure
# at all. That's the whole point of the VNC fallback.
mkdir -p "$PULSE_RUNTIME_PATH"
pulseaudio -D --exit-idle-time=-1 --disallow-exit --system=false
pactl load-module module-null-sink sink_name=vnc_sink sink_properties=device.description=vnc_sink
pactl set-default-sink vnc_sink

x11vnc -display "$DISPLAY" -forever -shared -nopw -rfbport 5900 -quiet &

# The audio player, built once here and used by both pages websockify
# serves. Two reasons it isn't just a bare link to the MP3 stream:
#
# - A browser pointed straight at an endless chunked stream shows its own
#   media viewer, reports the stream as 0:00 long, and often won't start it.
#   An <audio> element handles the same stream fine.
# - The stream can drop: the container restarts, or the encoder serving this
#   listener goes away. Without a retry loop that means hard-refreshing.
#
# On the VNC page the Playdate's own volume slider drives this player. A click
# on that slider is a real click, delivered to the Simulator, so it moves the
# Simulator's volume as it always did. The page watches for clicks in the same
# region, works out the fraction of the trough that was clicked, and applies
# it here too. So one gesture sets both, and the slider you see on the
# Playdate is the level the player is at.
#
# It only goes one way. Nothing can read the Simulator's slider back (the SDK
# exposes getVolume() with no setter, and its INI has no key for it), so
# moving this player's own control does not move the Playdate's.
#
# The host is taken from location.hostname rather than hardcoded to
# localhost, so this still works when the container is reached over the
# network instead of from the Docker host itself.
cat > /usr/share/novnc/pd-audio.js <<'JS'
(function () {
  var url = 'http://' + location.hostname + ':8000/stream.mp3';

  // On the VNC page the player is hidden: the Playdate's own slider is the
  // control, so an on-screen player is just something covering the workspace.
  // The element still has to exist in the document to play at all. It appears
  // only if the stream needs attention, because a silent failure with no
  // visible player is indistinguishable from silence.
  //
  // audio.html sets PD_AUDIO_VISIBLE, because there the player is the page.
  var visible = window.PD_AUDIO_VISIBLE === true;

  var bar = document.createElement('div');
  bar.style.cssText = 'position:fixed;right:12px;bottom:12px;z-index:99999;' +
    'display:flex;gap:8px;align-items:center;padding:6px 10px;' +
    'border-radius:6px;background:rgba(0,0,0,0.7);color:#fff;' +
    'font:12px sans-serif';
  if (!visible) bar.style.display = 'none';

  var el = document.createElement('audio');
  // Named, because noVNC's own page ships an <audio id="noVNC_bell"> and an
  // unqualified query for "audio" finds that one first.
  el.id = 'pd-audio';
  el.controls = true;
  el.src = url;
  // Nothing is fetched until playback is actually wanted. Assigning src alone
  // is enough for the browser to open the stream, and the encoder serves one
  // listener at a time, so a VNC tab nobody is listening to would sit on the
  // stream and lock out the tab that is.
  el.preload = 'none';
  el.style.height = '32px';

  var status = document.createElement('span');

  bar.appendChild(el);
  bar.appendChild(status);
  document.body.appendChild(bar);

  var attempts = 0;
  // Whether playback has been asked for. Without this, a failed connection
  // reconnects and starts playing on its own, which is the same "audio out of
  // nowhere" behaviour that clicking anywhere used to cause.
  var wanted = false;
  var reconnectTimer = null;

  function reconnect() {
    // One pending attempt at a time. The element can fire several errors for a
    // single failure, and a timer per error turns one dropped stream into a
    // burst of connections, each of which spawns an encoder in the container.
    if (reconnectTimer) return;

    if (++attempts > 40) {
      status.textContent = 'gave up, reload to retry';
      bar.style.display = 'flex';
      return;
    }

    // Backing off rather than hammering every 400ms. A container that is
    // restarting takes seconds, not milliseconds, and the old fixed interval
    // meant ~150 attempts across that window.
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
    // load() first, every time. Pausing does not discard what the element has
    // already buffered, so resuming without this replays audio from wherever it
    // stopped - explosions from several minutes ago, arriving while the game sits
    // on its game-over screen. A live stream has no business resuming from a
    // buffer, so the connection is dropped and remade.
    el.load();
    el.play().then(function () {
      status.textContent = '';
      attempts = 0;
      if (!visible) bar.style.display = 'none';
    }).catch(function () {
      // A rejected play is not a stream failure: an autoplay block resolves
      // itself on the next gesture, so don't burn reconnect attempts on it.
    });
  }

  // A browser that falls behind on a live stream keeps the gap rather than
  // catching up, so latency grows and never recovers. Backgrounded tabs are the
  // usual cause: timers get throttled, the element keeps buffering, and coming
  // back to the tab means hearing the past.
  //
  // Reconnecting rather than seeking. Seeking a chunked live stream is not
  // meaningful - there is no seekable range behind it - and setting currentTime
  // on one produces exactly the stutter this is meant to remove. A fresh
  // connection gets a fresh encoder, which starts at the live edge.
  var lastResync = 0;
  function resyncIfBehind() {
    if (el.paused || el.buffered.length === 0) return;
    var live = el.buffered.end(el.buffered.length - 1);
    if (live - el.currentTime < 3) return;
    // Rate limited, so a browser that simply cannot keep up doesn't end up
    // reconnecting on a loop and stuttering worse than the drift it is fixing.
    if (Date.now() - lastResync < 30000) return;
    lastResync = Date.now();
    el.load();
    resume();
  }
  setInterval(resyncIfBehind, 5000);

  el.addEventListener('error', reconnect);
  el.addEventListener('ended', reconnect);
  // Covers the visible player on audio.html too: pressing its play button is
  // just as much a request for audio as clicking the Playdate's slider.
  el.addEventListener('play', function () { wanted = true; });

  // The Playdate's own volume slider decides everything. The container reads
  // the slider off the framebuffer once a second and publishes it here, because
  // that slider is the only place the Simulator's volume exists. Turn it up in
  // the VNC view and sound starts at that level; turn it down and it stops.
  //
  // Nothing in this page decides when to play. There is no click handling, no
  // hit-testing against the slider's pixels, and no "click here for audio"
  // affordance to miss, which is what the previous version got wrong.
  var lastVolume = -1;

  function follow(volume) {
    // -1 means the scan couldn't read the slider. Leave the audio exactly as it
    // is: a failed read looks identical to silence if you act on it.
    if (volume < 0) return;
    if (volume === lastVolume) return;
    lastVolume = volume;

    el.volume = Math.max(0, Math.min(1, volume));

    if (volume < 0.02) {
      // Paused rather than muted, so the connection is released and its encoder
      // exits. Muting would leave an encoder capturing audio nobody hears.
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

  // Browsers refuse to start audio until the page has seen a user gesture, and
  // there is no way around that. This only records that one happened - it never
  // decides to play, so a click on the game is not a request for sound. Clicking
  // to connect to the display is enough to satisfy it, which is why the slider
  // appears to just work.
  function unlock() {
    if (lastVolume >= 0.02) resume();
  }
  document.addEventListener('pointerdown', unlock, true);
  document.addEventListener('keydown', unlock, true);
})();
JS

# The short URL, and the audio-only page. noVNC defaults to no scaling, so a
# display larger than the tab gets scrollbars instead of being scaled to fit.
# resize=scale fixes that and autoconnect skips the connect screen, both being
# noVNC's own URL parameters.
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

# Injected rather than shipped as patched copies of vnc.html, so noVNC's own
# page stays vendor-clean and this keeps working across noVNC updates. If the
# anchor ever disappears, say so loudly instead of silently serving a page
# with no audio and no scaling.
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

# The audio stream: socat accepts the connection, and each listener gets its own
# freshly started encoder.
#
# The obvious approach - one long-lived `ffmpeg -listen 1` acting as the HTTP
# server - is broken in a way that takes measuring to see. ffmpeg opens its input
# before its output, so the pulse capture starts immediately and then ffmpeg
# blocks waiting for someone to connect. Everything captured meanwhile queues up,
# and the listener that finally arrives is handed the backlog: measured with a
# beep played at a known moment, a fresh encoder delivered it ~2s later, one left
# idle for a few minutes delivered it ~22s later and stayed that far behind for
# the rest of the session. Which is exactly what late audio that keeps playing
# after you pause sounds like.
#
# Starting the encoder only once a client is connected removes the problem rather
# than managing it: there is no idle period to accumulate. It also drops the
# one-listener-at-a-time limit that came with using ffmpeg as the server, since
# socat forks per connection and a pulse monitor can be read by several clients
# at once.
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

# Pins the Simulator to the top-left corner, then publishes what its volume
# slider currently reads, once a second.
#
# The corner placement is the point of the workspace: centred, the Simulator sits
# in the middle of dead space with nowhere to put the console window. Flush in the
# corner, everything left over is somewhere to drag debugging windows to.
#
# The volume reading is the interesting half. The Simulator's slider is the only
# place its volume exists - the SDK exposes getVolume() with no setter and no way
# to read it from outside - so it gets read off the framebuffer instead. One
# pixel-wide column down the device frame crosses the LOCK button, the MENU
# button, the volume track, its knob and the mute icon, each reading as dark or
# light against the yellow frame. The knob's position along the track is the
# volume.
#
# The browser page follows this file, which is what makes the slider behave the
# way you would expect: turn it up in the VNC view and sound starts, turn it down
# and it stops. Nothing in the page decides when to play.
#
# A failed read publishes -1, which the page treats as "don't know" and leaves the
# audio alone. Publishing 0 instead would silence working audio every time a scan
# happened to fail, which is indistinguishable from a broken pipeline.
( set +eu
  seen=""
  while true; do
    # Corner-pin before reading, not after. Publishing first and moving second
    # leaves a window where the file describes the position the window just left,
    # and anything that clicks those coordinates misses.
    win=$(xdotool search --name '^Playdate Simulator$' 2>/dev/null | tail -1)
    if [ -n "$win" ] && [ "$win" != "$seen" ]; then
      xdotool windowmove "$win" 0 0 2>/dev/null
      seen="$win"
      sleep 0.5
    fi

    reading=$(shared_read_slider 2>/dev/null)

    # Nine fields, or it isn't a reading. Guarding on the count rather than on
    # emptiness because a short line would leave the later fields unset, and this
    # loop has already died silently once that way.
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

# Keeps the container alive exactly as long as these background services
# are, without needing an attached TTY - works the same whether this runs
# attached in the foreground (make up-vnc) or detached in the background
# (make up-shared).
wait
