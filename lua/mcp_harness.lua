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

function playdate.buttonJustPressed(button)
    -- Not faked, only "currently held" is overridden - same simplification
    -- as the C harness, see its mcp_override_get_button_state.
    return realButtonJustPressed(button)
end

function playdate.buttonJustReleased(button)
    return realButtonJustReleased(button)
end

function playdate.getButtonState()
    local current, pressed, released = realGetButtonState()
    for buttonConst, o in pairs(override.button) do
        if o.active then
            if o.value then
                current = current | buttonConst
            else
                current = current & ~buttonConst
            end
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
    }
end

function mcp.update()
    local nowMs = playdate.getCurrentTimeMilliseconds()
    expireOverrides(nowMs)

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
