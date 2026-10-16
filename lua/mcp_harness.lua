local mcp = {}
_G.mcp = mcp

playdate.file.mkdir("mcp")

local override = {
    button = {},
    -- The dock is overridden only when a command asks, not whenever the angle is.
    crank = {active = false, angle = 0, delta = 0, docked = false, dockedActive = false, expiresAt = 0},
}

-- Expiry meaning "never", set by a non-positive duration on any override.
-- Negative so it can't collide with a real deadline.
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

-- Content fingerprint of this harness, stamped by `setup` and reported in every
-- response so the server can detect a drifted copy. Not maintained by hand.
local HARNESS_VERSION = "@HARNESS_VERSION@"

-- Responses are written to the tmp path and renamed into place.
local COMMAND_PATH = "mcp/command.json"
local RESPONSE_PATH = "mcp/response.json"
local RESPONSE_TMP_PATH = "mcp/response.tmp.json"

-- print() output and tracebacks, appended as JSON lines. The Simulator's stdout
-- is block-buffered and lost on a hard kill, so this file is the reliable copy
-- (see docs/GOTCHAS.md). Rotated by rename into two generations.
local GAME_LOGS_PATH = "mcp/game_logs.jsonl"
local GAME_LOGS_PREV_PATH = "mcp/game_logs.1.jsonl"
-- Pre-JSONL log file, only referenced to delete it.
local LEGACY_GAME_LOGS_PATH = "mcp/game_logs.json"
-- Per generation.
local GAME_LOGS_MAX_BYTES = 256 * 1024
local gameLogBytes = 0

-- The old generation is deleted first because playdate.file.rename doesn't
-- document overwriting. A failed rename truncates rather than growing unbounded.
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

    -- Closed per call so a log written just before a crash still lands on disk.
    local f = playdate.file.open(GAME_LOGS_PATH, playdate.file.kFileAppend)
    if f then
        f:write(line)
        f:close()
        gameLogBytes = gameLogBytes + #line
    end
end

-- Logs are per run. The legacy file is deleted because the server treats its
-- presence as a stale harness copy.
local function resetGameLog()
    local f = playdate.file.open(GAME_LOGS_PATH, playdate.file.kFileWrite)
    if f then
        f:close()
    end
    gameLogBytes = 0
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

-- Synthetic edges for overridden buttons. The SDK's own edge functions and
-- *ButtonDown callbacks only reflect real hardware.
local lastEffectivePressed = {}
local overrideWasActiveLastFrame = {}
local justPressedSynthetic = {}
local justReleasedSynthetic = {}

-- Prefix for each button's *ButtonDown/*ButtonUp callback, casing per Inside
-- Playdate. AButtonHeld/BButtonHeld are not synthesized.
local buttonCallbackPrefix = {
    [playdate.kButtonA] = "A",
    [playdate.kButtonB] = "B",
    [playdate.kButtonUp] = "up",
    [playdate.kButtonDown] = "down",
    [playdate.kButtonLeft] = "left",
    [playdate.kButtonRight] = "right",
}

-- Calls a game button callback, logging anything it throws, so one broken
-- callback doesn't stop the rest of this frame's harness work.
local function callGameCallback(fn)
    if not fn then return end
    local ok, err = xpcall(fn, debug.traceback)
    if not ok then
        appendGameLog("error", err)
    end
end

-- Runs before the frame's command is processed, so a command's edge shows up
-- next frame. Buttons never overridden are left alone.
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
        -- Overridden buttons use synthetic edges, as in buttonJustPressed.
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
    if override.crank.active and override.crank.dockedActive then
        return override.crank.docked
    end
    return realIsCrankDocked()
end

function mcp.registerState(fn)
    stateFn = fn
end

-- Runs the game's update then mcp.update(), each under xpcall, so a game error
-- can't stop the harness answering. Tracebacks go to the game log.
local function wrapUpdate(fn)
    return function()
        local ok, err = xpcall(fn, debug.traceback)
        if not ok then
            appendGameLog("error", err)
        end

        -- mcp.update() runs game code too (callbacks, sprite fields).
        local updateOk, updateErr = xpcall(mcp.update, debug.traceback)
        if not updateOk then
            appendGameLog("error", updateErr)
        end
    end
end

-- Explicit alternative to the auto-wrap. rawset skips __newindex so the
-- update isn't wrapped twice.
function mcp.run(gameUpdateFn)
    rawset(playdate, "update", wrapUpdate(gameUpdateFn))
end

-- Auto-wrap via __newindex on the playdate table, so the import can come
-- before or after the game assigns playdate.update. An update already set at
-- import is wrapped directly; __newindex won't fire again for that key.
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

-- durationMs <= 0 holds the button until something replaces it.
local function applyPress(button, durationMs, nowMs)
    override.button[button] = {
        active = true,
        value = true,
        expiresAt = durationMs > 0 and (nowMs + durationMs) or NO_EXPIRY,
    }
end

local function applyRelease(button, durationMs, nowMs)
    -- Forces not-pressed rather than passthrough, so it wins even while a human
    -- is also pressing real buttons.
    override.button[button] = {
        active = true,
        value = false,
        expiresAt = durationMs > 0 and (nowMs + durationMs) or NO_EXPIRY,
    }
end

-- durationMs <= 0 holds the crank where it was set, like a real crank; a
-- zero-length expiry would fire before the game ever read it.
local function applyCrank(angle, delta, dockedActive, docked, durationMs, nowMs)
    override.crank.active = true
    override.crank.angle = angle
    override.crank.delta = delta
    override.crank.dockedActive = dockedActive
    override.crank.docked = docked
    override.crank.expiresAt = durationMs > 0 and (nowMs + durationMs) or NO_EXPIRY
end

-- Returns (override the dock?, docked?). Anything unrecognised, including nil
-- and "", leaves it alone: every command field must be safe when zeroed.
local function dockOverrideFromMode(mode)
    if mode == "docked" then
        return true, true
    elseif mode == "undocked" then
        return true, false
    end
    return false, false
end

-- Drops every override. Edge tracking is kept, so a held button still gets its
-- released edge and ButtonUp callback.
local function applyReset()
    override.button = {}
    override.crank.active = false
    override.crank.dockedActive = false
end

local function expireOverrides(nowMs)
    for _, o in pairs(override.button) do
        if o.active and o.expiresAt ~= NO_EXPIRY and nowMs >= o.expiresAt then
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

-- getAllSprites() is complete, unlike the C harness's querySpritesInRect.
local function listEntities()
    local entities = {}
    for _, s in ipairs(playdate.graphics.sprite.getAllSprites()) do
        local x, y, width, height = s:getBounds()
        table.insert(entities, {
            tag = s:getTag(),
            -- Never nil: a plain sprite reports "Sprite".
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
        elseif t == "reset" then
            applyReset()
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
            -- writeToFile() takes a host path, not a Data-directory one, so the
            -- server passes an absolute base as argv[2] (argv[1] is the pdx).
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

    -- Published by rename so a reader never sees it truncated or part-written.
    local out = playdate.file.open(RESPONSE_TMP_PATH, playdate.file.kFileWrite)
    if out then
        out:write(json.encode(resp))
        out:close()
        if not playdate.file.rename(RESPONSE_TMP_PATH, RESPONSE_PATH) then
            -- A possibly-short response beats none; the Go side waits out short reads.
            local direct = playdate.file.open(RESPONSE_PATH, playdate.file.kFileWrite)
            if direct then
                direct:write(json.encode(resp))
                direct:close()
            end
        end
    end
end
