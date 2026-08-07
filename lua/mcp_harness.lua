local mcp = {}
_G.mcp = mcp

playdate.file.mkdir("mcp")

local override = {
    button = {},
    -- dockedActive is separate from active: the crank override always takes over
    -- angle and delta, but only takes over the dock state when a command actually
    -- asked it to. Without that split there is no way to move the angle and leave
    -- the dock reading whatever the game would really see.
    crank = {active = false, angle = 0, delta = 0, docked = false, dockedActive = false, expiresAt = 0},
}

-- Sentinel expiry meaning "never". Negative so it cannot collide with a real
-- deadline, which is always nowMs plus a duration. Matches MCP_NO_EXPIRY in
-- c-harness/mcp_harness.h.
local NO_EXPIRY = -1

local stateFn = nil

local realButtonIsPressed = playdate.buttonIsPressed
local realButtonJustPressed = playdate.buttonJustPressed
local realButtonJustReleased = playdate.buttonJustReleased
local realGetButtonState = playdate.getButtonState
local realGetCrankPosition = playdate.getCrankPosition
local realGetCrankChange = playdate.getCrankChange
local realIsCrankDocked = playdate.isCrankDocked
local realPrint = print

-- Which canonical harness this copy came from, reported in every response so the
-- Go side can tell whether a game's vendored copy has drifted.
--
-- Not maintained by hand: the `setup` tool substitutes a content fingerprint of
-- the canonical source as it writes this file into a game, so every harness
-- change produces a new value with nothing to remember. The literal below is what
-- ships in this repo; a copy still carrying it was not installed by `setup`, which
-- the server reports as its own case. See internal/harness/version.go.
local HARNESS_VERSION = "@HARNESS_VERSION@"

-- The IPC files. RESPONSE_TMP_PATH exists so a response can be published by
-- rename rather than written in place; see the end of mcp.update().
local COMMAND_PATH = "mcp/command.json"
local RESPONSE_PATH = "mcp/response.json"
local RESPONSE_TMP_PATH = "mcp/response.tmp.json"

-- Captures the game's own print() output and unhandled-error tracebacks into
-- a file the Go side can read, since PlaydateSimulator renders them into its
-- own GUI console and nothing here can reach that.
--
-- This comment has been wrong twice, so here is what was actually measured.
--
-- It first claimed Lua print() "never touches the process's real stdout/stderr on
-- Linux (SDK 3.1.1)". Wrong: print() and unhandled tracebacks both reach real
-- stdout there. But the correction was also incomplete, and the missing piece is
-- what justifies this channel: **stdout is block-buffered**. A game printing one
-- line produced 3 captured lines (none of them its own); the same game printing
-- 300 produced 299. Below roughly 4KB nothing appears at all, and stopping the
-- Simulator is a hard kill, which never flushes what is left.
--
-- A traceback is exactly the low-volume, just-happened case, so stdout is least
-- trustworthy precisely when it matters. This channel is immune because it
-- open/write/closes per entry: what it has written is readable immediately.
-- See docs/GOTCHAS.md for the measurements.
--
-- One JSON object per line, appended, rather than one JSON array rewritten.
-- The rewrite cost 0.855ms per print() at the 200-entry steady state (a
-- 15KB re-encode plus a full file write, every call, measured in the
-- Simulator); appending one line costs 0.0117ms, 73x less. That is 2.6% of a
-- 33ms frame per print() versus 0.04%, and it scaled with the number of
-- entries retained rather than staying flat - so a game that logs while you
-- are hunting a bug paid most for logging.
--
-- Rotation replaces the old in-memory cap, in two generations. At the size limit
-- the current file is *renamed* to the previous-generation path and a fresh one
-- starts, so what is on disk is always at least one full generation of history and
-- never nothing.
--
-- The first attempt truncated instead, and the comment here claimed it kept "the
-- older half" - it kept none. That is the worst possible moment to have no
-- history, because the reason this channel exists is reading a traceback, and a
-- traceback is always appended *after* whatever caused it. A crash shortly after a
-- rotation would have shown the traceback with none of its run-up. Measured, that
-- was not a rare corner: at roughly 65 bytes an entry a 256KB generation is about
-- 4,000 entries, so a game printing once per frame rotated every ~2 minutes and
-- reset its history to zero each time.
--
-- Renaming rather than copying the tail back is what keeps this O(1). Reading
-- 256KB to keep half of it is the cost this whole change removed, and it would
-- land inside a single frame.
local GAME_LOGS_PATH = "mcp/game_logs.jsonl"
local GAME_LOGS_PREV_PATH = "mcp/game_logs.1.jsonl"
-- The file this channel replaced. Only referenced to delete it - see
-- resetGameLog.
local LEGACY_GAME_LOGS_PATH = "mcp/game_logs.json"
-- Per generation, so at most twice this is on disk and at least this much history
-- survives a rotation.
local GAME_LOGS_MAX_BYTES = 256 * 1024
local gameLogBytes = 0

