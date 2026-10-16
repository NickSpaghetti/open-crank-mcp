# Exposing game state

How to make `get_game_state` return something useful.

`get_game_state` returns whatever your game hands it. There is no default. Until
you register a state function it returns nothing, and an agent playtesting your
game is working from screenshots alone.

This is the highest-value thing you can do for an agent. A screenshot shows a
sprite moved. State tells it the score went up, the level changed, and three
lives are left.

## Lua

Register a function that returns a JSON string.

```lua
import "mcp_harness"

mcp.registerState(function()
    return json.encode({
        score = game.score,
        lives = game.lives,
        level = game.level,
        paused = game.paused,
    })
end)
```

`json.encode` is from the SDK's own `json` library. The harness calls your
function on every `get_game_state`, decodes the string, and puts the result in
the response.

A complete example lives at
[`lua/test-fixture/Source/main.lua`](../lua/test-fixture/Source/main.lua).

## C

Register a function returning a `const char *`.

```c
static char state_buf[512];

static const char *report_state(void)
{
    snprintf(state_buf, sizeof(state_buf),
             "{\"score\":%d,\"lives\":%d,\"level\":%d}",
             g_score, g_lives, g_level);
    return state_buf;
}

// in eventHandler, on kEventInit:
mcp_harness_register_state(report_state);
```

There is no JSON library in the C API, so you build the string yourself. Use
`mcp_json_escape_string` from
[`c-harness/mcp_harness.h`](../c-harness/mcp_harness.h) for any value that could
contain a quote or a backslash.

A complete example lives at
[`c-harness/test/fixture-game/src/main.c`](../c-harness/test/fixture-game/src/main.c).

## Two ways this fails

**Lua: return a string, not a table.** The harness runs `json.decode` on what
you return. If that fails, or your function throws, `get_game_state` succeeds
and returns nothing, with no error anywhere. Returning a table instead of
`json.encode(table)` fails exactly this way.

**C: invalid JSON, or more than 4095 bytes.** The harness copies your string
into a 4096-byte buffer and embeds it in the response as-is. Anything longer is
cut off mid string. Invalid JSON from either cause makes the whole response
unreadable, so `get_game_state` waits out its 5 second timeout and then fails
with a `parsing response` error. Keep the state small, which you want anyway.

## What to put in it

Small and stable beats complete. An agent reads this on every step of a loop.

Good candidates:

- score, lives, level, currency
- game phase: menu, playing, paused, game over
- whether the player can act right now
- counts that should change when an input lands, like shots fired

Leave out anything an agent cannot act on. Frame counters, positions that change
every tick, and anything already visible in a screenshot mostly add noise.

Include a field that proves an input worked. The fixture games track button
press counts for this reason. It turns "did my press register" from a guess into
a check.

## list_entities covers sprites

`list_entities` is separate and needs no registration. It reports sprites in the
display list with position, bounds, tag, z-index and visibility.

The two are complementary. `list_entities` tells an agent where things are.
Your state function tells it what they mean.

The response carries a `complete` field, and it is not always true.

| Game | Backed by | Complete |
|---|---|---|
| Lua | `getAllSprites()` | yes |
| C | `querySpritesInRect` over the screen | no |

The C API has no true "list every sprite" call. The harness approximates it by
querying a screen-sized rect, which only finds sprites with a collide rect set.
A purely decorative C sprite is missed. Check `complete` before treating the list
as the whole picture.

## Checking your work

Launch the game and ask for the state directly.

```
get_game_state
```

If it comes back empty or errors, work through these in order:

1. Is `setup` run and the harness imported or included?
2. Does `get_status` report `harness_reachable: true`?
3. Is your function registered before the first frame? In C that means on
   `kEventInit`.
4. Does it return a string? In Lua, `json.encode(...)`, not the table.
5. In C, is the result valid JSON and under 4096 bytes?

See [troubleshooting.md](troubleshooting.md) for the rest.
