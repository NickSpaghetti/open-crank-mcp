package doctor

import "testing"

func TestCheckForErrorsPassesOnCleanOutput(t *testing.T) {
	if err := checkForErrors("Playdate Simulator started\nRunning game\n"); err != nil {
		t.Fatalf("checkForErrors: %v", err)
	}
}

func TestCheckForErrorsCatchesError(t *testing.T) {
	if err := checkForErrors("error: could not load pdx\n"); err == nil {
		t.Fatal("checkForErrors: expected an error, got nil")
	}
}

func TestCheckForErrorsCatchesNotFound(t *testing.T) {
	if err := checkForErrors("libfoo.so: not found\n"); err == nil {
		t.Fatal("checkForErrors: expected an error, got nil")
	}
}

func TestCheckForErrorsCatchesInitalizedTypo(t *testing.T) {
	// SDL2 itself uses the misspelling "initalized". Matched directly, not
	// just the correctly-spelled form.
	if err := checkForErrors("audio device could not be initalized\n"); err == nil {
		t.Fatal("checkForErrors: expected an error, got nil")
	}
}

func TestCheckForErrorsCatchesInitializedCorrectSpelling(t *testing.T) {
	if err := checkForErrors("audio device could not be initialized\n"); err == nil {
		t.Fatal("checkForErrors: expected an error, got nil")
	}
}

func TestCheckForErrorsIsCaseInsensitive(t *testing.T) {
	if err := checkForErrors("ERROR: something broke\n"); err == nil {
		t.Fatal("checkForErrors: expected an error, got nil")
	}
}

// The exact output that failed `make smoke-check` on a healthy machine: the
// Simulator started, and libEGL mentioned "error" inside a line it had already
// labelled a warning.
func TestCheckForErrorsIgnoresLibEGLWarnings(t *testing.T) {
	const output = `
(PlaydateSimulator:94): Gtk-WARNING **: 19:03:51.504: Could not load a pixbuf from /org/gtk/libgtk/theme/Adwaita/assets/bullet-symbolic.svg.
This may indicate that pixbuf loaders or the mime database could not be found.
19:03:51: Loading: /opt/playdate-sdk/Disk/System/Launcher.pdx/
libEGL warning: DRI3 error: Could not get DRI3 device
libEGL warning: Ensure your X server supports DRI3 to get accelerated rendering
`
	if err := checkForErrors(output); err != nil {
		t.Fatalf("checkForErrors flagged a healthy run: %v", err)
	}
}

// A warning on one line must not excuse a real error on another.
func TestCheckForErrorsStillCatchesAnErrorBesideAWarning(t *testing.T) {
	const output = `libEGL warning: DRI3 error: Could not get DRI3 device
audio device could not be initalized
`
	if err := checkForErrors(output); err == nil {
		t.Fatal("checkForErrors: expected the real error to be caught")
	}
}