-- Moves the current log aside so a fresh one can start, keeping one generation of
-- history. One rename, no data copied.
--
-- The previous generation is deleted first rather than relying on rename to
-- replace it: playdate.file.rename documents only that it returns true on success,
-- not what it does to an existing destination, and this needs to be the same on
-- every platform. Losing the older generation is exactly what rotation is for, so
-- deleting it before the rename is not a risk - and if the process died between
-- the two, the full current file is still there under its own name.
--
-- A failed rename falls back to truncating. That is the old behaviour and it loses
-- history, but the alternative is a file that grows without limit because the cap
-- can never be enforced, and a log that eats the disk is worse than a log that
-- forgets. There is deliberately no attempt to report the failure: the only channel
-- available for saying so is this file.
local function rotateGameLog()
    playdate.file.delete(GAME_LOGS_PREV_PATH)
    if not playdate.file.rename(GAME_LOGS_PATH, GAME_LOGS_PREV_PATH) then
        local truncate = playdate.file.open(GAME_LOGS_PATH, playdate.file.kFileWrite)
        if truncate then
            truncate:close()
        end
    end
    gameLogBytes = 0
end

local function appendGameLog(logType, message)
    local line = json.encode({
        type = logType,
        message = message,
        ms = playdate.getCurrentTimeMilliseconds(),
    }) .. "\n"

    if gameLogBytes + #line > GAME_LOGS_MAX_BYTES then
        rotateGameLog()
    end

    -- Appended and closed on every call, not batched into mcp.update() - so a
    -- log written the frame before a crash still lands on disk even if
    -- mcp.update() itself never runs again afterward.
    local f = playdate.file.open(GAME_LOGS_PATH, playdate.file.kFileAppend)
    if f then
        f:write(line)
        f:close()
        gameLogBytes = gameLogBytes + #line
    end
end

-- A previous run's log is not this run's. Truncated at load rather than
-- appended to, since the old array format was implicitly per-run too (it
-- started from an empty table) and a stale prefix would otherwise read as
-- this session's history.
--
-- Also deletes the file this channel replaced, so a game that has just been
-- re-setup stops carrying two logs where only one is authoritative. The Go side
-- treats the old file's presence as proof that a game's harness copy is stale
-- (see getGameLogs), which only works if a current harness removes it.
local function resetGameLog()
    local f = playdate.file.open(GAME_LOGS_PATH, playdate.file.kFileWrite)
    if f then
        f:close()
    end
    gameLogBytes = 0
    -- Both the rotated generation and the file this channel replaced. A previous
    -- run's rotated log would otherwise be read as part of this run's history.
    playdate.file.delete(GAME_LOGS_PREV_PATH)
    playdate.file.delete(LEGACY_GAME_LOGS_PATH)
end

resetGameLog()

function print(...)
    realPrint(...)
    local n = select("#", ...)
    local parts = {}
    for i = 1, n do
        parts[i] = tostring(select(i, ...))
    end
    appendGameLog("print", table.concat(parts, "\t"))
end

local buttonConstants = {
    a = playdate.kButtonA,
    b = playdate.kButtonB,
    up = playdate.kButtonUp,
    down = playdate.kButtonDown,
    left = playdate.kButtonLeft,
    right = playdate.kButtonRight,
}

local function buttonFromString(s)
    return buttonConstants[s]
end

