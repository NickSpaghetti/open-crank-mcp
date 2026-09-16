// Gather is I/O composition: exec, stat, and a process launch. It is in its own
// file so its gremlins exclusion covers nothing else - the same reason checks.go
// is separate from launch.go.
package doctor

import (
	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
)

// Gather runs the checks. It never returns an error: every failure it finds is a
// finding to report, not a reason to stop, which is why the caller can exit 0.
func Gather(env sdk.Env, goos, version string, opts Options) Findings {
	f := Findings{Version: version, GOOS: goos, Launched: opts.Launch}

	f.SimulatorRan, f.SimulatorRanKnown = SimulatorEverRan(env, goos)

	f.SDK, f.SDKErr = sdk.Resolve(env)
	if f.SDKErr != nil {
		// Nothing downstream can run without a resolved SDK, and saying so once
		// beats four checks each reporting the same missing path.
		return f
	}

	f.LibsErr = SharedLibraries(f.SDK.SimulatorBin)
	f.PDCOut, f.PDCErr = PDCVersion(f.SDK.PDC)
	f.DataDirs = f.SDK.DataDirCandidates(env, placeholderBundleID)

	if opts.Launch {
		f.LaunchErr = Launch(f.SDK.SimulatorBin)
	}
	return f
}
