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

-- Armed by the sentinel crank angle below, so the counting behaviour the other
-- assertions rely on is unaffected until a test asks for the failure.
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

-- Exercises get_game_logs: a real print() call the contract test can read
-- back, and a deliberate error - triggered via the existing "crank" command
-- with this exact sentinel angle, rather than a new command type, since
-- the harness protocol only speaks fixed JSON commands, not arbitrary Lua
-- calls - proving mcp.run() keeps the harness alive afterward instead of
-- freezing it.
print("fixture print line")

local ERROR_TRIGGER_ANGLE = 999999
local errorTriggered = false

-- Second sentinel, same mechanism, for the log rotation. Writing past the
-- harness's per-generation size cap is the only way to make a rotation happen, and
-- 256KB of real print() calls at a realistic entry size would take minutes of
-- gameplay - so this floods it deliberately with large lines instead.
--
-- The two markers are what the contract test looks for. ROTATION-MARKER-OLD is
-- printed first and must end up in the rotated generation; ROTATION-MARKER-NEW is
-- printed after the rotation and lands in the fresh one. Reading both back proves
-- the rotation moved the old generation aside rather than deleting it, which is
-- what the first implementation did.
-- Arms an error inside a callback the *harness* invokes, rather than inside the
-- game's frame logic. That distinction is the whole point: wrapUpdate protects the
-- frame logic, and until callGameCallback existed it did not protect this.
local CALLBACK_ERROR_TRIGGER_ANGLE = 777777

local ROTATION_TRIGGER_ANGLE = 888888
local rotationTriggered = false

local function floodLogPastRotation()
    print("ROTATION-MARKER-OLD")
    -- Comfortably past one 256KB generation, in chunks big enough that this costs
    -- a frame rather than a minute.
    local filler = string.rep("x", 4096)
    for i = 1, 80 do
        print(filler .. " " .. i)
    end
    print("ROTATION-MARKER-NEW")
end

-- Latched so this throws exactly once per activation, not every single
-- frame the override stays active (which would flood game_logs.jsonl with
-- duplicate entries and push the print() line above out of the retained
-- generations before the test ever reads it back).
local function fixtureUpdate()
    if playdate.getCrankPosition() == CALLBACK_ERROR_TRIGGER_ANGLE then
        callbackErrorArmed = true
    end

    if playdate.getCrankPosition() == ROTATION_TRIGGER_ANGLE then
        -- Latched for the same reason as the error below: once per activation.
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