function playdate.buttonIsPressed(button)
    local o = override.button[button]
    if o and o.active then
        return o.value
    end
    return realButtonIsPressed(button)
end

-- Real edges for an overridden button, synthesized in updateButtonEdges()
-- below - see its comment for why this needs its own tracking rather than
-- just passing through the real buttonJustPressed/buttonJustReleased or
-- letting the SDK's own AButtonDown/leftButtonDown/etc callbacks fire on
-- their own (they can't: those reflect a real hardware edge the
-- Simulator's own runtime decides to dispatch, which an override doesn't
-- cause to happen).
local lastEffectivePressed = {}
local overrideWasActiveLastFrame = {}
local justPressedSynthetic = {}
local justReleasedSynthetic = {}

-- Name prefix used to build each button's *ButtonDown/*ButtonUp callback
-- name (e.g. "A" -> "AButtonDown", "up" -> "upButtonDown") - exact casing
-- confirmed against Inside Playdate.html's "Button callbacks" section.
-- AButtonHeld/BButtonHeld (fired after a continuous 1-second hold) are a
-- separate mechanism, deliberately not synthesized here.
local buttonCallbackPrefix = {
    [playdate.kButtonA] = "A",
    [playdate.kButtonB] = "B",
    [playdate.kButtonUp] = "up",
    [playdate.kButtonDown] = "down",
    [playdate.kButtonLeft] = "left",
    [playdate.kButtonRight] = "right",
}

-- Calls one of the game's own button callbacks, catching anything it throws.
--
-- These are invoked from inside mcp.update(), which wrapUpdate calls *outside* its
-- xpcall - so before this, an error in a game's AButtonDown escaped the harness
-- entirely. Measured: the traceback reached stdout, was absent from game_logs
-- despite get_game_logs advertising exactly that, and killed the polling loop for
-- good; a ping issued afterwards was never answered. The stack said so plainly:
--
--   main.lua:11: in local 'fn'
--   mcp_harness.lua:245: in upvalue 'updateButtonEdges'
--   mcp_harness.lua:486: in field 'update'
--
-- Caught here rather than only at the mcp.update() level so the rest of this frame's
-- harness work still happens: one broken callback should not stop the other five
-- buttons' edges being computed, or the pending command being answered.
local function callGameCallback(fn)
    if not fn then return end
    local ok, err = xpcall(fn, debug.traceback)
    if not ok then
        appendGameLog("error", err)
    end
end

-- Called once per frame from mcp.update(), before that frame's incoming
-- command (if any) is processed - so a press/release command only
-- produces its edge starting the *next* frame's call, a small,
-- predictable latency rather than an order-dependent one. Buttons never
-- touched by an override are left alone entirely: no risk of double
-- firing a real edge the SDK already dispatched on its own.
local function updateButtonEdges()
    for button, prefix in pairs(buttonCallbackPrefix) do
        justPressedSynthetic[button] = false
        justReleasedSynthetic[button] = false

        local o = override.button[button]
        local activeNow = o ~= nil and o.active
        local effective
        if activeNow then
            effective = o.value
        else
            effective = realButtonIsPressed(button)
        end

        if activeNow or overrideWasActiveLastFrame[button] then
            if effective and not lastEffectivePressed[button] then
                justPressedSynthetic[button] = true
                callGameCallback(playdate[prefix .. "ButtonDown"])
            elseif not effective and lastEffectivePressed[button] then
                justReleasedSynthetic[button] = true
                callGameCallback(playdate[prefix .. "ButtonUp"])
            end
        end

        lastEffectivePressed[button] = effective
        overrideWasActiveLastFrame[button] = activeNow
    end
end

function playdate.buttonJustPressed(button)
    local o = override.button[button]
    local activeNow = o ~= nil and o.active
    if activeNow or overrideWasActiveLastFrame[button] then
        return justPressedSynthetic[button] or false
    end
    return realButtonJustPressed(button)
end

function playdate.buttonJustReleased(button)
    local o = override.button[button]
    local activeNow = o ~= nil and o.active
    if activeNow or overrideWasActiveLastFrame[button] then
        return justReleasedSynthetic[button] or false
    end
    return realButtonJustReleased(button)
