package doctor

import (
	"fmt"
	"strings"
)

// String renders the findings as prose.
//
// Prose and not JSON, deliberately. docs/mcp-schema.json is this project's
// structured surface and it has a contract test guarding it; a second structured
// output would be a second contract with nothing guarding it. The readers here
// are a person and a model, and both read prose.
//
// Pure: no I/O, no clock, no environment. That is what lets every branch below -
// including the ones a developer's own machine can never produce - be asserted
// in report_test.go.
func (f Findings) String() string {
	var b strings.Builder

	fmt.Fprintf(&b, "open-crank-mcp %s on %s\n\n", f.Version, f.GOOS)

	if f.SDKErr != nil {
		// Not phrased as a failure. On a machine that has just installed the
		// plugin this is the ordinary state, and the useful thing is the remedy
		// plus the list of places already looked - which Describe carries even
		// on the error path, because Resolve fills in Tried before returning.
		b.WriteString("Playdate SDK: not found\n\n")
		// The error already lists every candidate and names the remedy, so
		// Paths.Tried is deliberately not printed again underneath it. Indented
		// wholesale, including its continuation lines, because a multi-line error
		// that only indents its first line reads as two unrelated paragraphs.
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

// writeFirstRun adds the macOS first-run warning, and only ever as an inference.
//
// The wording matters more than the check. "The plist is absent" is what was
// measured; "the Simulator has never run" is what it probably means. Saying the
// second as though it were the first would be the kind of claim this project
// keeps having to correct in its own documents, so it is hedged in the text
// itself rather than in a comment nobody reading the output will see.
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

// writeIndented writes text with every non-empty line prefixed.
//
// Blank lines are left bare rather than turned into trailing whitespace, which
// keeps the output diffable and stops editors stripping it into a difference.
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
