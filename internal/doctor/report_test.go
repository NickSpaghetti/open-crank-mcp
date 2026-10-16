package doctor

import (
	"errors"
	"strings"
	"testing"

	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
)

// okFindings is a healthy machine, the base for varying one thing at a time.
func okFindings() Findings {
	return Findings{
		Version: "0.1.0",
		GOOS:    "linux",
		SDK: sdk.Paths{
			Root:         "/home/u/PlaydateSDK",
			RootSource:   sdk.SourceEnv,
			SimulatorBin: "/home/u/PlaydateSDK/bin/PlaydateSimulator",
			PDC:          "/home/u/PlaydateSDK/bin/pdc",
		},
		PDCOut:   "3.1.1\n",
		DataDirs: []string{"/home/u/PlaydateSDK/Disk/Data/<bundleID>"},
	}
}

// An SDK found on the second try makes Describe emit a blank line, exercising
// writeIndented's blank-line branch.
func TestReportIndentsAMultiParagraphSDKBlock(t *testing.T) {
	f := okFindings()
	f.SDK.Tried = []string{"/home/u/.Playdate/config (SDKRoot key)", "/home/u/PlaydateSDK (found)"}
	got := f.String()

	if !strings.Contains(got, "  considered, in order:") {
		t.Errorf("the candidate list was not indented under the SDK heading:\n%s", got)
	}
	if !strings.Contains(got, "    /home/u/PlaydateSDK (found)") {
		t.Errorf("candidate entries kept their own indent under the prefix:\n%s", got)
	}
	if strings.Contains(got, "  \n") {
		t.Errorf("a blank line was padded with the indent prefix:\n%q", got)
	}
}

func TestReportHealthyMachine(t *testing.T) {
	got := okFindings().String()
	for _, want := range []string{
		"open-crank-mcp 0.1.0 on linux",
		"/home/u/PlaydateSDK",
		"shared libraries: ok",
		"pdc:              3.1.1",
		"launch:           not attempted",
		"/home/u/PlaydateSDK/Disk/Data/<bundleID>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report does not mention %q:\n%s", want, got)
		}
	}
	// A healthy Linux machine must not be told about a macOS dialog.
	if strings.Contains(got, "first-run") {
		t.Errorf("report warned about the macOS first-run dialog on linux:\n%s", got)
	}
}

func TestReportMacFirstRunIsPhrasedAsInference(t *testing.T) {
	f := okFindings()
	f.GOOS = "darwin"
	f.SimulatorRanKnown = true
	f.SimulatorRan = false
	got := f.String()

	if !strings.Contains(got, "appears never to have run") {
		t.Errorf("the macOS warning does not read as an inference:\n%s", got)
	}
	for _, want := range []string{"expect a first-run dialog", "Dismiss it once by hand", "docs/GOTCHAS.md"} {
		if !strings.Contains(got, want) {
			t.Errorf("the macOS warning omits %q:\n%s", want, got)
		}
	}
}

func TestReportMacThatHasRunSaysNothing(t *testing.T) {
	f := okFindings()
	f.GOOS = "darwin"
	f.SimulatorRanKnown = true
	f.SimulatorRan = true
	if got := f.String(); strings.Contains(got, "first-run") {
		t.Errorf("a Mac that has run the Simulator was still warned:\n%s", got)
	}
}

func TestReportNoSDK(t *testing.T) {
	f := Findings{
		Version: "0.1.0",
		GOOS:    "linux",
		SDKErr:  errors.New("could not find a Playdate SDK. Looked at:\n  /a\n  /b\nSet PLAYDATE_SDK_PATH"),
	}
	got := f.String()

	if !strings.Contains(got, "Playdate SDK: not found") {
		t.Errorf("no-SDK report does not say so:\n%s", got)
	}
	if !strings.Contains(got, "nothing can be checked without an SDK") {
		t.Errorf("no-SDK report does not explain why the rest is missing:\n%s", got)
	}
	for _, want := range []string{"  could not find a Playdate SDK", "    /a", "    /b", "  Set PLAYDATE_SDK_PATH"} {
		if !strings.Contains(got, want) {
			t.Errorf("no-SDK report is not indented as expected, missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"shared libraries: ok", "launch:"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("no-SDK report claims %q despite having no SDK:\n%s", unwanted, got)
		}
	}
}

// Unreachable in production, since DataDirCandidates always returns at least one path.
func TestReportOmitsTheDataDirHeadingWhenThereAreNone(t *testing.T) {
	f := okFindings()
	f.DataDirs = nil
	if got := f.String(); strings.Contains(got, "Game data would be looked for") {
		t.Errorf("the data-directory heading was printed over an empty list:\n%s", got)
	}
}

func TestReportFailedChecks(t *testing.T) {
	f := okFindings()
	f.LibsErr = errors.New("missing shared libraries:\n\tlibwebkit2gtk-4.1.so.0 => not found")
	f.PDCErr = errors.New("pdc --version: permission denied")
	f.Launched = true
	f.LaunchErr = errors.New("simulator exited early")
	got := f.String()

	for _, want := range []string{"libwebkit2gtk", "permission denied", "simulator exited early"} {
		if !strings.Contains(got, want) {
			t.Errorf("report omits the failure %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "shared libraries: ok") {
		t.Errorf("a failed library check still reported ok:\n%s", got)
	}
	if strings.Contains(got, "not attempted") {
		t.Errorf("a launch that ran was reported as not attempted:\n%s", got)
	}
}

func TestReportDistinguishesLaunchedFromSkipped(t *testing.T) {
	skipped := okFindings().String()
	f := okFindings()
	f.Launched = true
	ran := f.String()

	if !strings.Contains(skipped, "launch:           not attempted") {
		t.Errorf("a skipped launch was not reported as skipped:\n%s", skipped)
	}
	if !strings.Contains(ran, "launch:           ok") {
		t.Errorf("a successful launch was not reported as ok:\n%s", ran)
	}
}
