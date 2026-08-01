#include <assert.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>
#include "mcp_harness.h"

static char g_fake_root[512];

static void fake_path(const char *name, char *out, size_t out_len)
{
    snprintf(out, out_len, "%s/%s", g_fake_root, name);
}

static PDButtons g_fake_real_current = 0;
static float g_fake_crank_change = 0.0f;
static float g_fake_crank_angle = 0.0f;
static int g_fake_crank_docked = 0;
static unsigned int g_fake_now_ms = 0;
static uint8_t g_fake_frame[LCD_ROWS * LCD_ROWSIZE];

static void fake_getButtonState(PDButtons *current, PDButtons *pushed, PDButtons *released)
{
    if (current) *current = g_fake_real_current;
    if (pushed) *pushed = 0;
    if (released) *released = 0;
}

static float fake_getCrankChange(void) { return g_fake_crank_change; }
static float fake_getCrankAngle(void) { return g_fake_crank_angle; }
static int fake_isCrankDocked(void) { return g_fake_crank_docked; }
static unsigned int fake_getCurrentTimeMilliseconds(void) { return g_fake_now_ms; }
static uint8_t *fake_getDisplayFrame(void) { return g_fake_frame; }

static int fake_stat(const char *path, FileStat *out_stat)
{
    char full[600];
    fake_path(path, full, sizeof(full));
    struct stat st;
    if (stat(full, &st) != 0) return -1;
    memset(out_stat, 0, sizeof(*out_stat));
    out_stat->isdir = S_ISDIR(st.st_mode) ? 1 : 0;
    out_stat->size = (unsigned int)st.st_size;
    return 0;
}

static SDFile *fake_open(const char *name, FileOptions mode)
{
    char full[600];
    fake_path(name, full, sizeof(full));
    const char *m = (mode & kFileWrite) ? "wb" : (mode & kFileAppend) ? "ab" : "rb";
    return (SDFile *)fopen(full, m);
}

static int fake_close(SDFile *file) { return fclose((FILE *)file); }

static int fake_read(SDFile *file, void *buf, unsigned int len)
{
    return (int)fread(buf, 1, len, (FILE *)file);
}

static int fake_write(SDFile *file, const void *buf, unsigned int len)
{
    return (int)fwrite(buf, 1, len, (FILE *)file);
}

static int fake_unlink(const char *name, int recursive)
{
    (void)recursive;
    char full[600];
    fake_path(name, full, sizeof(full));
    return remove(full);
}

static int fake_mkdir(const char *path)
{
    char full[600];
    fake_path(path, full, sizeof(full));
    return mkdir(full, 0755);
}

/* Matches the real API's contract: 0 on success. The harness publishes its
   response by renaming a temp file into place, so without this the response would
   only ever arrive through the fallback path and these tests would not be
   exercising what a real game does. */
static int fake_rename(const char *from, const char *to)
{
    char full_from[600], full_to[600];
    fake_path(from, full_from, sizeof(full_from));
    fake_path(to, full_to, sizeof(full_to));
    return rename(full_from, full_to);
}

static struct playdate_sys fake_sys = {
    .getButtonState = fake_getButtonState,
    .getCrankChange = fake_getCrankChange,
    .getCrankAngle = fake_getCrankAngle,
    .isCrankDocked = fake_isCrankDocked,
    .getCurrentTimeMilliseconds = fake_getCurrentTimeMilliseconds,
};

static struct playdate_file fake_file = {
    .stat = fake_stat,
    .open = fake_open,
    .close = fake_close,
    .read = fake_read,
    .write = fake_write,
    .unlink = fake_unlink,
    .mkdir = fake_mkdir,
    .rename = fake_rename,
};

static struct playdate_graphics fake_gfx = {
    .getDisplayFrame = fake_getDisplayFrame,
};

static PlaydateAPI fake_pd = {
    .system = &fake_sys,
    .file = &fake_file,
    .graphics = &fake_gfx,
};

static void write_file(const char *relpath, const char *content)
{
    char full[600];
    fake_path(relpath, full, sizeof(full));
    FILE *f = fopen(full, "wb");
    assert(f != NULL);
    fwrite(content, 1, strlen(content), f);
    fclose(f);
}

