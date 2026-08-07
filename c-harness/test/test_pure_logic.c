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
    const char *json = "{\"id\":\"y\",\"type\":\"crank\",\"crank_angle\":90.5,\"crank_delta\":1.5,\"crank_dock\":\"docked\"}";
    McpCommand cmd;
    int ok = mcp_parse_command(json, strlen(json), &cmd);
    assert(ok == 1);
    assert(cmd.type == MCP_CMD_CRANK);
    assert(cmd.crank_angle > 90.4f && cmd.crank_angle < 90.6f);
    assert(cmd.crank_delta > 1.4f && cmd.crank_delta < 1.6f);
    assert(cmd.crank_docked_set == 1);
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

static void test_override_update_edges_press_produces_pushed_once(void)
{
    McpOverrideState ov;
    mcp_override_init(&ov);
    PDButtons cur, pushed, released;

    /* A tick with no override yet produces nothing. */
    mcp_override_update_edges(&ov, 0);
    mcp_override_get_button_state(&ov, 0, 0, 0, &cur, &pushed, &released);
    assert(pushed == 0 && released == 0);

    mcp_override_apply_press(&ov, kButtonA, 1000, 0);

    /* First tick after the press is applied - synthetic pushed edge. */
    mcp_override_update_edges(&ov, 0);
    mcp_override_get_button_state(&ov, 0, 0, 0, &cur, &pushed, &released);
    assert((pushed & kButtonA) != 0);
    assert((cur & kButtonA) != 0);

    /* Still held next tick - no repeated pushed edge. */
    mcp_override_update_edges(&ov, 0);
    mcp_override_get_button_state(&ov, 0, 0, 0, &cur, &pushed, &released);
    assert((pushed & kButtonA) == 0);
    assert((cur & kButtonA) != 0);
}

static void test_override_update_edges_release_produces_released_once(void)
{
    McpOverrideState ov;
    mcp_override_init(&ov);
    PDButtons cur, pushed, released;

    mcp_override_apply_press(&ov, kButtonA, 1000, 0);
    mcp_override_update_edges(&ov, 0); /* pushed edge consumed here */

    mcp_override_apply_release(&ov, kButtonA, 1000, 100);
    mcp_override_update_edges(&ov, 0);
    mcp_override_get_button_state(&ov, 0, 0, 0, &cur, &pushed, &released);
    assert((released & kButtonA) != 0);
    assert((cur & kButtonA) == 0);

    /* Still released next tick - no repeated edge. */
    mcp_override_update_edges(&ov, 0);
    mcp_override_get_button_state(&ov, 0, 0, 0, &cur, &pushed, &released);
    assert((released & kButtonA) == 0);
}

static void test_override_update_edges_expiry_produces_released(void)
{
    McpOverrideState ov;
    mcp_override_init(&ov);
    PDButtons cur, pushed, released;

    mcp_override_apply_press(&ov, kButtonA, 100, 0);
    mcp_override_update_edges(&ov, 0); /* pushed edge */

    /* Override lapses on its own (no explicit release), real hardware
       isn't actually pressed - should still produce a released edge. */
    mcp_override_expire(&ov, 150);
    mcp_override_update_edges(&ov, 0);
    mcp_override_get_button_state(&ov, 0, 0, 0, &cur, &pushed, &released);
    assert((released & kButtonA) != 0);
    assert((cur & kButtonA) == 0);
}

static void test_override_update_edges_untouched_button_passes_through(void)
{
    McpOverrideState ov;
    mcp_override_init(&ov);
    PDButtons cur, pushed, released;

    mcp_override_apply_press(&ov, kButtonA, 1000, 0); /* only A is overridden */
    mcp_override_update_edges(&ov, 0);

    PDButtons real_pushed = kButtonB; /* B really just pressed, no override on B */
    mcp_override_get_button_state(&ov, 0, real_pushed, 0, &cur, &pushed, &released);
    assert((pushed & kButtonB) != 0); /* passed through untouched */
    assert((pushed & kButtonA) != 0); /* synthetic edge for A still present */
}

