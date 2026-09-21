package main

import (
	"testing"
)

func TestDownloadLatestRelease(t *testing.T) {
	s := setupRunner{}

	err := s.installRgCommand()

	if err != nil {
		t.Errorf("Error installing rg command: %v", err)
	}
}
