#!/usr/bin/env bash
set -euo pipefail

# shellcheck source=scripts/byos-lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/byos-lib.sh"

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
# - The encoder below serves one listener per invocation and exits when that
#   listener leaves, so every reconnect has a short window where the port
#   refuses connections. Without a retry loop you're left hard-refreshing
#   until the timing happens to line up.
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

  function reconnect() {
    if (++attempts > 120) {
      status.textContent = 'gave up, reload to retry';
      bar.style.display = 'flex';
      return;
    }
    status.textContent = 'reconnecting';
    bar.style.display = 'flex';
    setTimeout(function () {
      el.load();
      if (wanted) resume();
    }, 400);
  }

  function resume() {
    if (!el.paused) return;
    el.play().then(function () {
      status.textContent = '';
      attempts = 0;
      if (!visible) bar.style.display = 'none';
    }).catch(function () {});
  }

  el.addEventListener('error', reconnect);
  el.addEventListener('ended', reconnect);
  // Covers the visible player on audio.html too: pressing its play button is
  // just as much a request for audio as clicking the Playdate's slider.
  el.addEventListener('play', function () { wanted = true; });

  // Where the Simulator's window and its volume slider currently are, in
  // framebuffer pixels, republished by the container whenever the Simulator
  // is relaunched.
  var layout = null;
  function pollLayout() {
    return fetch('pd-layout.json', { cache: 'no-store' })
      .then(function (r) { return r.json(); })
      .then(function (j) { layout = j && j.troughX ? j : null; })
      .catch(function () {});
  }
  pollLayout();
  setInterval(pollLayout, 1000);

  // noVNC draws the framebuffer into a canvas and scales it with CSS, so a
  // click's page coordinates have to be converted back to framebuffer pixels
  // before they can be compared against the layout above.
  function toFramebuffer(event) {
    var canvas = document.querySelector('#noVNC_canvas, canvas');
    if (!canvas || !canvas.width) return null;
    var rect = canvas.getBoundingClientRect();
    if (!rect.width || !rect.height) return null;
    if (event.clientX < rect.left || event.clientX > rect.right) return null;
    if (event.clientY < rect.top || event.clientY > rect.bottom) return null;
    return {
      x: (event.clientX - rect.left) * (canvas.width / rect.width),
      y: (event.clientY - rect.top) * (canvas.height / rect.height)
    };
  }

  // Deliberately not "any click anywhere". Starting playback on every click
  // means audio fires when you are aiming a crank or pressing a d-pad, which
  // is not something you asked the page to do.
  //
  // Bound on mousedown in the capture phase, not on click. noVNC calls
  // preventDefault on the mouse events it forwards to the remote display, so a
  // listener waiting for a bubbling click is at its mercy. Capture runs before
  // the canvas sees the event at all.
  function applyAt(pt) {
    if (!layout) return;
    if (Math.abs(pt.x - layout.troughX) > 12) return;

    if (Math.abs(pt.y - layout.muteY) <= 12) {
      el.muted = !el.muted;
      status.textContent = el.muted ? 'muted' : '';
      if (!el.muted) {
        wanted = true;
        resume();
      }
      return;
    }

    if (pt.y < layout.troughTop - 8 || pt.y > layout.troughBottom + 8) return;
    var span = layout.troughBottom - layout.troughTop;
    var fraction = (layout.troughBottom - pt.y) / span;
    el.volume = Math.max(0, Math.min(1, fraction));
    el.muted = false;
    wanted = true;
    resume();
  }

  document.addEventListener('mousedown', function (event) {
    var pt = toFramebuffer(event);
    if (!pt) return;
    if (layout) {
      applyAt(pt);
      return;
    }
    // A click can land before the first layout fetch has come back, and
    // dropping it means the slider does nothing the first time you touch it.
    // Fetch now and apply this same click once the answer arrives.
    pollLayout().then(function () { applyAt(pt); });
  }, true);
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
<p>Only one listener is served at a time, so close any other tab pointed at
port 8000.</p>
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

# ffmpeg's -listen HTTP server serves exactly one client per invocation and
# exits when that client disconnects. Wrapped in a restart loop so it
# survives reconnects, like re-opening the stream in a browser.
#
# -content_type is required. Without it ffmpeg's HTTP server sends
# application/octet-stream, and browsers download the stream as a file
# instead of playing it inline.
( set +e; while true; do
  ffmpeg -loglevel error -f pulse -i vnc_sink.monitor -c:a libmp3lame -f mp3 \
    -content_type audio/mpeg -listen 1 http://0.0.0.0:8000/stream.mp3
  sleep 0.2
done ) &

