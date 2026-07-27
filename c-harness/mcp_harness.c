#include <string.h>
#include <stdio.h>
#include <stdlib.h>
#include "mcp_harness.h"

int mcp_button_index(PDButtons button)
{
    switch (button) {
        case kButtonLeft:  return 0;
        case kButtonRight: return 1;
        case kButtonUp:    return 2;
        case kButtonDown:  return 3;
        case kButtonB:     return 4;
        case kButtonA:     return 5;
        default:           return -1;
    }
}

static const PDButtons kButtonBits[6] = {
    kButtonLeft, kButtonRight, kButtonUp, kButtonDown, kButtonB, kButtonA
};

void mcp_override_init(McpOverrideState *ov)
{
    memset(ov, 0, sizeof(*ov));
}

void mcp_override_apply_press(McpOverrideState *ov, PDButtons button, int duration_ms, long now_ms)
{
    int idx = mcp_button_index(button);
    if (idx < 0) return;
    ov->button_override_active[idx] = 1;
    ov->button_override_value[idx] = 1;
    ov->button_override_expires_at_ms[idx] = now_ms + duration_ms;
}

void mcp_override_apply_release(McpOverrideState *ov, PDButtons button, int duration_ms, long now_ms)
{
    int idx = mcp_button_index(button);
    if (idx < 0) return;
    /* Actively forces not-pressed for the duration, symmetric with press -
       not just clearing the override to passthrough. A passthrough-only
       release wouldn't actually force the button up if something else was
       also driving real input (e.g. a human using the visual profile) at
       the same time. */
    ov->button_override_active[idx] = 1;
    ov->button_override_value[idx] = 0;
    ov->button_override_expires_at_ms[idx] = now_ms + duration_ms;
}

void mcp_override_apply_crank(McpOverrideState *ov, float angle, float delta, int docked, int duration_ms, long now_ms)
{
    ov->crank_override_active = 1;
    ov->crank_override_angle = angle;
    ov->crank_override_delta = delta;
    ov->crank_override_docked = docked;
    ov->crank_override_expires_at_ms = now_ms + duration_ms;
}

void mcp_override_expire(McpOverrideState *ov, long now_ms)
{
    for (int i = 0; i < 6; i++) {
        if (ov->button_override_active[i] && now_ms >= ov->button_override_expires_at_ms[i]) {
            ov->button_override_active[i] = 0;
        }
    }
    if (ov->crank_override_active && now_ms >= ov->crank_override_expires_at_ms) {
        ov->crank_override_active = 0;
    }
}

void mcp_override_get_button_state(const McpOverrideState *ov,
                                    PDButtons real_current, PDButtons real_pushed, PDButtons real_released,
                                    PDButtons *out_current, PDButtons *out_pushed, PDButtons *out_released)
{
    PDButtons current = real_current;
    for (int i = 0; i < 6; i++) {
        if (!ov->button_override_active[i]) continue;
        if (ov->button_override_value[i]) {
            current |= kButtonBits[i];
        } else {
            current &= ~kButtonBits[i];
        }
    }
    /* Pushed/released one-shot edge events are passed through unmodified.
       Faking precise edges would need to track the previous frame's
       override value per button; not done here, only "currently held"
       is overridden. Revisit if a game actually needs edge-accurate
       overrides. */
    *out_current = current;
    *out_pushed = real_pushed;
    *out_released = real_released;
}

float mcp_override_get_crank_angle(const McpOverrideState *ov, float real_angle)
{
    return ov->crank_override_active ? ov->crank_override_angle : real_angle;
}

float mcp_override_get_crank_change(const McpOverrideState *ov, float real_change)
{
    return ov->crank_override_active ? ov->crank_override_delta : real_change;
}

int mcp_override_get_crank_docked(const McpOverrideState *ov, int real_docked)
{
    return ov->crank_override_active ? ov->crank_override_docked : real_docked;
}

