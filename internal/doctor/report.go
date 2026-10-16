package doctor

import (
	"fmt"
	"strings"
)

// String renders the findings as prose for a person or a model. Pure, so every
// branch is asserted in report_test.go.
func (f Findings) String() string {
	var b strings.Builder

	fmt.Fprintf(&b, "open-crank-mcp %s on %s\n\n", f.Version, f.GOOS)

	if f.SDKErr != nil {
		b.WriteString("Playdate SDK: not found\n\n")
		// The error already lists every candidate, so Tried is not repeated.
		writeIndented(&b, "  ", f.SDKErr.Error())
		b.WriteString("\nEverything else is skipped: nothing can be checked without an SDK.\n")
		f.writeFirstRun(&b)
		return b.String()
	}

	b.WriteString("Playdate SDK\n")
	writeIndented(&b, "  ", f.SDK.Describe())
	b.WriteString("\n")

	b.WriteString("Simulator\n")
	if f.LibsErr != nil {
		fmt.Fprintf(&b, "  shared libraries: %v\n", f.LibsErr)
	} else {
		b.WriteString("  shared libraries: ok\n")
	}
	if f.PDCErr != nil {
		fmt.Fprintf(&b, "  pdc:              %v\n", f.PDCErr)
	} else {
		fmt.Fprintf(&b, "  pdc:              %s\n", strings.TrimSpace(f.PDCOut))
	}

	switch {
	case !f.Launched:
		b.WriteString("  launch:           not attempted (pass -doctor-launch to try it)\n")
	case f.LaunchErr != nil:
		fmt.Fprintf(&b, "  launch:           %v\n", f.LaunchErr)
	default:
		b.WriteString("  launch:           ok\n")
	}
	b.WriteString("\n")

	if len(f.DataDirs) > 0 {
		b.WriteString("Game data would be looked for in, in order:\n")
		for _, d := range f.DataDirs {
			fmt.Fprintf(&b, "  %s\n", d)
		}
		b.WriteString("\n")
	}

	f.writeFirstRun(&b)
	return b.String()
}

// writeFirstRun adds the macOS first-run warning, worded as an inference.
func (f Findings) writeFirstRun(b *strings.Builder) {
	if !f.SimulatorRanKnown || f.SimulatorRan {
		return
	}
	b.WriteString("\nmacOS: the Simulator appears never to have run on this machine, so " +
		"expect a first-run dialog.\n" +
		"  It opens behind the Simulator window and the game waits on a click that never\n" +
		"  comes, while `Loading: <game>.pdx/` still appears on stdout - so a launch looks\n" +
		"  fine and nothing happens. Dismiss it once by hand. The documented setting does\n" +
		"  not suppress it; see the macOS entry in docs/GOTCHAS.md.\n")
}

// writeIndented writes text with every non-empty line prefixed, leaving blank
// lines bare rather than adding trailing whitespace.
func writeIndented(b *strings.Builder, prefix, text string) {
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if line == "" {
			b.WriteString("\n")
			continue
		}
		b.WriteString(prefix)
		b.WriteString(line)
		b.WriteString("\n")
	}
}
