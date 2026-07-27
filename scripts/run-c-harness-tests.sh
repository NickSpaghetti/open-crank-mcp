#!/usr/bin/env bash
set -euo pipefail

CFLAGS=(-DTARGET_EXTENSION=1 -I"${PLAYDATE_SDK_PATH}/C_API" -Ic-harness -Wall -Wextra -Werror -fsanitize=address,undefined -g -O1)

gcc "${CFLAGS[@]}" -o /tmp/test_sdk_contract c-harness/test/test_sdk_contract.c
/tmp/test_sdk_contract

gcc "${CFLAGS[@]}" -o /tmp/test_pure_logic c-harness/test/test_pure_logic.c c-harness/mcp_harness.c
/tmp/test_pure_logic

gcc "${CFLAGS[@]}" -o /tmp/test_fake_api c-harness/test/test_fake_api.c c-harness/mcp_harness.c
/tmp/test_fake_api

echo "all c-harness tests passed"