int mcp_json_escape_string(const char *in, char *out, size_t out_len)
{
    size_t o = 0;
    for (size_t i = 0; in[i] != '\0'; i++) {
        char c = in[i];
        const char *rep = NULL;
        char single[2] = {c, '\0'};
        switch (c) {
            case '"':  rep = "\\\""; break;
            case '\\': rep = "\\\\"; break;
            case '\n': rep = "\\n"; break;
            case '\r': rep = "\\r"; break;
            case '\t': rep = "\\t"; break;
            default:   rep = single; break;
        }
        size_t rl = strlen(rep);
        if (o + rl >= out_len) return -1;
        memcpy(out + o, rep, rl);
        o += rl;
    }
    if (o >= out_len) return -1;
    out[o] = '\0';
    return (int)o;
}

static const char *mcp_json_find_value(const char *json, const char *key)
{
    char pattern[80];
    snprintf(pattern, sizeof(pattern), "\"%s\"", key);
    const char *p = strstr(json, pattern);
    if (!p) return NULL;
    p += strlen(pattern);
    while (*p == ' ' || *p == '\t' || *p == '\n' || *p == '\r') p++;
    if (*p != ':') return NULL;
    p++;
    while (*p == ' ' || *p == '\t' || *p == '\n' || *p == '\r') p++;
    return p;
}

static int mcp_json_extract_string(const char *value_start, char *out, size_t out_len)
{
    if (*value_start != '"') return -1;
    const char *p = value_start + 1;
    size_t o = 0;
    while (*p != '"' && *p != '\0') {
        char c = *p;
        if (c == '\\' && p[1] != '\0') {
            p++;
            switch (*p) {
                case 'n': c = '\n'; break;
                case 'r': c = '\r'; break;
                case 't': c = '\t'; break;
                default:  c = *p;   break;
            }
        }
        if (o + 1 >= out_len) return -1;
        out[o++] = c;
        p++;
    }
    if (*p != '"') return -1;
    out[o] = '\0';
    return (int)o;
}

static int mcp_json_extract_number(const char *value_start, double *out)
{
    char *end = NULL;
    double v = strtod(value_start, &end);
    if (end == value_start) return -1;
    *out = v;
    return 0;
}

static int mcp_json_extract_bool(const char *value_start, int *out)
{
    if (strncmp(value_start, "true", 4) == 0) { *out = 1; return 0; }
    if (strncmp(value_start, "false", 5) == 0) { *out = 0; return 0; }
    return -1;
}

static McpCommandType mcp_command_type_from_string(const char *s)
{
    if (strcmp(s, "screenshot") == 0) return MCP_CMD_SCREENSHOT;
    if (strcmp(s, "press") == 0)      return MCP_CMD_PRESS;
    if (strcmp(s, "release") == 0)    return MCP_CMD_RELEASE;
    if (strcmp(s, "crank") == 0)      return MCP_CMD_CRANK;
    if (strcmp(s, "state") == 0)      return MCP_CMD_STATE;
    if (strcmp(s, "ping") == 0)       return MCP_CMD_PING;
    return MCP_CMD_UNKNOWN;
}

static PDButtons mcp_button_from_string(const char *s)
{
    if (strcmp(s, "a") == 0)     return kButtonA;
    if (strcmp(s, "b") == 0)     return kButtonB;
    if (strcmp(s, "up") == 0)    return kButtonUp;
    if (strcmp(s, "down") == 0)  return kButtonDown;
    if (strcmp(s, "left") == 0)  return kButtonLeft;
    if (strcmp(s, "right") == 0) return kButtonRight;
    return 0;
}

