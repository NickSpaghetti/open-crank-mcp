#include <stdio.h>
#include "mcp_harness.h"

_Static_assert(LCD_COLUMNS == 400, "LCD_COLUMNS changed - screenshot code assumes 400");
_Static_assert(LCD_ROWS == 240, "LCD_ROWS changed - screenshot code assumes 240");
_Static_assert(LCD_ROWSIZE == 52, "LCD_ROWSIZE changed - screenshot byte-size math assumes 52");

_Static_assert(kButtonLeft == (1 << 0), "kButtonLeft bit value changed");
_Static_assert(kButtonRight == (1 << 1), "kButtonRight bit value changed");
_Static_assert(kButtonUp == (1 << 2), "kButtonUp bit value changed");
_Static_assert(kButtonDown == (1 << 3), "kButtonDown bit value changed");
_Static_assert(kButtonB == (1 << 4), "kButtonB bit value changed");
_Static_assert(kButtonA == (1 << 5), "kButtonA bit value changed");

_Static_assert(kFileRead == (1 << 0), "kFileRead value changed");
_Static_assert(kFileReadData == (1 << 1), "kFileReadData value changed");
_Static_assert(kFileWrite == (1 << 2), "kFileWrite value changed");
_Static_assert(kFileAppend == (2 << 2), "kFileAppend value changed");

/* Never called - only needs to compile. If the SDK's declared signature
   for any of these changes, the assignment stops being type-compatible
   and the build fails right here, at the exact function, instead of a
   confusing runtime failure after a PLAYDATE_SDK_VERSION bump. */
static void check_signatures(struct playdate_sys *sys, struct playdate_file *file, struct playdate_graphics *gfx)
{
    void (*check_getButtonState)(PDButtons *, PDButtons *, PDButtons *) = sys->getButtonState;
    float (*check_getCrankChange)(void) = sys->getCrankChange;
    float (*check_getCrankAngle)(void) = sys->getCrankAngle;
    int (*check_isCrankDocked)(void) = sys->isCrankDocked;
    unsigned int (*check_getCurrentTimeMilliseconds)(void) = sys->getCurrentTimeMilliseconds;

    int (*check_stat)(const char *, FileStat *) = file->stat;
    SDFile *(*check_open)(const char *, FileOptions) = file->open;
    int (*check_close)(SDFile *) = file->close;
    int (*check_read)(SDFile *, void *, unsigned int) = file->read;
    int (*check_write)(SDFile *, const void *, unsigned int) = file->write;
    int (*check_unlink)(const char *, int) = file->unlink;
    int (*check_mkdir)(const char *) = file->mkdir;
    /* The harness publishes a response by renaming a temp file into place, so
       rename is a real dependency and belongs pinned here like the rest. */
    int (*check_rename)(const char *, const char *) = file->rename;

    uint8_t *(*check_getDisplayFrame)(void) = gfx->getDisplayFrame;

    (void)check_getButtonState;
    (void)check_getCrankChange;
    (void)check_getCrankAngle;
    (void)check_isCrankDocked;
    (void)check_getCurrentTimeMilliseconds;
    (void)check_stat;
    (void)check_open;
    (void)check_close;
    (void)check_read;
    (void)check_write;
    (void)check_unlink;
    (void)check_mkdir;
    (void)check_rename;
    (void)check_getDisplayFrame;
}

int main(void)
{
    (void)check_signatures;
    printf("sdk contract: compile-time checks passed\n");
    return 0;
}
