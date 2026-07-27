local mcp = {}
_G.mcp = mcp

playdate.file.mkdir("mcp")

local override = {
    button = {},
    crank = {active = false, angle = 0, delta = 0, docked = false, expiresAt = 0},
}

local stateFn = nil

local realButtonIsPressed = playdate.buttonIsPressed
local realButtonJustPressed = playdate.buttonJustPressed
local realButtonJustReleased = playdate.buttonJustReleased
local realGetButtonState = playdate.getButtonState
local realGetCrankPosition = playdate.getCrankPosition
local realGetCrankChange = playdate.getCrankChange
local realIsCrankDocked = playdate.isCrankDocked
local realPrint = print

-- PlaydateSimulator's own Lua console (print()/error output) never
-- touches the process's real stdout/stderr on Linux (SDK 3.1.1) - it
-- renders only into an internal GUI console widget, despite the SDK docs'
-- claim otherwise. get_logs (the Go-side tool reading real process
-- stdout/stderr) can't see any of this, so this harness captures it
-- itself into a small file-based channel instead. Capped so the file
-- can't grow unbounded over a long play session; oldest entries drop
-- first.
local GAME_LOGS_MAX = 200
local gameLogs = {}

local function appendGameLog(logType, message)
    table.insert(gameLogs, {type = logType, message = message, ms = playdate.getCurrentTimeMilliseconds()})
    if #gameLogs > GAME_LOGS_MAX then
        table.remove(gameLogs, 1)
    end
    -- Flushed on every call, not batched into mcp.update() - so a log
    -- written the frame before a crash still lands on disk even if
    -- mcp.update() itself never runs again afterward.
    local f = playdate.file.open("mcp/game_logs.json", playdate.file.kFileWrite)
    if f then
        f:write(json.encode(gameLogs))
        f:close()
    end
end

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
                local fn = playdate[prefix .. "ButtonDown"]
                if fn then fn() end
            elseif not effective and lastEffectivePressed[button] then
                justReleasedSynthetic[button] = true
                local fn = playdate[prefix .. "ButtonUp"]
                if fn then fn() end
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
    if override.crank.active then
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
-- traceback lands in the same game_logs.json channel as print() output.
local function wrapUpdate(fn)
    return function()
        local ok, err = xpcall(fn, debug.traceback)
        if not ok then
            appendGameLog("error", err)
        end
        mcp.update()
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

local function applyCrank(angle, delta, docked, durationMs, nowMs)
    override.crank.active = true
    override.crank.angle = angle
    override.crank.delta = delta
    override.crank.docked = docked
    override.crank.expiresAt = nowMs + durationMs
end

local function expireOverrides(nowMs)
    for _, o in pairs(override.button) do
        if o.active and nowMs >= o.expiresAt then
            o.active = false
        end
    end
    if override.crank.active and nowMs >= override.crank.expiresAt then
        override.crank.active = false
    end
end

local function emptyResponse()
    return {
        id = "",
        status = "error",
        error = "",
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

    if not playdate.file.exists("mcp/command.json") then
        return
    end

    local f = playdate.file.open("mcp/command.json", playdate.file.kFileRead)
    if not f then return end
    local content = f:read(65536)
    f:close()
    playdate.file.delete("mcp/command.json")

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
            applyCrank(cmd.crank_angle or 0, cmd.crank_delta or 0, cmd.crank_docked or false, cmd.duration_ms or 0, nowMs)
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

    local out = playdate.file.open("mcp/response.json", playdate.file.kFileWrite)
    if out then
        out:write(json.encode(resp))
        out:close()
    end
end
