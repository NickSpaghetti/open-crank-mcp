#ifndef MCP_HARNESS_H
#define MCP_HARNESS_H

#include <stddef.h>
#include "pd_api.h"

typedef enum {
    MCP_CMD_UNKNOWN,
    MCP_CMD_SCREENSHOT,
    MCP_CMD_PRESS,
    MCP_CMD_RELEASE,
    MCP_CMD_CRANK,
    MCP_CMD_STATE,
    MCP_CMD_PING
} McpCommandType;

typedef struct {
    char id[64];
    McpCommandType type;
    PDButtons button;
    int duration_ms;
    float crank_angle;
    float crank_delta;
    int crank_docked;
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
} McpResponse;

typedef struct {
    int button_override_active[6];
    int button_override_value[6];
    long button_override_expires_at_ms[6];
    int crank_override_active;
    float crank_override_angle;
    float crank_override_delta;
    int crank_override_docked;
    long crank_override_expires_at_ms;
} McpOverrideState;

/* Pure logic: no PlaydateAPI dependency, directly unit-testable. */

int mcp_parse_command(const char *json, size_t len, McpCommand *out);
int mcp_format_response(const McpResponse *r, char *buf, size_t buflen);
int mcp_json_escape_string(const char *in, char *out, size_t out_len);

void mcp_override_init(McpOverrideState *ov);
void mcp_override_apply_press(McpOverrideState *ov, PDButtons button, int duration_ms, long now_ms);
void mcp_override_apply_release(McpOverrideState *ov, PDButtons button, int duration_ms, long now_ms);
void mcp_override_apply_crank(McpOverrideState *ov, float angle, float delta, int docked, int duration_ms, long now_ms);
void mcp_override_expire(McpOverrideState *ov, long now_ms);

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

void mcp_get_button_state(PlaydateAPI *pd, PDButtons *current, PDButtons *pushed, PDButtons *released);
float mcp_get_crank_angle(PlaydateAPI *pd);
float mcp_get_crank_change(PlaydateAPI *pd);
int mcp_get_crank_docked(PlaydateAPI *pd);

#endif
