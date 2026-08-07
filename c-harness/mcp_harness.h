#ifndef MCP_HARNESS_H
#define MCP_HARNESS_H

#include <stddef.h>
#include "pd_api.h"

/* Which canonical harness this copy came from, reported in every response so the
   Go side can tell whether a game's vendored copy has drifted.

   Not maintained by hand: the `setup` tool substitutes a content fingerprint of
   the canonical sources (this header and mcp_harness.c together) as it writes
   this file into a game, so every harness change produces a new value with
   nothing to remember. The literal below is what ships in this repo; a copy still
   carrying it was not installed by `setup`, which the server reports as its own
   case. See internal/harness/version.go. */
#define MCP_HARNESS_VERSION "@HARNESS_VERSION@"

/* The IPC files, relative to the game's sandboxed data directory.

   MCP_RESPONSE_TMP_PATH exists so a response can be published by rename rather
   than written in place - see the end of mcp_harness_update. */
#define MCP_COMMAND_PATH      "mcp/command.json"
#define MCP_RESPONSE_PATH     "mcp/response.json"
#define MCP_RESPONSE_TMP_PATH "mcp/response.tmp.json"

typedef enum {
    MCP_CMD_UNKNOWN,
    MCP_CMD_SCREENSHOT,
    MCP_CMD_PRESS,
    MCP_CMD_RELEASE,
    MCP_CMD_CRANK,
    MCP_CMD_STATE,
    MCP_CMD_PING,
    MCP_CMD_ENTITIES
} McpCommandType;

typedef struct {
    char id[64];
    McpCommandType type;
    PDButtons button;
    int duration_ms;
    float crank_angle;
    float crank_delta;
    /* Resolved from the command's crank_dock string by mcp_parse_command:
       crank_docked_set says whether the dock state should be overridden at all,
       crank_docked what to force it to. Two ints in the struct, one
       self-describing string on the wire - see internal/harness/protocol.go. */
    int crank_docked;
    int crank_docked_set;
} McpCommand;

typedef struct {
    char id[64];
    int ok;
    char error[256];
    int is_raw_screenshot;
    char path[256];
    int width;
    int height;
    int row_bytes;
    char state[4096];
    /* Raw JSON array string, built by mcp_build_entities_json. Empty means
       "not requested", matching how an empty state means the same thing. */
    char entities[8192];
    /* Always false for this harness: querySpritesInRect (the only bulk
       sprite query the C API has) only matches sprites with a collide
       rect set, so this can never be a complete list the way Lua's
       getAllSprites() is. See mcp_build_entities_json. */
    int entities_complete;
} McpResponse;

/* Sentinel expiry meaning "never". Negative so it can never collide with a real
   deadline, which is always now_ms + a duration and therefore non-negative. */
#define MCP_NO_EXPIRY (-1L)

typedef struct {
    int button_override_active[6];
    int button_override_value[6];
    long button_override_expires_at_ms[6];
    int crank_override_active;
    float crank_override_angle;
    float crank_override_delta;
    int crank_override_docked;
    /* Separate from crank_override_active: a crank override always takes over
       angle and delta, but takes over the dock state only when a command asked
       it to. Without the split, moving the angle would force a dock reading the
       game never asked for, and "docked" is the crank's resting state on real
       hardware, so that is not a harmless default to pick. */
    int crank_override_docked_active;
    /* MCP_NO_EXPIRY means the crank override is held until something replaces
       it, rather than until a deadline. See mcp_override_apply_crank. */
    long crank_override_expires_at_ms;
    /* Edge tracking for mcp_override_update_edges - see its definition. */
    int last_effective_pressed[6];
    int override_was_active_last_frame[6];
    PDButtons pending_pushed;
    PDButtons pending_released;
} McpOverrideState;

/* Pure logic: no PlaydateAPI dependency, directly unit-testable. */

int mcp_parse_command(const char *json, size_t len, McpCommand *out);
int mcp_format_response(const McpResponse *r, char *buf, size_t buflen);
int mcp_json_escape_string(const char *in, char *out, size_t out_len);

void mcp_override_init(McpOverrideState *ov);
void mcp_override_apply_press(McpOverrideState *ov, PDButtons button, int duration_ms, long now_ms);
void mcp_override_apply_release(McpOverrideState *ov, PDButtons button, int duration_ms, long now_ms);
void mcp_override_apply_crank(McpOverrideState *ov, float angle, float delta,
                              int docked_set, int docked, int duration_ms, long now_ms);
void mcp_override_expire(McpOverrideState *ov, long now_ms);

/* Computes this frame's pending_pushed/pending_released from the override
   state and the real current button bitmask, comparing against last
   frame's effective (override-or-real) pressed state per button. Call
   once per frame, after mcp_override_expire and before any new
   press/release command is applied - so a fresh command's edge only
   shows up starting the *next* frame's call, not the same frame it
   arrived on (predictable latency, avoids depending on read-order within
   a frame). Buttons never touched by an override are left alone: their
   real pushed/released bits pass through mcp_override_get_button_state
   unchanged, so there's no risk of double-firing a real edge the SDK
   already reported on its own. */
void mcp_override_update_edges(McpOverrideState *ov, PDButtons real_current);

void mcp_override_get_button_state(const McpOverrideState *ov,
                                    PDButtons real_current, PDButtons real_pushed, PDButtons real_released,
                                    PDButtons *out_current, PDButtons *out_pushed, PDButtons *out_released);
float mcp_override_get_crank_angle(const McpOverrideState *ov, float real_angle);
float mcp_override_get_crank_change(const McpOverrideState *ov, float real_change);
int mcp_override_get_crank_docked(const McpOverrideState *ov, int real_docked);

/* Button bit index (0-5) for the McpOverrideState arrays, or -1 if button
   is not exactly one of the six known buttons. */
int mcp_button_index(PDButtons button);

/* Glue: calls through the PlaydateAPI it's given.

   pd->system is `const struct playdate_sys*` in the real Simulator, and
   verified in practice (not just by the type) to live in memory that's
   actually read-only - casting the const away and writing through it
   segfaults for real, it isn't just nominally unsafe. So unlike the Lua
   harness (which patches playdate.buttonIsPressed etc. directly, since
   Lua's table is genuinely mutable), a C game must call these wrapper
   functions instead of pd->system->getButtonState/getCrankAngle/etc.
   directly for overrides to take effect. That's a real, load-bearing
   difference in how invasive wiring in each harness is - documented here
   rather than left to be discovered as a mystery crash. */

void mcp_harness_init(PlaydateAPI *pd);
void mcp_harness_update(PlaydateAPI *pd);
void mcp_harness_register_state(const char *(*fn)(void));

/* Builds a JSON array of every sprite querySpritesInRect finds over the
   full screen rect (only sprites with a collide rect set - see the
   entities_complete comment on McpResponse) into out. Returns the
   written length, or -1 if it didn't fit. */
int mcp_build_entities_json(PlaydateAPI *pd, char *out, size_t out_len);

void mcp_get_button_state(PlaydateAPI *pd, PDButtons *current, PDButtons *pushed, PDButtons *released);
float mcp_get_crank_angle(PlaydateAPI *pd);
float mcp_get_crank_change(PlaydateAPI *pd);
int mcp_get_crank_docked(PlaydateAPI *pd);

#endif
