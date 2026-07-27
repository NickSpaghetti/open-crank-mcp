#include <stdio.h>
#include "pd_api.h"
#include "mcp_harness.h"

static PlaydateAPI *g_pd;
static char g_state_buf[256];

static const char *report_state(void)
{
    PDButtons current, pushed, released;
    mcp_get_button_state(g_pd, &current, &pushed, &released);
    float angle = mcp_get_crank_angle(g_pd);
    float change = mcp_get_crank_change(g_pd);
    int docked = mcp_get_crank_docked(g_pd);
    snprintf(g_state_buf, sizeof(g_state_buf),
        "{\"current\":%d,\"crank_angle\":%.2f,\"crank_change\":%.2f,\"crank_docked\":%s}",
        (int)current, (double)angle, (double)change, docked ? "true" : "false");
    return g_state_buf;
}

static int update(void *userdata)
{
    (void)userdata;
    mcp_harness_update(g_pd);
    return 1;
}

int eventHandler(PlaydateAPI *pd, PDSystemEvent event, uint32_t arg)
{
    (void)arg;
    if (event == kEventInit) {
        g_pd = pd;
        mcp_harness_init(pd);
        mcp_harness_register_state(report_state);
        pd->system->setUpdateCallback(update, NULL);
    }
    return 0;
}
