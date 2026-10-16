package doctor

import (
	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
)

// Gather runs the checks. Every failure is a finding, not an error.
func Gather(env sdk.Env, goos, version string, opts Options) Findings {
	f := Findings{Version: version, GOOS: goos, Launched: opts.Launch}

	f.SimulatorRan, f.SimulatorRanKnown = SimulatorEverRan(env, goos)

	f.SDK, f.SDKErr = sdk.Resolve(env)
	if f.SDKErr != nil {
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