static void test_override_crank(void)
{
    McpOverrideState ov;
    mcp_override_init(&ov);
    /* docked_set=1, docked=1: the caller asked for the dock to be forced. */
    mcp_override_apply_crank(&ov, 45.0f, 2.0f, 1, 1, 500, 0);

    assert(mcp_override_get_crank_angle(&ov, 999.0f) == 45.0f);
    assert(mcp_override_get_crank_change(&ov, 999.0f) == 2.0f);
    assert(mcp_override_get_crank_docked(&ov, 0) == 1);

    mcp_override_expire(&ov, 600);
    assert(mcp_override_get_crank_angle(&ov, 999.0f) == 999.0f);
    assert(mcp_override_get_crank_change(&ov, 999.0f) == 999.0f);
    assert(mcp_override_get_crank_docked(&ov, 0) == 0);
}

/* The point of splitting docked_set out: an active crank override that was not
   asked to touch the dock must leave the game's real reading alone, in both
   directions. Angle and delta are still overridden. */
static void test_override_crank_leaves_dock_alone(void)
{
    McpOverrideState ov;
    mcp_override_init(&ov);
    mcp_override_apply_crank(&ov, 45.0f, 2.0f, 0, 0, 500, 0);

    assert(mcp_override_get_crank_angle(&ov, 999.0f) == 45.0f);
    assert(mcp_override_get_crank_change(&ov, 999.0f) == 2.0f);
    /* Real value passes straight through, whichever it is. */
    assert(mcp_override_get_crank_docked(&ov, 0) == 0);
    assert(mcp_override_get_crank_docked(&ov, 1) == 1);
}

/* Forcing undocked has to be distinguishable from not asking. Both leave
   crank_override_docked at 0, so only docked_set separates them - which is
   exactly the bug this replaced: a bool could not say "leave it". */
static void test_override_crank_forces_undocked(void)
{
    McpOverrideState ov;
    mcp_override_init(&ov);
    mcp_override_apply_crank(&ov, 0.0f, 0.0f, 1, 0, 500, 0);

    assert(mcp_override_get_crank_docked(&ov, 1) == 0);
    assert(mcp_override_get_crank_docked(&ov, 0) == 0);
}

/* Expiry has to clear the dock override too, or a crank command with a dock
   would keep forcing it after the rest of the override lapsed. */
static void test_override_crank_expiry_releases_dock(void)
{
    McpOverrideState ov;
    mcp_override_init(&ov);
    mcp_override_apply_crank(&ov, 45.0f, 2.0f, 1, 0, 500, 0);
    assert(mcp_override_get_crank_docked(&ov, 1) == 0);

    mcp_override_expire(&ov, 600);
    assert(mcp_override_get_crank_docked(&ov, 1) == 1);
}

/* A crank set with no duration is the case that omitting duration_ms produces,
   and it used to be the case that quietly did nothing: the override was created
   and destroyed before any frame could read it, because expire runs at the top
   of every frame and now_ms >= now_ms + 0. Held forever now. The clock is walked
   far out to prove it is a sentinel and not just a long deadline. */
static void test_override_crank_zero_duration_holds(void)
{
    McpOverrideState ov;
    mcp_override_init(&ov);
    mcp_override_apply_crank(&ov, 45.0f, 2.0f, 1, 1, 0, 0);

    mcp_override_expire(&ov, 1);
    assert(mcp_override_get_crank_angle(&ov, 999.0f) == 45.0f);

    mcp_override_expire(&ov, 100000000);
    assert(mcp_override_get_crank_angle(&ov, 999.0f) == 45.0f);
    assert(mcp_override_get_crank_change(&ov, 999.0f) == 2.0f);
    assert(mcp_override_get_crank_docked(&ov, 0) == 1);
}

/* A negative duration is the same request as zero. Nothing in the Go layer sends
   one, but the harness parses whatever is on disk, and "-1" reaching a naive
   now_ms + duration_ms would expire in the past. */
static void test_override_crank_negative_duration_holds(void)
{
    McpOverrideState ov;
    mcp_override_init(&ov);
    mcp_override_apply_crank(&ov, 45.0f, 2.0f, 0, 0, -1, 5000);

    mcp_override_expire(&ov, 5001);
    assert(mcp_override_get_crank_angle(&ov, 999.0f) == 45.0f);
}

/* A held crank is not permanent, it is just not on a timer. The next crank
   command replaces it, including one that does have a duration. */
static void test_override_crank_held_is_replaceable(void)
{
    McpOverrideState ov;
    mcp_override_init(&ov);
    mcp_override_apply_crank(&ov, 45.0f, 2.0f, 0, 0, 0, 0);
    mcp_override_apply_crank(&ov, 90.0f, 3.0f, 0, 0, 500, 0);

    assert(mcp_override_get_crank_angle(&ov, 999.0f) == 90.0f);
    mcp_override_expire(&ov, 600);
    assert(mcp_override_get_crank_angle(&ov, 999.0f) == 999.0f);
}