static int read_file(const char *relpath, char *buf, size_t buflen)
{
    char full[600];
    fake_path(relpath, full, sizeof(full));
    FILE *f = fopen(full, "rb");
    if (!f) return -1;
    size_t n = fread(buf, 1, buflen - 1, f);
    fclose(f);
    buf[n] = '\0';
    return (int)n;
}

static int file_exists(const char *relpath)
{
    char full[600];
    fake_path(relpath, full, sizeof(full));
    struct stat st;
    return stat(full, &st) == 0;
}

static void test_ping_roundtrip(void)
{
    write_file("mcp/command.json", "{\"id\":\"req1\",\"type\":\"ping\"}");
    mcp_harness_update(&fake_pd);

    assert(!file_exists("mcp/command.json"));
    char resp[1024];
    int n = read_file("mcp/response.json", resp, sizeof(resp));
    assert(n > 0);
    assert(strstr(resp, "\"id\":\"req1\"") != NULL);
    assert(strstr(resp, "\"status\":\"ok\"") != NULL);
}

/* The response is published by renaming a temp file into place, so the temp path
   must not be left behind. A lingering mcp/response.tmp.json would mean the rename
   silently failed and the fallback wrote the response directly - which still works,
   but loses the guarantee that a response on disk is complete. */
static void test_response_published_by_rename_leaves_no_temp(void)
{
    write_file("mcp/command.json", "{\"id\":\"req1t\",\"type\":\"ping\"}");
    mcp_harness_update(&fake_pd);

    char resp[512];
    int n = read_file("mcp/response.json", resp, sizeof(resp));
    assert(n > 0);
    assert(strstr(resp, "\"id\":\"req1t\"") != NULL);

    char leftover[64];
    assert(read_file("mcp/response.tmp.json", leftover, sizeof(leftover)) < 0);
}

static void test_press_overrides_real_button_state(void)
{
    g_fake_now_ms = 1000;
    g_fake_real_current = 0;
    write_file("mcp/command.json", "{\"id\":\"req2\",\"type\":\"press\",\"button\":\"a\",\"duration_ms\":100000}");
    mcp_harness_update(&fake_pd);

    PDButtons current = 0, pushed = 0, released = 0;
    mcp_get_button_state(&fake_pd, &current, &pushed, &released);
    assert((current & kButtonA) != 0);
}

static void test_release_forces_not_pressed(void)
{
    g_fake_now_ms = 2000;
    g_fake_real_current = kButtonB;
    write_file("mcp/command.json", "{\"id\":\"req3\",\"type\":\"release\",\"button\":\"b\",\"duration_ms\":100000}");
    mcp_harness_update(&fake_pd);

    PDButtons current = 0, pushed = 0, released = 0;
    mcp_get_button_state(&fake_pd, &current, &pushed, &released);
    assert((current & kButtonB) == 0);
}

static void test_crank_override(void)
{
    g_fake_now_ms = 3000;
    write_file("mcp/command.json", "{\"id\":\"req4\",\"type\":\"crank\",\"crank_angle\":123.0,\"crank_delta\":5.0,\"crank_dock\":\"docked\",\"duration_ms\":100000}");
    mcp_harness_update(&fake_pd);

    assert(mcp_get_crank_angle(&fake_pd) == 123.0f);
    assert(mcp_get_crank_change(&fake_pd) == 5.0f);
    assert(mcp_get_crank_docked(&fake_pd) == 1);
}

/* The same command without a dock mode, end to end through the harness: angle and
   delta are overridden, the dock reading is not.
   
   The fake is set to report *docked* first, deliberately. The old bool-shaped
   protocol would have resolved a missing dock to false and forced undocked, so
   asserting against a real value of 1 is what distinguishes "passed the real
   reading through" from "happened to agree with the default". Against a fake that
   reports 0 this test would pass either way. */
