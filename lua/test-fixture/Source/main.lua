import "CoreLibs/sprites"
import "mcp_harness"

-- getAllSprites() should return both, unlike the C harness's collide-rect query.
local collidable = playdate.graphics.sprite.new()
collidable:setSize(16, 16)
collidable:moveTo(50, 60)
collidable:setCollideRect(0, 0, 16, 16)
collidable:add()

local decorative = playdate.graphics.sprite.new()
decorative:setSize(8, 8)
decorative:moveTo(100, 120)
decorative:add()

-- Counted to prove press_button fires the synthesized ButtonDown/ButtonUp callbacks.
local aDownCount = 0
local aUpCount = 0

local callbackErrorArmed = false

playdate.AButtonDown = function()
    aDownCount += 1
    if callbackErrorArmed then
        callbackErrorArmed = false
        error("deliberate callback error")
    end
end
playdate.AButtonUp = function() aUpCount += 1 end

mcp.registerState(function()
    local current, pushed, released = playdate.getButtonState()
    local angle = playdate.getCrankPosition()
    local change = playdate.getCrankChange()
    local docked = playdate.isCrankDocked()
    return json.encode({
        current = current,
        pushed = pushed,
        released = released,
        crank_angle = angle,
        crank_change = change,
        crank_docked = docked,
        a_down_count = aDownCount,
        a_up_count = aUpCount,
        a_just_pressed = playdate.buttonJustPressed(playdate.kButtonA),
        a_just_released = playdate.buttonJustReleased(playdate.kButtonA),
    })
end)

-- For get_game_logs: a print to read back, and errors triggered by sentinel
-- crank angles, since the protocol has no way to run arbitrary Lua.
print("fixture print line")

local ERROR_TRIGGER_ANGLE = 999999
local errorTriggered = false

-- An error inside a callback the harness invokes, not the game's frame logic.
local CALLBACK_ERROR_TRIGGER_ANGLE = 777777

-- Floods the log past one generation; the two markers must land in different files.
local ROTATION_TRIGGER_ANGLE = 888888
local rotationTriggered = false

local function floodLogPastRotation()
    print("ROTATION-MARKER-OLD")
    local filler = string.rep("x", 4096)
    for i = 1, 80 do
        print(filler .. " " .. i)
    end
    print("ROTATION-MARKER-NEW")
end

-- Triggers are latched to fire once per activation, not every frame the
-- override holds, which would flush the print line out of the log.
local function fixtureUpdate()
    if playdate.getCrankPosition() == CALLBACK_ERROR_TRIGGER_ANGLE then
        callbackErrorArmed = true
    end

    if playdate.getCrankPosition() == ROTATION_TRIGGER_ANGLE then
        if not rotationTriggered then
            rotationTriggered = true
            floodLogPastRotation()
        end
    else
        rotationTriggered = false
    end

    if playdate.getCrankPosition() == ERROR_TRIGGER_ANGLE then
        if not errorTriggered then
            errorTriggered = true
            error("deliberate fixture error")
        end
    else
        errorTriggered = false
    end
end

mcp.run(fixtureUpdate)