/* Buttons deliberately do not get the crank's treatment. Nothing exposes a
   release, so a button held with no expiry could never be let go - the Go layer
   substitutes a real duration instead. This asserts the asymmetry is on purpose,
   so that anyone copying the crank change down to buttons has to argue with a
   test first. */
static void test_override_press_zero_duration_still_expires(void)
{
    McpOverrideState ov;
    mcp_override_init(&ov);
    mcp_override_apply_press(&ov, kButtonA, 0, 0);

    mcp_override_expire(&ov, 1);
    PDButtons cur, pushed, released;
    mcp_override_get_button_state(&ov, 0, 0, 0, &cur, &pushed, &released);
    assert((cur & kButtonA) == 0);
}

/* crank_dock parsing, all three values plus the absent case. The wire carries a
   string precisely so these are three distinct states rather than two booleans
   with one nonsense combination. */
static void test_parse_crank_dock_modes(void)
{
    McpCommand cmd;

    assert(mcp_parse_command("{\"id\":\"1\",\"type\":\"crank\",\"crank_dock\":\"docked\"}", 47, &cmd) == 1);
    assert(cmd.crank_docked_set == 1);
    assert(cmd.crank_docked == 1);

    assert(mcp_parse_command("{\"id\":\"1\",\"type\":\"crank\",\"crank_dock\":\"undocked\"}", 49, &cmd) == 1);
    assert(cmd.crank_docked_set == 1);
    assert(cmd.crank_docked == 0);

    assert(mcp_parse_command("{\"id\":\"1\",\"type\":\"crank\",\"crank_dock\":\"unchanged\"}", 50, &cmd) == 1);
    assert(cmd.crank_docked_set == 0);

    /* Absent entirely, and an unrecognised value: both leave the dock alone,
       which is what makes a zeroed command safe. */
    assert(mcp_parse_command("{\"id\":\"1\",\"type\":\"crank\"}", 26, &cmd) == 1);
    assert(cmd.crank_docked_set == 0);

    assert(mcp_parse_command("{\"id\":\"1\",\"type\":\"crank\",\"crank_dock\":\"\"}", 41, &cmd) == 1);
    assert(cmd.crank_docked_set == 0);

    assert(mcp_parse_command("{\"id\":\"1\",\"type\":\"crank\",\"crank_dock\":\"sideways\"}", 49, &cmd) == 1);
    assert(cmd.crank_docked_set == 0);
}

/* A full command, the way the Go server actually writes one: every field
   present, including the ones this command type does not use. mcp_json_find_value
   matches on a quoted key, so neighbouring keys that share a prefix must not
   confuse it. */
static void test_parse_full_command_shape(void)
{
    McpCommand cmd;
    const char *json =
        "{\"id\":\"12\",\"type\":\"crank\",\"button\":\"\",\"duration_ms\":250,"
        "\"crank_angle\":123.5,\"crank_delta\":5,\"crank_dock\":\"docked\"}";
    assert(mcp_parse_command(json, strlen(json), &cmd) == 1);
    assert(cmd.type == MCP_CMD_CRANK);
    assert(strcmp(cmd.id, "12") == 0);
    assert(cmd.duration_ms == 250);
    assert(cmd.crank_angle > 123.4f && cmd.crank_angle < 123.6f);
    assert(cmd.crank_delta > 4.9f && cmd.crank_delta < 5.1f);
    assert(cmd.crank_docked_set == 1);
    assert(cmd.crank_docked == 1);
    /* An empty button name maps to no button, same as absent. */
    assert(cmd.button == 0);
}

int main(void)
{
    test_parse_ping();
    test_parse_press();
    test_parse_crank();
    test_parse_crank_dock_modes();
    test_parse_full_command_shape();
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
    test_override_update_edges_press_produces_pushed_once();
    test_override_update_edges_release_produces_released_once();
    test_override_update_edges_expiry_produces_released();
    test_override_update_edges_untouched_button_passes_through();
    test_override_crank();
    test_override_crank_leaves_dock_alone();
    test_override_crank_forces_undocked();
    test_override_crank_expiry_releases_dock();
    test_override_crank_zero_duration_holds();
    test_override_crank_negative_duration_holds();
    test_override_crank_held_is_replaceable();
    test_override_press_zero_duration_still_expires();

    printf("pure logic: all tests passed\n");
    return 0;
}
