import "mcp_harness"

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
