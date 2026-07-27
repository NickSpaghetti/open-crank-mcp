#include <assert.h>
#include <string.h>
#include <stdio.h>
#include "mcp_harness.h"

static void test_parse_ping(void)
{
    McpCommand cmd;
    int ok = mcp_parse_command("{\"id\":\"abc\",\"type\":\"ping\"}", 26, &cmd);
    assert(ok == 1);
    assert(cmd.type == MCP_CMD_PING);
    assert(strcmp(cmd.id, "abc") == 0);
}

static void test_parse_press(void)
{
    const char *json = "{\"id\":\"x\",\"type\":\"press\",\"button\":\"a\",\"duration_ms\":200}";
    McpCommand cmd;
    int ok = mcp_parse_command(json, strlen(json), &cmd);
    assert(ok == 1);
    assert(cmd.type == MCP_CMD_PRESS);
    assert(cmd.button == kButtonA);
    assert(cmd.duration_ms == 200);
}

static void test_parse_crank(void)
{
    const char *json = "{\"id\":\"y\",\"type\":\"crank\",\"crank_angle\":90.5,\"crank_delta\":1.5,\"crank_docked\":true}";
    McpCommand cmd;
    int ok = mcp_parse_command(json, strlen(json), &cmd);
    assert(ok == 1);
    assert(cmd.type == MCP_CMD_CRANK);
    assert(cmd.crank_angle > 90.4f && cmd.crank_angle < 90.6f);
    assert(cmd.crank_delta > 1.4f && cmd.crank_delta < 1.6f);
    assert(cmd.crank_docked == 1);
}

static void test_parse_missing_type_fails(void)
{
    const char *json = "{\"id\":\"x\"}";
    McpCommand cmd;
    int ok = mcp_parse_command(json, strlen(json), &cmd);
    assert(ok == 0);
}

static void test_parse_unknown_type_fails(void)
{
    const char *json = "{\"id\":\"x\",\"type\":\"bogus\"}";
    McpCommand cmd;
    int ok = mcp_parse_command(json, strlen(json), &cmd);
    assert(ok == 0);
}

static void test_parse_empty_fails(void)
{
    McpCommand cmd;
    int ok = mcp_parse_command("", 0, &cmd);
    assert(ok == 0);
}

static void test_parse_truncated_fails(void)
{
    const char *json = "{\"id\":\"x\",\"type\":";
    McpCommand cmd;
    int ok = mcp_parse_command(json, strlen(json), &cmd);
    assert(ok == 0);
}

static void test_parse_negative_duration(void)
{
    const char *json = "{\"id\":\"x\",\"type\":\"press\",\"button\":\"a\",\"duration_ms\":-50}";
    McpCommand cmd;
    int ok = mcp_parse_command(json, strlen(json), &cmd);
    assert(ok == 1);
    assert(cmd.duration_ms == -50);
}

static void test_parse_overlong_id_truncates_safely(void)
{
    char json[512];
    char long_id[300];
    memset(long_id, 'x', sizeof(long_id) - 1);
    long_id[sizeof(long_id) - 1] = '\0';
    snprintf(json, sizeof(json), "{\"id\":\"%s\",\"type\":\"ping\"}", long_id);
    McpCommand cmd;
    int ok = mcp_parse_command(json, strlen(json), &cmd);
    assert(ok == 1);
    assert(strlen(cmd.id) < sizeof(cmd.id));
}

static void test_format_response_basic(void)
{
    McpResponse r;
    memset(&r, 0, sizeof(r));
    strcpy(r.id, "abc");
    r.ok = 1;
    char buf[512];
    int n = mcp_format_response(&r, buf, sizeof(buf));
    assert(n > 0);
    assert(strstr(buf, "\"id\":\"abc\"") != NULL);
    assert(strstr(buf, "\"status\":\"ok\"") != NULL);
    assert(strstr(buf, "\"format\":\"none\"") != NULL);
    assert(strstr(buf, "\"state\":null") != NULL);
}

static void test_format_response_with_state(void)
{
    McpResponse r;
    memset(&r, 0, sizeof(r));
    strcpy(r.id, "abc");
    r.ok = 1;
    strcpy(r.state, "{\"score\":42}");
    char buf[512];
    int n = mcp_format_response(&r, buf, sizeof(buf));
    assert(n > 0);
    assert(strstr(buf, "\"state\":{\"score\":42}") != NULL);
}

static void test_format_response_screenshot(void)
{
    McpResponse r;
    memset(&r, 0, sizeof(r));
    strcpy(r.id, "abc");
    r.ok = 1;
    r.is_raw_screenshot = 1;
    strcpy(r.path, "mcp/screenshot.raw");
    r.width = 400;
    r.height = 240;
    r.row_bytes = 52;
    char buf[512];
    int n = mcp_format_response(&r, buf, sizeof(buf));
    assert(n > 0);
    assert(strstr(buf, "\"format\":\"raw\"") != NULL);
    assert(strstr(buf, "\"width\":400") != NULL);
}