end

function playdate.getButtonState()
    local current, pressed, released = realGetButtonState()
    for button, _ in pairs(buttonCallbackPrefix) do
        local o = override.button[button]
        local activeNow = o ~= nil and o.active
        if activeNow then
            if o.value then
                current = current | button
            else
                current = current & ~button
            end
        end
        -- Same merge as buttonJustPressed/buttonJustReleased above: an
        -- overridden (or just-was-overridden) button's real pressed/
        -- released bit is meaningless, since no real hardware edge
        -- caused it - use the synthetic edge instead.
        if activeNow or overrideWasActiveLastFrame[button] then
            pressed = pressed & ~button
            released = released & ~button
            if justPressedSynthetic[button] then pressed = pressed | button end
            if justReleasedSynthetic[button] then released = released | button end
        end
    end
    return current, pressed, released
end

function playdate.getCrankPosition()
    if override.crank.active then
        return override.crank.angle
    end
    return realGetCrankPosition()
end

function playdate.getCrankChange()
    if override.crank.active then
        return override.crank.delta, override.crank.delta
    end
    return realGetCrankChange()
end

function playdate.isCrankDocked()
    -- Both flags: an active crank override that was not asked to touch the dock
    -- must not touch it.
    if override.crank.active and override.crank.dockedActive then
        return override.crank.docked
    end
    return realIsCrankDocked()
end

function mcp.registerState(fn)
    stateFn = fn
end

