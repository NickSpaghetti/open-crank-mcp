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

void mcp_override_apply_crank(McpOverrideState *ov, float angle, float delta,
                              int docked_set, int docked, int duration_ms, long now_ms)
{
    ov->crank_override_active = 1;
    ov->crank_override_angle = angle;
    ov->crank_override_delta = delta;
    ov->crank_override_docked_active = docked_set;
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
        ov->crank_override_docked_active = 0;
    }
}

void mcp_override_update_edges(McpOverrideState *ov, PDButtons real_current)
{
    ov->pending_pushed = 0;
    ov->pending_released = 0;
    for (int i = 0; i < 6; i++) {
        int active_now = ov->button_override_active[i];
        int effective = active_now ? ov->button_override_value[i] : ((real_current & kButtonBits[i]) != 0);

        if (active_now || ov->override_was_active_last_frame[i]) {
            if (effective && !ov->last_effective_pressed[i]) {
                ov->pending_pushed |= kButtonBits[i];
            } else if (!effective && ov->last_effective_pressed[i]) {
                ov->pending_released |= kButtonBits[i];
            }
        }
        ov->last_effective_pressed[i] = effective;
        ov->override_was_active_last_frame[i] = active_now;
    }
}

void mcp_override_get_button_state(const McpOverrideState *ov,
                                    PDButtons real_current, PDButtons real_pushed, PDButtons real_released,
                                    PDButtons *out_current, PDButtons *out_pushed, PDButtons *out_released)
{
    PDButtons current = real_current;
    PDButtons pushed = real_pushed;
    PDButtons released = real_released;
    for (int i = 0; i < 6; i++) {
        if (ov->button_override_active[i]) {
            if (ov->button_override_value[i]) {
                current |= kButtonBits[i];
            } else {
                current &= ~kButtonBits[i];
            }
        }
        /* An overridden (or just-was-overridden) button's real
           pushed/released bit is meaningless - no real hardware edge
           caused it - so use the synthetic edge from
           mcp_override_update_edges instead. Untouched buttons pass
           their real bits through unchanged. */
        if (ov->button_override_active[i] || ov->override_was_active_last_frame[i]) {
            pushed &= ~kButtonBits[i];
            released &= ~kButtonBits[i];
        }
    }
    pushed |= ov->pending_pushed;
    released |= ov->pending_released;

    *out_current = current;
    *out_pushed = pushed;
    *out_released = released;
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
    /* Both flags: an active crank override that was not asked to touch the dock
       must pass the real reading through. */
    return (ov->crank_override_active && ov->crank_override_docked_active)
        ? ov->crank_override_docked : real_docked;
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

/* There is deliberately no mcp_json_extract_bool. There was one, for
   crank_docked, and it went with that field: the command protocol now has no
   boolean values at all, because the one place that wanted a bool needed three
   states (see crank_dock). Under -Wall -Wextra -Werror an unused static function
   is a build failure, so leaving it "for later" was not an option, and re-adding
   it is a few lines if a genuinely two-state field ever turns up. */

static McpCommandType mcp_command_type_from_string(const char *s)
{
    if (strcmp(s, "screenshot") == 0) return MCP_CMD_SCREENSHOT;
    if (strcmp(s, "press") == 0)      return MCP_CMD_PRESS;
    if (strcmp(s, "release") == 0)    return MCP_CMD_RELEASE;
    if (strcmp(s, "crank") == 0)      return MCP_CMD_CRANK;
    if (strcmp(s, "state") == 0)      return MCP_CMD_STATE;
    if (strcmp(s, "ping") == 0)       return MCP_CMD_PING;
    if (strcmp(s, "entities") == 0)   return MCP_CMD_ENTITIES;
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

    /* crank_dock is a three-valued string, not a bool: "docked"/"undocked" ask
       for an override, anything else (including absent or "unchanged") leaves the
       game's real dock reading alone. memset above already set both ints to 0,
       which is the leave-alone case, so an unrecognised value needs no branch. */
    char dock_str[16] = {0};
    v = mcp_json_find_value(json, "crank_dock");
    if (v && mcp_json_extract_string(v, dock_str, sizeof(dock_str)) >= 0) {
        if (strcmp(dock_str, "docked") == 0) {
            out->crank_docked_set = 1;
            out->crank_docked = 1;
        } else if (strcmp(dock_str, "undocked") == 0) {
            out->crank_docked_set = 1;
            out->crank_docked = 0;
        }
    }

    return 1;
}

int mcp_format_response(const McpResponse *r, char *buf, size_t buflen)
{
    char id_esc[128], error_esc[512], path_esc[512];
    if (mcp_json_escape_string(r->id, id_esc, sizeof(id_esc)) < 0) return -1;
    if (mcp_json_escape_string(r->error, error_esc, sizeof(error_esc)) < 0) return -1;
    if (mcp_json_escape_string(r->path, path_esc, sizeof(path_esc)) < 0) return -1;

    const char *format_str = (r->path[0] == '\0') ? "none" : (r->is_raw_screenshot ? "raw" : "png");
    const char *state_json = (r->state[0] == '\0') ? "null" : r->state;
    const char *entities_json = (r->entities[0] == '\0') ? "null" : r->entities;

    /* harness_version comes from the #define rather than from McpResponse: it is
       the same for every response this build ever writes, so putting it in the
       struct would grow the fat struct to carry a constant. */
    int n = snprintf(buf, buflen,
        "{\"id\":\"%s\",\"status\":\"%s\",\"error\":\"%s\",\"format\":\"%s\","
        "\"path\":\"%s\",\"width\":%d,\"height\":%d,\"row_bytes\":%d,\"state\":%s,"
        "\"entities\":%s,\"entities_complete\":%s,\"harness_version\":\"%s\"}",
        id_esc, r->ok ? "ok" : "error", error_esc, format_str,
        path_esc, r->width, r->height, r->row_bytes, state_json,
        entities_json, r->entities_complete ? "true" : "false",
        MCP_HARNESS_VERSION);
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

int mcp_build_entities_json(PlaydateAPI *pd, char *out, size_t out_len)
{
    int count = 0;
    LCDSprite **sprites = pd->sprite->querySpritesInRect(0, 0, LCD_COLUMNS, LCD_ROWS, &count);

    size_t o = 0;
    if (out_len < 2) return -1;
    out[o++] = '[';

    for (int i = 0; i < count; i++) {
        LCDSprite *s = sprites[i];
        PDRect bounds = pd->sprite->getBounds(s);
        uint8_t tag = pd->sprite->getTag(s);
        int16_t z = pd->sprite->getZIndex(s);
        int visible = pd->sprite->isVisible(s);

        char entry[192];
        int n = snprintf(entry, sizeof(entry),
            "%s{\"tag\":%d,\"class_name\":\"\",\"x\":%.2f,\"y\":%.2f,"
            "\"width\":%.2f,\"height\":%.2f,\"z_index\":%d,\"visible\":%s}",
            (i == 0) ? "" : ",", (int)tag, (double)bounds.x, (double)bounds.y,
            (double)bounds.width, (double)bounds.height, (int)z,
            visible ? "true" : "false");
        if (n < 0 || (size_t)n >= sizeof(entry)) return -1;
        if (o + (size_t)n >= out_len - 1) return -1;
        memcpy(out + o, entry, (size_t)n);
        o += (size_t)n;
    }

    out[o++] = ']';
    out[o] = '\0';
    return (int)o;
}

void mcp_harness_update(PlaydateAPI *pd)
{
    long now_ms = (long)pd->system->getCurrentTimeMilliseconds();
    mcp_override_expire(&g_override, now_ms);

    PDButtons real_current;
    pd->system->getButtonState(&real_current, NULL, NULL);
    mcp_override_update_edges(&g_override, real_current);

    FileStat st;
    if (pd->file->stat(MCP_COMMAND_PATH, &st) != 0) {
        return;
    }

    /* kFileRead alone only searches the read-only pdx bundle; our files
       live in the data folder (same place kFileWrite/kFileAppend always
       write to), which needs kFileReadData. */
    SDFile *f = pd->file->open(MCP_COMMAND_PATH, kFileReadData);
    if (!f) return;

    char buf[2048];
    int n = pd->file->read(f, buf, sizeof(buf) - 1);
    pd->file->close(f);
    if (n < 0) {
        pd->file->unlink(MCP_COMMAND_PATH, 0);
        return;
    }
    buf[n] = '\0';

    /* static, not automatic. McpResponse is ~12.9KB (state[4096] +
       entities[8192] + the fixed fields) and out_buf below is another 13KB, so
       as locals they put ~25.6KB of automatic storage in a function the game
       calls every frame. Nothing is given up by making them static: the harness
       is already single-instance and single-threaded (see g_override above),
       mcp_harness_update is never reentrant, and neither buffer outlives the
       call. This is where the fat-struct trade actually costs something, and
       moving the storage is what keeps the payoff without the cost. No
       measurable effect in the Simulator, which is the only supported target -
       it matters for anyone who builds a harnessed game for the device, where
       the stack is not this generous. */
    McpCommand cmd;
    static McpResponse resp;
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
                mcp_override_apply_crank(&g_override, cmd.crank_angle, cmd.crank_delta,
                                         cmd.crank_docked_set, cmd.crank_docked, cmd.duration_ms, now_ms);
                resp.ok = 1;
                break;
            case MCP_CMD_STATE:
                if (g_state_fn) {
                    const char *s = g_state_fn();
                    if (s) strncpy(resp.state, s, sizeof(resp.state) - 1);
                }
                resp.ok = 1;
                break;
            case MCP_CMD_ENTITIES: {
                int n_entities = mcp_build_entities_json(pd, resp.entities, sizeof(resp.entities));
                if (n_entities < 0) {
                    resp.ok = 0;
                    strncpy(resp.error, "entities list did not fit", sizeof(resp.error) - 1);
                } else {
                    resp.ok = 1;
                }
                /* Always false: querySpritesInRect only matches sprites
                   with a collide rect set, so this is never a complete
                   list the way Lua's getAllSprites() is. */
                resp.entities_complete = 0;
                break;
            }
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
            /* Unreachable in practice, and kept anyway: mcp_parse_command
               returns 0 for a type it does not recognise, so an unknown type
               never gets here - it is answered above with "failed to parse
               command" instead. Verified on the wire against a real Simulator,
               which is also the divergence to know about if you are comparing
               the two harnesses: Lua reports "unknown command type" for the same
               input, because its dispatch is a chain of string comparisons with
               a real else branch. Left in place because it is the correct answer
               for a McpCommandType that exists but has no arm here, which is
               what a future command type looks like on the day it is added to
               the enum and not to this switch. */
            default:
                resp.ok = 0;
                strncpy(resp.error, "unknown command type", sizeof(resp.error) - 1);
                break;
        }
    }

    pd->file->unlink(MCP_COMMAND_PATH, 0);

    /* static for the same reason as resp above. */
    static char out_buf[13312]; /* room for the fixed fields plus state[4096] and entities[8192] */
    int len = mcp_format_response(&resp, out_buf, sizeof(out_buf));
    if (len > 0) {
        /* Published by rename, so response.json only ever exists complete. Written
           in place, a reader can catch it just after the truncating open (zero
           length) or part-written, with no way to tell short from finished.

           Not a bug anyone hit - 240 calls against a 512KB response on
           Linux/overlayfs found no partial reads - but it costs one rename, the Go
           reader's handling of a short read used to be actively destructive, and
           native mode will run on filesystems nobody has measured.

           The useful side effect: the response becomes a commit point, so the
           screenshot written earlier in this same dispatch is guaranteed finished
           before response.json exists, by construction rather than by the order of
           statements above. */
        SDFile *rf = pd->file->open(MCP_RESPONSE_TMP_PATH, kFileWrite);
        if (rf) {
            pd->file->write(rf, out_buf, (unsigned int)len);
            pd->file->close(rf);
            if (pd->file->rename(MCP_RESPONSE_TMP_PATH, MCP_RESPONSE_PATH) != 0) {
                /* A response that might be read short beats no response: the Go
                   side waits out a short read rather than failing on it. */
                SDFile *direct = pd->file->open(MCP_RESPONSE_PATH, kFileWrite);
                if (direct) {
                    pd->file->write(direct, out_buf, (unsigned int)len);
                    pd->file->close(direct);
                }
            }
        }
    }
}