static void test_crank_override_leaves_dock_alone(void)
{
    g_fake_now_ms = 3000;
    g_fake_crank_docked = 1;
    write_file("mcp/command.json", "{\"id\":\"req4b\",\"type\":\"crank\",\"crank_angle\":45.0,\"crank_delta\":1.0,\"crank_dock\":\"unchanged\",\"duration_ms\":100000}");
    mcp_harness_update(&fake_pd);

    assert(mcp_get_crank_angle(&fake_pd) == 45.0f);
    assert(mcp_get_crank_change(&fake_pd) == 1.0f);
    assert(mcp_get_crank_docked(&fake_pd) == 1);

    /* And forcing undocked still wins over a real docked reading, so the
       passthrough above is a choice rather than an inability to override. */
    write_file("mcp/command.json", "{\"id\":\"req4c\",\"type\":\"crank\",\"crank_angle\":45.0,\"crank_delta\":1.0,\"crank_dock\":\"undocked\",\"duration_ms\":100000}");
    mcp_harness_update(&fake_pd);
    assert(mcp_get_crank_docked(&fake_pd) == 0);

    g_fake_crank_docked = 0;
}

static void test_screenshot(void)
{
    for (int i = 0; i < LCD_ROWS * LCD_ROWSIZE; i++) {
        g_fake_frame[i] = (uint8_t)(i & 0xFF);
    }
    write_file("mcp/command.json", "{\"id\":\"req5\",\"type\":\"screenshot\"}");
    mcp_harness_update(&fake_pd);

    char resp[1024];
    int n = read_file("mcp/response.json", resp, sizeof(resp));
    assert(n > 0);
    assert(strstr(resp, "\"format\":\"raw\"") != NULL);
    assert(strstr(resp, "\"width\":400") != NULL);
    assert(strstr(resp, "\"height\":240") != NULL);
    assert(strstr(resp, "\"row_bytes\":52") != NULL);

    char full[600];
    fake_path("mcp/screenshot.raw", full, sizeof(full));
    FILE *f = fopen(full, "rb");
    assert(f != NULL);
    uint8_t got[LCD_ROWS * LCD_ROWSIZE];
    size_t got_n = fread(got, 1, sizeof(got), f);
    fclose(f);
    assert(got_n == (size_t)(LCD_ROWS * LCD_ROWSIZE));
    assert(memcmp(got, g_fake_frame, sizeof(got)) == 0);
}

static const char *fake_state_callback(void)
{
    return "{\"score\":7}";
}

static void test_state_callback(void)
{
    mcp_harness_register_state(fake_state_callback);
    write_file("mcp/command.json", "{\"id\":\"req6\",\"type\":\"state\"}");
    mcp_harness_update(&fake_pd);

    char resp[1024];
    int n = read_file("mcp/response.json", resp, sizeof(resp));
    assert(n > 0);
    assert(strstr(resp, "\"state\":{\"score\":7}") != NULL);
}

static void test_malformed_command_reports_error_and_cleans_up(void)
{
    write_file("mcp/command.json", "not json at all");
    mcp_harness_update(&fake_pd);

    assert(!file_exists("mcp/command.json"));
    char resp[1024];
    int n = read_file("mcp/response.json", resp, sizeof(resp));
    assert(n > 0);
    assert(strstr(resp, "\"status\":\"error\"") != NULL);
}

static void test_no_pending_command_is_a_noop(void)
{
    g_fake_now_ms = 4000;
    mcp_harness_update(&fake_pd);
    assert(!file_exists("mcp/command.json"));
}

int main(void)
{
    char tmpl[] = "/tmp/mcp_harness_test_XXXXXX";
    char *dir = mkdtemp(tmpl);
    assert(dir != NULL);
    strncpy(g_fake_root, dir, sizeof(g_fake_root) - 1);

    mcp_harness_init(&fake_pd);

    test_ping_roundtrip();
    test_response_published_by_rename_leaves_no_temp();
    test_press_overrides_real_button_state();
    test_release_forces_not_pressed();
    test_crank_override();
    test_crank_override_leaves_dock_alone();
    test_screenshot();
    test_state_callback();
    test_malformed_command_reports_error_and_cleans_up();
    test_no_pending_command_is_a_noop();

    char path[700];
    fake_path("mcp/response.json", path, sizeof(path));
    remove(path);
    fake_path("mcp/screenshot.raw", path, sizeof(path));
    remove(path);
    fake_path("mcp", path, sizeof(path));
    rmdir(path);
    rmdir(g_fake_root);

    printf("fake-PlaydateAPI integration: all tests passed\n");
    return 0;
}
