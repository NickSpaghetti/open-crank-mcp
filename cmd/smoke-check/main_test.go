package main

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