int mcp_parse_command(const char *json, size_t len, McpCommand *out)
{
    if (len == 0 || json == NULL) return 0;
    memset(out, 0, sizeof(*out));

    const char *v;

    v = mcp_json_find_value(json, "id");
    if (v) mcp_json_extract_string(v, out->id, sizeof(out->id));

    v = mcp_json_find_value(json, "type");
    char type_str[32] = {0};
    if (!v || mcp_json_extract_string(v, type_str, sizeof(type_str)) < 0) {
        out->type = MCP_CMD_UNKNOWN;
        return 0;
    }
    out->type = mcp_command_type_from_string(type_str);
    if (out->type == MCP_CMD_UNKNOWN) return 0;

    char button_str[16] = {0};
    v = mcp_json_find_value(json, "button");
    if (v && mcp_json_extract_string(v, button_str, sizeof(button_str)) >= 0) {
        out->button = mcp_button_from_string(button_str);
    }

    double num;
    v = mcp_json_find_value(json, "duration_ms");
    if (v && mcp_json_extract_number(v, &num) == 0) out->duration_ms = (int)num;

    v = mcp_json_find_value(json, "crank_angle");
    if (v && mcp_json_extract_number(v, &num) == 0) out->crank_angle = (float)num;

    v = mcp_json_find_value(json, "crank_delta");
    if (v && mcp_json_extract_number(v, &num) == 0) out->crank_delta = (float)num;

    int b;
    v = mcp_json_find_value(json, "crank_docked");
    if (v && mcp_json_extract_bool(v, &b) == 0) out->crank_docked = b;

    return 1;
}

int mcp_format_response(const McpResponse *r, char *buf, size_t buflen)
{
    char id_esc[128], error_esc[512], path_esc[512];
    if (mcp_json_escape_string(r->id, id_esc, sizeof(id_esc)) < 0) return -1;
    if (mcp_json_escape_string(r->error, error_esc, sizeof(error_esc)) < 0) return -1;
    if (mcp_json_escape_string(r->path, path_esc, sizeof(path_esc)) < 0) return -1;

    const char *format_str = (r->path[0] == '\0') ? "none" : (r->is_raw_screenshot ? "raw" : "png");

    int n;
    if (r->state[0] == '\0') {
        n = snprintf(buf, buflen,
            "{\"id\":\"%s\",\"status\":\"%s\",\"error\":\"%s\",\"format\":\"%s\","
            "\"path\":\"%s\",\"width\":%d,\"height\":%d,\"row_bytes\":%d,\"state\":null}",
            id_esc, r->ok ? "ok" : "error", error_esc, format_str,
            path_esc, r->width, r->height, r->row_bytes);
    } else {
        n = snprintf(buf, buflen,
            "{\"id\":\"%s\",\"status\":\"%s\",\"error\":\"%s\",\"format\":\"%s\","
            "\"path\":\"%s\",\"width\":%d,\"height\":%d,\"row_bytes\":%d,\"state\":%s}",
            id_esc, r->ok ? "ok" : "error", error_esc, format_str,
            path_esc, r->width, r->height, r->row_bytes, r->state);
    }
    if (n < 0 || (size_t)n >= buflen) return -1;
    return n;
}

static McpOverrideState g_override;
static const char *(*g_state_fn)(void) = NULL;

void mcp_get_button_state(PlaydateAPI *pd, PDButtons *current, PDButtons *pushed, PDButtons *released)
{
    PDButtons rc = 0, rp = 0, rr = 0;
    pd->system->getButtonState(&rc, &rp, &rr);
    PDButtons oc, op, or_;
    mcp_override_get_button_state(&g_override, rc, rp, rr, &oc, &op, &or_);
    if (current) *current = oc;
    if (pushed) *pushed = op;
    if (released) *released = or_;
}

float mcp_get_crank_angle(PlaydateAPI *pd)
{
    return mcp_override_get_crank_angle(&g_override, pd->system->getCrankAngle());
}

float mcp_get_crank_change(PlaydateAPI *pd)
{
    return mcp_override_get_crank_change(&g_override, pd->system->getCrankChange());
}

int mcp_get_crank_docked(PlaydateAPI *pd)
{
    return mcp_override_get_crank_docked(&g_override, pd->system->isCrankDocked());
}

