package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

func TestNewCommandLayout(t *testing.T) {
	cmd := newCommand()

	if cmd.Name != "mc2" {
		t.Fatalf("command name = %q, want %q", cmd.Name, "mc2")
	}

	wantCommands := []string{
		"version",
		"clone",
		"config",
		"down",
		"init",
		"ls",
		"mkdir",
		"mv",
		"proj",
		"rm",
		"remotes",
		"up",
	}

	for _, name := range wantCommands {
		if findCommand(cmd, name) == nil {
			t.Errorf("missing subcommand %q", name)
		}
	}
}

func TestImportantFlags(t *testing.T) {
	cmd := newCommand()

	tests := []struct {
		command string
		flag    string
	}{
		{command: "clone", flag: "id"},
		{command: "config", flag: "proj"},
		{command: "down", flag: "recursive"},
		{command: "down", flag: "force"},
		{command: "ls", flag: "action"},
		{command: "mkdir", flag: "remote-only"},
		{command: "mv", flag: "remote-only"},
		{command: "rm", flag: "recursive"},
		{command: "rm", flag: "remote-only"},
		{command: "remotes", flag: "list"},
		{command: "remotes", flag: "show-apikey"},
		{command: "remotes", flag: "add"},
		{command: "remotes", flag: "remove"},
		{command: "remotes", flag: "set-default"},
		{command: "up", flag: "recursive"},
		{command: "up", flag: "ws-url"},
	}

	for _, tt := range tests {
		t.Run(tt.command+"/"+tt.flag, func(t *testing.T) {
			subcommand := findCommand(cmd, tt.command)
			if subcommand == nil {
				t.Fatalf("missing subcommand %q", tt.command)
			}

			if !hasFlag(subcommand, tt.flag) {
				t.Fatalf("command %q missing flag %q", tt.command, tt.flag)
			}
		})
	}
}

func TestFormatVersion(t *testing.T) {
	t.Setenv("GOTOOLCHAIN", "local")

	oldVersion := version
	oldGitTag := gitTag
	oldGitBranch := gitBranch
	oldGitCommit := gitCommit
	oldGitDate := gitDate
	oldGitDirty := gitDirty

	t.Cleanup(func() {
		version = oldVersion
		gitTag = oldGitTag
		gitBranch = oldGitBranch
		gitCommit = oldGitCommit
		gitDate = oldGitDate
		gitDirty = oldGitDirty
	})

	version = "2.0.0"
	gitTag = "v2.0.0"
	gitBranch = "main"
	gitCommit = "abc1234"
	gitDate = "2026-08-08T12:00:00Z"
	gitDirty = "false"

	got := formatVersion()

	for _, want := range []string{
		"mc2: 2.0.0",
		"git tag: v2.0.0",
		"git branch: main",
		"git commit: abc1234",
		"git date: 2026-08-08T12:00:00Z",
		"git dirty: false",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("formatVersion() missing %q in:\n%s", want, got)
		}
	}
}

func findCommand(cmd *cli.Command, name string) *cli.Command {
	for _, subcommand := range cmd.Commands {
		if subcommand.Name == name {
			return subcommand
		}
	}

	return nil
}

func hasFlag(cmd *cli.Command, name string) bool {
	for _, flag := range cmd.Flags {
		if slices.Contains(flag.Names(), name) {
			return true
		}
	}

	return false
}
