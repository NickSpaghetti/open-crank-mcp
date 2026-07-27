import "CoreLibs/sprites"
import "mcp_harness"

-- One sprite with a collide rect, one without - so the entities command
-- has something real to distinguish (Lua's getAllSprites() should return
-- both regardless, unlike the C harness's collide-rect approximation).
local collidable = playdate.graphics.sprite.new()
collidable:setSize(16, 16)
collidable:moveTo(50, 60)
collidable:setCollideRect(0, 0, 16, 16)
collidable:add()

local decorative = playdate.graphics.sprite.new()
decorative:setSize(8, 8)
decorative:moveTo(100, 120)
decorative:add()

-- Exercises the button-edge fix: AButtonDown/AButtonUp are SDK-invoked
-- callbacks, not something a game polls - the only way to prove press_button
-- actually triggers them (not just buttonIsPressed's "currently held" bit)
-- is to define them and count invocations. See mcp_harness.lua's
-- updateButtonEdges for how these get synthesized from the override.
local aDownCount = 0
local aUpCount = 0

playdate.AButtonDown = function() aDownCount += 1 end
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

-- Exercises get_game_logs: a real print() call the contract test can read
-- back, and a deliberate error - triggered via the existing "crank" command
-- with this exact sentinel angle, rather than a new command type, since
-- the harness protocol only speaks fixed JSON commands, not arbitrary Lua
-- calls - proving mcp.run() keeps the harness alive afterward instead of
-- freezing it.
print("fixture print line")

local ERROR_TRIGGER_ANGLE = 999999
local errorTriggered = false

-- Latched so this throws exactly once per activation, not every single
-- frame the override stays active (which would flood game_logs.json with
-- duplicate entries and evict the print() line above out of the ring
-- buffer before the test ever reads it back).
local function fixtureUpdate()
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
