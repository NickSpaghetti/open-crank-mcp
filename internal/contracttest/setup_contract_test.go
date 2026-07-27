package contracttest

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NickSpaghetti/open-crank-mcp/internal/build"
	"github.com/NickSpaghetti/open-crank-mcp/internal/harness"
	"github.com/NickSpaghetti/open-crank-mcp/internal/setup"
	"github.com/NickSpaghetti/open-crank-mcp/internal/simulator"
)

// TestSetupContract proves setup's patched output is actually usable, not
// just "files were written": starting from a bare, unwired game (no
// mcp_harness reference at all - what a real user's project looks like
// before running setup), it runs the real setup.Setup, builds the result
// with the real SDK, drives it through a real Simulator to confirm the
// harness responds, then runs setup.Teardown and confirms the project
// still builds clean afterward. Skipped under the same condition as
// TestSDKContract - needs the full simulator Docker image.
func TestSetupContract(t *testing.T) {
	sdkPath := os.Getenv("PLAYDATE_SDK_PATH")
	if sdkPath == "" {
		t.Skip("PLAYDATE_SDK_PATH not set - run inside the simulator image (make sdk-contract-check)")
	}

	// A separate display from TestSDKContract's :99 - these are
	// independent test functions, each self-contained, not relying on
	// execution order for shared setup.
	xvfb, err := simulator.Launch("Xvfb", ":98", "-screen", "0", "1280x800x24")
	if err != nil {
		t.Fatalf("launching Xvfb: %v", err)
	}
	defer func() {
		_ = xvfb.Stop()
		_ = xvfb.Wait()
	}()
	time.Sleep(1 * time.Second)
	if err := os.Setenv("DISPLAY", ":98"); err != nil {
		t.Fatalf("setting DISPLAY: %v", err)
	}

	t.Run("Lua", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "Source", "pdxinfo"), luaSetupFixturePdxinfo)
		writeFile(t, filepath.Join(dir, "Source", "main.lua"), luaSetupFixtureMain)

		if _, err := setup.Setup(dir, setup.Lua); err != nil {
			t.Fatalf("setup.Setup: %v", err)
		}

		buildResult, err := build.Build(dir)
		if err != nil {
			t.Fatalf("build.Build after setup: %v\n%s", err, buildResult.Output)
		}

		runAndConfirmHarnessReachable(t, sdkPath, buildResult.PdxPath, "dev.setupcontract.lua")

		if _, err := setup.Teardown(dir, setup.Lua); err != nil {
			t.Fatalf("setup.Teardown: %v", err)
		}

		if _, err := os.Stat(filepath.Join(dir, "Source", "mcp_harness.lua")); !os.IsNotExist(err) {
			t.Fatalf("mcp_harness.lua still exists after teardown (stat err = %v)", err)
		}

		buildResult, err = build.Build(dir)
		if err != nil {
			t.Fatalf("build.Build after teardown: %v\n%s", err, buildResult.Output)
		}
	})

	t.Run("C", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "CMakeLists.txt"), cSetupFixtureCMakeLists)
		writeFile(t, filepath.Join(dir, "Source", "pdxinfo"), cSetupFixturePdxinfo)
		writeFile(t, filepath.Join(dir, "src", "main.c"), cSetupFixtureMain)

		result, err := setup.Setup(dir, setup.C)
		if err != nil {
			t.Fatalf("setup.Setup: %v", err)
		}
		if len(result.ManualSteps) != 0 {
			t.Fatalf("setup.Setup().ManualSteps = %v, want empty for this fixture", result.ManualSteps)
		}

		buildResult, err := build.Build(dir)
		if err != nil {
			t.Fatalf("build.Build after setup: %v\n%s", err, buildResult.Output)
		}

		runAndConfirmHarnessReachable(t, sdkPath, buildResult.PdxPath, "dev.setupcontract.c")

		if _, err := setup.Teardown(dir, setup.C); err != nil {
			t.Fatalf("setup.Teardown: %v", err)
		}

		if _, err := os.Stat(filepath.Join(dir, "src", "mcp_harness.c")); !os.IsNotExist(err) {
			t.Fatalf("mcp_harness.c still exists after teardown (stat err = %v)", err)
		}

		buildResult, err = build.Build(dir)
		if err != nil {
			t.Fatalf("build.Build after teardown: %v\n%s", err, buildResult.Output)
		}
	})
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// runAndConfirmHarnessReachable launches pdxPath and pings the harness
// through the real file-based protocol, proving setup's patched output
// isn't just syntactically valid but actually responds as a working
// harness would.
func runAndConfirmHarnessReachable(t *testing.T, sdkPath, pdxPath, bundleID string) {
	t.Helper()
	dataDir := filepath.Join(sdkPath, "Disk", "Data", bundleID)
	defer os.RemoveAll(dataDir)

	simBin := filepath.Join(sdkPath, "bin", "PlaydateSimulator")
	sim, err := simulator.Launch(simBin, pdxPath, dataDir)
	if err != nil {
		t.Fatalf("launching simulator: %v", err)
	}
	defer func() {
		_ = sim.Stop()
		_ = sim.Wait()
	}()

	if err := harness.WaitForDir(filepath.Join(dataDir, "mcp"), mcpDirTimeout); err != nil {
		t.Fatalf("waiting for mcp/ directory: %v\nsimulator output:\n%s", err, sim.Output())
	}

	if err := harness.SendCommand(dataDir, map[string]any{"id": "1", "type": "ping"}); err != nil {
		t.Fatalf("SendCommand: %v", err)
	}
	resp, err := harness.WaitForResponse(dataDir, responseTimeout)
	if err != nil {
		t.Fatalf("WaitForResponse: %v", err)
	}
	if resp["status"] != "ok" {
		t.Fatalf(`ping response["status"] = %v, want "ok" (full response: %v)`, resp["status"], resp)
	}
}

