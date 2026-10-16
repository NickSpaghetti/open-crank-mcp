#ifndef MCP_HARNESS_H
#define MCP_HARNESS_H

#include <stddef.h>
#include "pd_api.h"

/* Content fingerprint of this harness, stamped by `setup` and reported in every
   response so the server can detect a drifted copy. Not maintained by hand. */
#define MCP_HARNESS_VERSION "@HARNESS_VERSION@"

/* IPC files, relative to the game's data directory. Responses are written to
   the tmp path and renamed into place. */
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
    MCP_CMD_ENTITIES,
    MCP_CMD_RESET
} McpCommandType;

typedef struct {
    char id[64];
    McpCommandType type;
    PDButtons button;
    int duration_ms;
    float crank_angle;
    float crank_delta;
    /* From the wire's crank_dock string: whether to override the dock state,
       and what to force it to. */
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
    /* Raw JSON array; empty means "not requested". */
    char entities[8192];
    /* Always false: querySpritesInRect, the C API's only bulk sprite query,
       only sees sprites with a collide rect. */
    int entities_complete;
} McpResponse;

/* Expiry meaning "never", set by a non-positive duration on any override.
   Negative so it can't collide with a real deadline. */
#define MCP_NO_EXPIRY (-1L)

typedef struct {
    int button_override_active[6];
    int button_override_value[6];
    long button_override_expires_at_ms[6];
    int crank_override_active;
    float crank_override_angle;
    float crank_override_delta;
    int crank_override_docked;
    /* The dock state is overridden only when a command asks, not whenever the
       angle is. */
    int crank_override_docked_active;
    long crank_override_expires_at_ms;
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

/* Drops every override so the game reads real input again. Unlike
   mcp_override_init it keeps edge tracking, so held buttons get a released edge. */
void mcp_override_reset(McpOverrideState *ov);
void mcp_override_apply_press(McpOverrideState *ov, PDButtons button, int duration_ms, long now_ms);
void mcp_override_apply_release(McpOverrideState *ov, PDButtons button, int duration_ms, long now_ms);
void mcp_override_apply_crank(McpOverrideState *ov, float angle, float delta,
                              int docked_set, int docked, int duration_ms, long now_ms);
void mcp_override_expire(McpOverrideState *ov, long now_ms);

/* Computes this frame's synthetic pushed/released edges for overridden buttons.
   Call once per frame after mcp_override_expire and before applying new
   commands, so a command's edge shows up on the next frame. */
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

/* pd->system lives in read-only memory in the Simulator (writing through it
   segfaults), so a C game must read input through the mcp_get_* wrappers for
   overrides to take effect. */

void mcp_harness_init(PlaydateAPI *pd);
void mcp_harness_update(PlaydateAPI *pd);
void mcp_harness_register_state(const char *(*fn)(void));

/* Builds a JSON array of the sprites querySpritesInRect finds on screen.
   Returns the written length, or -1 if it didn't fit. */
int mcp_build_entities_json(PlaydateAPI *pd, char *out, size_t out_len);

void mcp_get_button_state(PlaydateAPI *pd, PDButtons *current, PDButtons *pushed, PDButtons *released);
float mcp_get_crank_angle(PlaydateAPI *pd);
float mcp_get_crank_change(PlaydateAPI *pd);
int mcp_get_crank_docked(PlaydateAPI *pd);

#endif