void mcp_harness_init(PlaydateAPI *pd)
{
    mcp_override_init(&g_override);
    pd->file->mkdir("mcp");
}

void mcp_harness_register_state(const char *(*fn)(void))
{
    g_state_fn = fn;
}

void mcp_harness_update(PlaydateAPI *pd)
{
    long now_ms = (long)pd->system->getCurrentTimeMilliseconds();
    mcp_override_expire(&g_override, now_ms);

    FileStat st;
    if (pd->file->stat("mcp/command.json", &st) != 0) {
        return;
    }

    /* kFileRead alone only searches the read-only pdx bundle; our files
       live in the data folder (same place kFileWrite/kFileAppend always
       write to), which needs kFileReadData. */
    SDFile *f = pd->file->open("mcp/command.json", kFileReadData);
    if (!f) return;

    char buf[2048];
    int n = pd->file->read(f, buf, sizeof(buf) - 1);
    pd->file->close(f);
    if (n < 0) {
        pd->file->unlink("mcp/command.json", 0);
        return;
    }
    buf[n] = '\0';

    McpCommand cmd;
    McpResponse resp;
    memset(&resp, 0, sizeof(resp));

    if (!mcp_parse_command(buf, (size_t)n, &cmd)) {
        resp.ok = 0;
        strncpy(resp.error, "failed to parse command", sizeof(resp.error) - 1);
    } else {
        strncpy(resp.id, cmd.id, sizeof(resp.id) - 1);
        switch (cmd.type) {
            case MCP_CMD_PING:
                resp.ok = 1;
                break;
            case MCP_CMD_PRESS:
                mcp_override_apply_press(&g_override, cmd.button, cmd.duration_ms, now_ms);
                resp.ok = 1;
                break;
            case MCP_CMD_RELEASE:
                mcp_override_apply_release(&g_override, cmd.button, cmd.duration_ms, now_ms);
                resp.ok = 1;
                break;
            case MCP_CMD_CRANK:
                mcp_override_apply_crank(&g_override, cmd.crank_angle, cmd.crank_delta, cmd.crank_docked, cmd.duration_ms, now_ms);
                resp.ok = 1;
                break;
            case MCP_CMD_STATE:
                if (g_state_fn) {
                    const char *s = g_state_fn();
                    if (s) strncpy(resp.state, s, sizeof(resp.state) - 1);
                }
                resp.ok = 1;
                break;
            case MCP_CMD_SCREENSHOT: {
                uint8_t *frame = pd->graphics->getDisplayFrame();
                if (frame) {
                    SDFile *out = pd->file->open("mcp/screenshot.raw", kFileWrite);
                    if (out) {
                        pd->file->write(out, frame, LCD_ROWS * LCD_ROWSIZE);
                        pd->file->close(out);
                        resp.ok = 1;
                        resp.is_raw_screenshot = 1;
                        strncpy(resp.path, "mcp/screenshot.raw", sizeof(resp.path) - 1);
                        resp.width = LCD_COLUMNS;
                        resp.height = LCD_ROWS;
                        resp.row_bytes = LCD_ROWSIZE;
                    } else {
                        resp.ok = 0;
                        strncpy(resp.error, "failed to open screenshot file", sizeof(resp.error) - 1);
                    }
                } else {
                    resp.ok = 0;
                    strncpy(resp.error, "getDisplayFrame returned NULL", sizeof(resp.error) - 1);
                }
                break;
            }
            default:
                resp.ok = 0;
                strncpy(resp.error, "unknown command type", sizeof(resp.error) - 1);
                break;
        }
    }

    pd->file->unlink("mcp/command.json", 0);

    char out_buf[4608];
    int len = mcp_format_response(&resp, out_buf, sizeof(out_buf));
    if (len > 0) {
        SDFile *rf = pd->file->open("mcp/response.json", kFileWrite);
        if (rf) {
            pd->file->write(rf, out_buf, (unsigned int)len);
            pd->file->close(rf);
        }
    }
}
