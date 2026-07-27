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

mcp.registerState(function()
    local current, pushed, released = playdate.getButtonState()
    local angle = playdate.getCrankPosition()
    local change = playdate.getCrankChange()
    local docked = playdate.isCrankDocked()
    return json.encode({
        current = current,
        crank_angle = angle,
        crank_change = change,
        crank_docked = docked,
    })
end)

function playdate.update()
    mcp.update()
end