# Pins the Simulator to the top-left corner and publishes where its volume
# slider is, once per Simulator window that appears.
#
# The corner placement is the point of the workspace: centred, it sits in the
# middle of a field of dead space, and there is nowhere to put the console
# window without overlapping it. Flush in the corner, everything left over is
# somewhere to drag debugging windows to.
#
# The layout file is how the browser page knows which pixels are the volume
# slider. It cannot work that out itself: it only sees a framebuffer, and the
# window's position depends on where the Simulator opened.
#
# The slider is found by reading the framebuffer rather than by hardcoding
# offsets, because those offsets move with the zoom level. One pixel-wide
# column down the device frame, 29px in from the window's right edge, crosses
# the LOCK button, the MENU button, the volume trough, its knob, and the mute
# icon. On the yellow frame each of those reads as either dark or white, so
# the runs of non-yellow pixels are the widgets:
#
#   rows 44-48    LOCK
#   rows 68-80    MENU
#   rows 124-193  the trough        <- longest run, and the one we want
#   rows 195-207  the knob, sitting at the bottom when volume is zero
#   rows 219-229  the mute icon
#
# Those row numbers are 1x zoom, quoted to show the shape of what's being
# matched, not relied on: the code takes the longest run below the buttons,
# whatever its position, so 2x and 3x calibrate themselves.
( set +e
  seen=""
  while true; do
    win=$(xdotool search --name '^Playdate Simulator$' 2>/dev/null | tail -1)
    if [ -z "$win" ]; then
      seen=""
      echo '{}' > /usr/share/novnc/pd-layout.json
    elif [ "$win" != "$seen" ]; then
      sleep 1
      xdotool windowmove "$win" 0 0 2>/dev/null
      sleep 0.5
      unset X Y WIDTH HEIGHT
      eval "$(xdotool getwindowgeometry --shell "$win" 2>/dev/null)"
      if [ -n "${WIDTH:-}" ] && [ -n "${HEIGHT:-}" ]; then
        trough_x=$(( X + WIDTH - 29 ))
        ffmpeg -loglevel error -f x11grab -video_size "$VNC_GEOMETRY" -i "$DISPLAY" \
          -frames:v 1 -y /tmp/pd-frame.png 2>/dev/null
        ffmpeg -loglevel error -i /tmp/pd-frame.png \
          -vf "crop=1:400:${trough_x}:${Y},format=gray" \
          -f rawvideo -y /tmp/pd-column.raw 2>/dev/null

        # Emits "top bottom mute" as offsets from the top of the window, or
        # nothing at all when the column doesn't look like a device frame. The
        # parser lives in find-volume-slider.awk so it can be unit tested
        # against synthetic columns instead of only against a live Simulator.
        read -r t_top t_bottom t_mute <<EOF
$(byos_column_from_raw /tmp/pd-column.raw | byos_find_slider)
EOF

        if [ -n "${t_top:-}" ] && [ -n "${t_bottom:-}" ]; then
          cat > /usr/share/novnc/pd-layout.json <<JSON
{
  "window": { "x": $X, "y": $Y, "w": $WIDTH, "h": $HEIGHT },
  "troughX": $trough_x,
  "troughTop": $(( Y + t_top )),
  "troughBottom": $(( Y + t_bottom )),
  "muteY": $(( Y + ${t_mute:-0} ))
}
JSON
          echo "layout: trough $(( Y + t_top ))-$(( Y + t_bottom )) mute $(( Y + ${t_mute:-0} )) at x=$trough_x" >&2
        else
          echo '{}' > /usr/share/novnc/pd-layout.json
          echo "WARNING: could not find the volume slider in the framebuffer" >&2
        fi
        seen="$win"
      fi
    fi
    sleep 2
  done ) &

echo "video + audio: http://localhost:6080/"
echo "audio only:    http://localhost:6080/audio.html"
echo "display:       ${VNC_GEOMETRY}, Simulator zoom ${SIM_ZOOM}x"

# Keeps the container alive exactly as long as these background services
# are, without needing an attached TTY - works the same whether this runs
# attached in the foreground (make up-vnc) or detached in the background
# (make up-byos).
wait