const luaSetupFixturePdxinfo = `name=Setup Contract Fixture
author=open-crank-mcp
description=Bare, unwired fixture for TestSetupContract
bundleID=dev.setupcontract.lua
`

const luaSetupFixtureMain = `import "CoreLibs/sprites"

function playdate.update()
    playdate.graphics.sprite.update()
end
`

const cSetupFixturePdxinfo = `name=Setup Contract Fixture
author=open-crank-mcp
description=Bare, unwired fixture for TestSetupContract
bundleID=dev.setupcontract.c
`

const cSetupFixtureCMakeLists = `cmake_minimum_required(VERSION 3.14)
set(CMAKE_C_STANDARD 11)

set(ENVSDK $ENV{PLAYDATE_SDK_PATH})
if (NOT ${ENVSDK} STREQUAL "")
	file(TO_CMAKE_PATH ${ENVSDK} SDK)
else()
	execute_process(
			COMMAND bash -c "egrep '^\\s*SDKRoot' $HOME/.Playdate/config"
			COMMAND head -n 1
			COMMAND cut -c9-
			OUTPUT_VARIABLE SDK
			OUTPUT_STRIP_TRAILING_WHITESPACE
	)
endif()
if (NOT EXISTS ${SDK})
	message(FATAL_ERROR "SDK Path not found; set ENV value PLAYDATE_SDK_PATH")
	return()
endif()

set(CMAKE_CONFIGURATION_TYPES "Debug;Release")
set(PLAYDATE_GAME_NAME setupcontractcheck)
set(PLAYDATE_GAME_DEVICE setupcontractcheck_DEVICE)

project(${PLAYDATE_GAME_NAME} C ASM)

if (TOOLCHAIN STREQUAL "armgcc")
	add_executable(${PLAYDATE_GAME_DEVICE} src/main.c)
else()
	add_library(${PLAYDATE_GAME_NAME} SHARED src/main.c)
endif()

include(${SDK}/C_API/buildsupport/playdate_game.cmake)
`

const cSetupFixtureMain = `#include "pd_api.h"

static PlaydateAPI *pd;

static int update(void *userdata) {
    (void)userdata;
    pd->graphics->clear(kColorWhite);
    return 1;
}

int eventHandler(PlaydateAPI *playdate, PDSystemEvent event, uint32_t arg) {
    (void)arg;
    if (event == kEventInit) {
        pd = playdate;
        pd->system->setUpdateCallback(update, NULL);
    }
    return 0;
}
`