-- Shared by mcp.run() and the auto-wrap below: xpcall guarantees
-- mcp.update() (the harness's own command-polling loop) always runs
-- after the game's frame logic, whether or not that logic threw - so an
-- uncaught error in the game's own code can no longer freeze
-- get_game_state/get_screenshot/list_entities along with it. The
-- traceback lands in the same game_logs.jsonl channel as print() output.
local function wrapUpdate(fn)
    return function()
        local ok, err = xpcall(fn, debug.traceback)
        if not ok then
            appendGameLog("error", err)
        end

        -- mcp.update() is protected too, not just the game's frame logic. It used
        -- to be called bare, which left a real hole: it invokes the game's own
        -- button callbacks (see callGameCallback) and reads game-defined sprite
        -- fields for list_entities, so game code runs inside it. An error there
        -- escaped to the SDK, so the traceback went unrecorded and the polling loop
        -- stopped - the exact freeze this wrapper exists to prevent, reached through
        -- the wrapper itself.
        --
        -- callGameCallback already catches the known case closer to the source,
        -- which is better because the rest of the frame's harness work still runs.
        -- This is the backstop for everything else, so that nothing inside the
        -- harness can take the harness down.
        local updateOk, updateErr = xpcall(mcp.update, debug.traceback)
        if not updateOk then
            appendGameLog("error", updateErr)
        end
    end
end

-- Games can call this once instead of relying on the auto-wrap below,
-- to be explicit about it. rawset bypasses the __newindex hook below
-- deliberately - this is already the fully-wrapped function, wrapping it
-- again would call mcp.update() twice a frame for no benefit.
function mcp.run(gameUpdateFn)
    rawset(playdate, "update", wrapUpdate(gameUpdateFn))
end

-- Auto-wrap, order-independent: a game calling mcp.registerState() (or
-- any other mcp.* function) needs "import mcp_harness" to come *before*
-- that call, but wrapping whatever playdate.update already exists only
-- works if the import comes *after* the game assigns it - those two
-- requirements can't both be satisfied by import placement alone. A
-- __newindex hook on the playdate table sidesteps the conflict entirely:
-- it intercepts the *assignment* itself, whenever and wherever it
-- happens, so "import mcp_harness" can go anywhere - typically first,
-- alongside a game's other CoreLibs imports, so mcp.registerState() etc.
-- work normally throughout the rest of the file.

-- Covers the rare case playdate.update already has a value at the point
-- this file is imported (nothing else in the SDK sets one by default,
-- but be defensive). Once raw-set here, the key exists on the table, so
-- __newindex below won't fire again for it specifically - fine, since
-- games don't normally reassign playdate.update more than once.
if rawget(playdate, "update") then
    rawset(playdate, "update", wrapUpdate(rawget(playdate, "update")))
end

local playdateMeta = getmetatable(playdate)
if not playdateMeta then
    playdateMeta = {}
    setmetatable(playdate, playdateMeta)
end
local previousNewIndex = playdateMeta.__newindex
playdateMeta.__newindex = function(t, k, v)
    if k == "update" and type(v) == "function" then
        v = wrapUpdate(v)
    end
    if type(previousNewIndex) == "function" then
        previousNewIndex(t, k, v)
    elseif type(previousNewIndex) == "table" then
        rawset(previousNewIndex, k, v)
    else
        rawset(t, k, v)
    end
end

local function applyPress(button, durationMs, nowMs)
    override.button[button] = {active = true, value = true, expiresAt = nowMs + durationMs}
end

local function applyRelease(button, durationMs, nowMs)
    -- Actively forces not-pressed for the duration, symmetric with press -
    -- see the C harness's mcp_override_apply_release for why this isn't
    -- just clearing the override.
    override.button[button] = {active = true, value = false, expiresAt = nowMs + durationMs}
end

-- dockedActive/docked come from the command's crank_dock mode, resolved by
-- dockOverrideFromMode. Kept as two values rather than passing the mode string in
-- so this mirrors the C harness's mcp_override_apply_crank exactly - the two are
-- read side by side often enough that matching shapes is worth more than saving a
-- line.
--
-- durationMs <= 0 holds the crank until something else moves it, rather than
-- expiring it immediately.
--
-- The crank is a position, not an event: "set the crank to 123 degrees" means
-- leave it there, the way a real crank stays where it was left. Treating an
-- omitted duration as a zero-length one made set_crank report success and do
-- nothing, because expireOverrides runs at the top of every frame and
-- nowMs >= nowMs + 0 is immediately true.
--
-- Buttons keep expiring, deliberately: nothing exposes a release, so a button
-- held indefinitely could never be let go. Mirrors mcp_override_apply_crank.
local function applyCrank(angle, delta, dockedActive, docked, durationMs, nowMs)
    override.crank.active = true
    override.crank.angle = angle
    override.crank.delta = delta
    override.crank.dockedActive = dockedActive
    override.crank.docked = docked
    override.crank.expiresAt = durationMs > 0 and (nowMs + durationMs) or NO_EXPIRY
end

-- Resolves a crank_dock wire value into "override the dock at all" and "force it
-- to what". Anything unrecognised, including nil and "", means leave it alone:
-- every field of a command has to be safe at its zero value, since a ping sends
-- the whole shape zeroed. See internal/harness/protocol.go's DockOverride, which
-- is the same three cases in Go.
local function dockOverrideFromMode(mode)
    if mode == "docked" then
        return true, true
    elseif mode == "undocked" then
        return true, false
    end
    return false, false
end

local function expireOverrides(nowMs)
    for _, o in pairs(override.button) do
        if o.active and nowMs >= o.expiresAt then
            o.active = false
        end
    end
    if override.crank.active and override.crank.expiresAt ~= NO_EXPIRY
        and nowMs >= override.crank.expiresAt then
        override.crank.active = false
        override.crank.dockedActive = false
    end
end

local function emptyResponse()
    return {
        id = "",
        status = "error",
        error = "",
        harness_version = HARNESS_VERSION,
        format = "none",
        path = "",
        width = 0,
        height = 0,
        row_bytes = 0,
        state = nil,
        entities = nil,
        entities_complete = false,
    }
end

-- getAllSprites() is a true, complete enumeration of the display list -
-- unlike the C harness's querySpritesInRect approximation, this never
-- misses a sprite regardless of whether it has a collide rect set.
local function listEntities()
    local entities = {}
    for _, s in ipairs(playdate.graphics.sprite.getAllSprites()) do
        local x, y, width, height = s:getBounds()
        table.insert(entities, {
            tag = s:getTag(),
            -- className is never nil, even for a plain, non-subclassed
            -- sprite - that case reports the base class name "Sprite".
            class_name = s.className,
            x = x,
            y = y,
            width = width,
            height = height,
            z_index = s:getZIndex(),
            visible = s:isVisible(),
        })
    end
    return entities
end

function mcp.update()
    local nowMs = playdate.getCurrentTimeMilliseconds()
    expireOverrides(nowMs)
    updateButtonEdges()

    if not playdate.file.exists(COMMAND_PATH) then
        return
    end

    local f = playdate.file.open(COMMAND_PATH, playdate.file.kFileRead)
    if not f then return end
    local content = f:read(65536)
    f:close()
    playdate.file.delete(COMMAND_PATH)

    local ok, cmd = pcall(json.decode, content)
    local resp = emptyResponse()

    if not ok or type(cmd) ~= "table" or type(cmd.type) ~= "string" then
        resp.error = "failed to parse command"
    else
        resp.id = cmd.id or ""
        local t = cmd.type
        if t == "ping" then
            resp.status = "ok"
        elseif t == "press" then
            local btn = buttonFromString(cmd.button)
            if btn then applyPress(btn, cmd.duration_ms or 0, nowMs) end
            resp.status = "ok"
        elseif t == "release" then
            local btn = buttonFromString(cmd.button)
            if btn then applyRelease(btn, cmd.duration_ms or 0, nowMs) end
            resp.status = "ok"
        elseif t == "crank" then
            local dockedActive, docked = dockOverrideFromMode(cmd.crank_dock)
            applyCrank(cmd.crank_angle or 0, cmd.crank_delta or 0, dockedActive, docked, cmd.duration_ms or 0, nowMs)
            resp.status = "ok"
        elseif t == "state" then
            if stateFn then
                local callOk, s = pcall(stateFn)
                if callOk and s then
                    local decodeOk, decoded = pcall(json.decode, s)
                    if decodeOk then resp.state = decoded end
                end
            end
            resp.status = "ok"
        elseif t == "screenshot" then
            -- simulator.writeToFile() takes a path on the dev machine, not
            -- a path in the sandboxed Data directory like the rest of the
            -- file API - it's meant for exporting dev-time assets, not
            -- reading/writing game data. Without an absolute base (passed
            -- in via playdate.argv[2] by whatever launched the Simulator -
            -- argv[1] is always the pdx path itself), it resolves relative
            -- to the Simulator process's own cwd, which is almost never
            -- the Data directory.
            local relPath = "mcp/screenshot.png"
            local path = relPath
            if playdate.argv and playdate.argv[2] then
                path = playdate.argv[2] .. "/" .. relPath
            end
            local image = playdate.graphics.getDisplayImage()
            playdate.simulator.writeToFile(image, path)
            resp.status = "ok"
            resp.format = "png"
            resp.path = relPath
            resp.width = 400
            resp.height = 240
        elseif t == "entities" then
            resp.status = "ok"
            resp.entities = listEntities()
            resp.entities_complete = true
        else
            resp.error = "unknown command type"
        end
    end

    -- Published by rename, so response.json only ever exists complete. Written in
    -- place, a reader can catch the file after the truncating open (zero length) or
    -- part-written, and the reader has no way to tell "short" from "finished".
    --
    -- Not a bug anyone hit: 240 calls against a 512KB response on Linux/overlayfs
    -- found no partial reads, so in-place writing was latent rather than broken
    -- there. It is done this way because it costs one rename, because the reader's
    -- own handling of a short read used to be actively destructive, and because
    -- native mode will run on filesystems nobody has measured.
    --
    -- The useful side effect is that the response becomes a commit point: anything
    -- it refers to, like the screenshot written earlier this frame, is finished
    -- before response.json exists at all. That was already true by statement order;
    -- now it is true by construction.
    local out = playdate.file.open(RESPONSE_TMP_PATH, playdate.file.kFileWrite)
    if out then
        out:write(json.encode(resp))
        out:close()
        if not playdate.file.rename(RESPONSE_TMP_PATH, RESPONSE_PATH) then
            -- A response that might be read short beats no response at all, and the
            -- Go side tolerates a short read by waiting rather than failing.
            local direct = playdate.file.open(RESPONSE_PATH, playdate.file.kFileWrite)
            if direct then
                direct:write(json.encode(resp))
                direct:close()
            end
        end
    end
end
