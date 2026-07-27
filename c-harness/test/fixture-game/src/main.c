#include <stdio.h>
#include "pd_api.h"
#include "mcp_harness.h"

static PlaydateAPI *g_pd;
static char g_state_buf[256];

/* Persistent counters rather than reporting the raw pushed/released bits
   alone: those are one-frame-only signals, and a "state" query is a
   separate round trip from the "press" that caused them, so it usually
   lands several frames later - long after a transient bit would have
   cleared. A monotonic count proves the edge fired at all regardless of
   exactly which frame the query catches. Field names match the Lua
   fixture's a_down_count/a_up_count so the same contract-test assertions
   apply to both languages. */
static int g_a_down_count = 0;
static int g_a_up_count = 0;

static const char *report_state(void)
{
    PDButtons current, pushed, released;
    mcp_get_button_state(g_pd, &current, &pushed, &released);
    float angle = mcp_get_crank_angle(g_pd);
    float change = mcp_get_crank_change(g_pd);
    int docked = mcp_get_crank_docked(g_pd);
    snprintf(g_state_buf, sizeof(g_state_buf),
        "{\"current\":%d,\"pushed\":%d,\"released\":%d,\"crank_angle\":%.2f,"
        "\"crank_change\":%.2f,\"crank_docked\":%s,\"a_down_count\":%d,\"a_up_count\":%d}",
        (int)current, (int)pushed, (int)released, (double)angle, (double)change,
        docked ? "true" : "false", g_a_down_count, g_a_up_count);
    return g_state_buf;
}

static int update(void *userdata)
{
    (void)userdata;
    mcp_harness_update(g_pd);

    PDButtons current, pushed, released;
    mcp_get_button_state(g_pd, &current, &pushed, &released);
    if (pushed & kButtonA) g_a_down_count++;
    if (released & kButtonA) g_a_up_count++;

    return 1;
}

int eventHandler(PlaydateAPI *pd, PDSystemEvent event, uint32_t arg)
{
    (void)arg;
    if (event == kEventInit) {
        g_pd = pd;
        pd->graphics->clear(kColorBlack);
        mcp_harness_init(pd);
        mcp_harness_register_state(report_state);
        pd->system->setUpdateCallback(update, NULL);

        /* One sprite with a collide rect, one without - so the entities
           command's querySpritesInRect approximation has something real
           to differ on: the collidable one should show up, the decorative
           one should not. */
        LCDSprite *collidable = pd->sprite->newSprite();
        pd->sprite->setSize(collidable, 16, 16);
        pd->sprite->moveTo(collidable, 50, 60);
        PDRect collideRect = {0, 0, 16, 16};
        pd->sprite->setCollideRect(collidable, collideRect);
        pd->sprite->addSprite(collidable);

        LCDSprite *decorative = pd->sprite->newSprite();
        pd->sprite->setSize(decorative, 8, 8);
        pd->sprite->moveTo(decorative, 100, 120);
        pd->sprite->addSprite(decorative);
    }
    return 0;
}