static void test_format_response_escapes_error(void)
{
    McpResponse r;
    memset(&r, 0, sizeof(r));
    strcpy(r.id, "abc");
    r.ok = 0;
    strcpy(r.error, "bad \"quote\" and \\backslash\\");
    char buf[512];
    int n = mcp_format_response(&r, buf, sizeof(buf));
    assert(n > 0);
    assert(strstr(buf, "\\\"quote\\\"") != NULL);
    assert(strstr(buf, "\\\\backslash\\\\") != NULL);
}

static void test_format_response_buffer_too_small_fails(void)
{
    McpResponse r;
    memset(&r, 0, sizeof(r));
    strcpy(r.id, "abc");
    r.ok = 1;
    char buf[8];
    int n = mcp_format_response(&r, buf, sizeof(buf));
    assert(n == -1);
}

static void test_json_escape_string(void)
{
    char out[64];
    int n = mcp_json_escape_string("plain", out, sizeof(out));
    assert(n == 5);
    assert(strcmp(out, "plain") == 0);

    n = mcp_json_escape_string("a\"b\\c\nd", out, sizeof(out));
    assert(n > 0);
    assert(strcmp(out, "a\\\"b\\\\c\\nd") == 0);

    char tiny[2];
    n = mcp_json_escape_string("too long", tiny, sizeof(tiny));
    assert(n == -1);
}

static void test_button_index(void)
{
    assert(mcp_button_index(kButtonLeft) == 0);
    assert(mcp_button_index(kButtonRight) == 1);
    assert(mcp_button_index(kButtonUp) == 2);
    assert(mcp_button_index(kButtonDown) == 3);
    assert(mcp_button_index(kButtonB) == 4);
    assert(mcp_button_index(kButtonA) == 5);
    assert(mcp_button_index(0) == -1);
    assert(mcp_button_index((PDButtons)(kButtonA | kButtonB)) == -1);
}

static void test_override_press_and_expiry(void)
{
    McpOverrideState ov;
    mcp_override_init(&ov);
    mcp_override_apply_press(&ov, kButtonA, 100, 1000);

    PDButtons cur, pushed, released;
    mcp_override_get_button_state(&ov, 0, 0, 0, &cur, &pushed, &released);
    assert((cur & kButtonA) != 0);

    mcp_override_expire(&ov, 1050);
    mcp_override_get_button_state(&ov, 0, 0, 0, &cur, &pushed, &released);
    assert((cur & kButtonA) != 0);

    mcp_override_expire(&ov, 1101);
    mcp_override_get_button_state(&ov, 0, 0, 0, &cur, &pushed, &released);
    assert((cur & kButtonA) == 0);
}

static void test_override_release_forces_not_pressed(void)
{
    McpOverrideState ov;
    mcp_override_init(&ov);
    mcp_override_apply_press(&ov, kButtonA, 100000, 0);
    mcp_override_apply_release(&ov, kButtonA, 100, 0);

    PDButtons cur, pushed, released;
    PDButtons real_current = kButtonA; /* even if "really" held, override forces it off */
    mcp_override_get_button_state(&ov, real_current, 0, 0, &cur, &pushed, &released);
    assert((cur & kButtonA) == 0);

    mcp_override_expire(&ov, 200);
    mcp_override_get_button_state(&ov, real_current, 0, 0, &cur, &pushed, &released);
    assert((cur & kButtonA) != 0); /* after expiry, reverts to real (held) state */
}

static void test_override_masks_real_bit_off(void)
{
    McpOverrideState ov;
    mcp_override_init(&ov);
    mcp_override_apply_press(&ov, kButtonA, 100000, 0);
    mcp_override_apply_release(&ov, kButtonB, 100000, 0);

    PDButtons cur, pushed, released;
    PDButtons real_current = kButtonB;
    mcp_override_get_button_state(&ov, real_current, 0, 0, &cur, &pushed, &released);
    assert((cur & kButtonA) != 0);
    assert((cur & kButtonB) == 0);
}

static void test_override_crank(void)
{
    McpOverrideState ov;
    mcp_override_init(&ov);
    mcp_override_apply_crank(&ov, 45.0f, 2.0f, 1, 500, 0);

    assert(mcp_override_get_crank_angle(&ov, 999.0f) == 45.0f);
    assert(mcp_override_get_crank_change(&ov, 999.0f) == 2.0f);
    assert(mcp_override_get_crank_docked(&ov, 0) == 1);

    mcp_override_expire(&ov, 600);
    assert(mcp_override_get_crank_angle(&ov, 999.0f) == 999.0f);
    assert(mcp_override_get_crank_change(&ov, 999.0f) == 999.0f);
    assert(mcp_override_get_crank_docked(&ov, 0) == 0);
}

int main(void)
{
    test_parse_ping();
    test_parse_press();
    test_parse_crank();
    test_parse_missing_type_fails();
    test_parse_unknown_type_fails();
    test_parse_empty_fails();
    test_parse_truncated_fails();
    test_parse_negative_duration();
    test_parse_overlong_id_truncates_safely();

    test_format_response_basic();
    test_format_response_with_state();
    test_format_response_screenshot();
    test_format_response_escapes_error();
    test_format_response_buffer_too_small_fails();

    test_json_escape_string();
    test_button_index();

    test_override_press_and_expiry();
    test_override_release_forces_not_pressed();
    test_override_masks_real_bit_off();
    test_override_crank();

    printf("pure logic: all tests passed\n");
    return 0;
}
