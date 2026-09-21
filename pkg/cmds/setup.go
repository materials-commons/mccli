package cmds

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"charm.land/huh/v2"
	"github.com/creativeprojects/go-selfupdate"
	"github.com/materials-commons/mccli/pkg/config"
)

type setupRunner struct {
	fdCommandPath string
	rgCommandPath string
}

func RunSetupCmd(config config.Global, cfgLoadErr error) error {
	return (&setupRunner{}).run(config, cfgLoadErr)
}

func (r *setupRunner) run(config config.Global, cfgLoadErr error) error {
	// Show the user the initial setup form. This will prompt them to install
	// any missing tools.
	if err := r.runInitialSetupForm(); err != nil {
		return err
	}

	if err := r.promptForAuth(config); err != nil {
		return err
	}

	installTools, err := r.promptForToolInstall()
	if err != nil {
		return err
	}

	if installTools {
		if err := r.installToolsFunc(); err != nil {
			return err
		}
	}

	// Check if fd and ripgrep are installed. If they are, then we can prompt to configure project locations.
	// Otherwise, just skip this step.
	if r.fdCommandPath != "" && r.rgCommandPath != "" {
		configureProjectLocations, err := r.runConfigureProjectLocationsForm()
		if err != nil {
			return err
		}

		var projectLocations []string

		if configureProjectLocations {
			locations, err := r.promptProjectLocations()
			if err != nil {
				return err
			}

			projectLocations = locations
		}

		_ = projectLocations
	}

	return nil
}

func (r *setupRunner) runInitialSetupForm() error {

	welcome := `Welcome to the Materials Commons CLI setup wizard.
Here you will configure mccli to connect to a Materials Commons server, and setup optional features.
You can rerun this wizard at any time by running 'mc2 setup'.

You will be taken through the following steps:

1. Setup your auth token so that 'mc2' can connect to a Materials Commons server.

2. If you don't have 'fdfind' and/or 'ripgrep' installed you will be asked if you want to install them.

   This is optional, but the commands are required if you want to use the 'mc2 search' and 'mc2 find' commands.
   These commands allow you to find files and directories by name (mc2 find) or search file content (mc2 search).

3. If you already have 'fdfind' and/or 'ripgrep' installed or you chose to have 'mc2' install them, you will be given
   the option to configure where your projects are located on your computer. This is optional, but it is recommended.
   Doing this will allow you to search and find files across multiple projects from anywhere on your computer.
`
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Materials Commons CLI Setup").
				Description(welcome).
				Next(true).
				NextLabel("Next")),
	)

	if err := form.Run(); err != nil {
		return err
	}

	return nil
}

func (r *setupRunner) promptForAuth(cfg config.Global) error {
	apiKey := cfg.DefaultRemote.APIKey
	mcurl := cfg.DefaultRemote.MCURL
	email := cfg.DefaultRemote.Email

	if mcurl == "" {
		mcurl = "https://materialscommons.org/api"
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Email").
				Description("Enter your email login").
				Placeholder(email).
				Value(&email),
			huh.NewInput().
				Title("API Key").
				Description("Enter your API Key").
				Placeholder(apiKey).
				Value(&apiKey),
			huh.NewInput().
				Title("Server URL").
				Description("Enter your the URL for Materials Commons").
				Placeholder(mcurl).
				Value(&mcurl),
		),
	)

	if err := form.Run(); err != nil {
		return err
	}

	return nil
}

func (r *setupRunner) promptForToolInstall() (bool, error) {
	var installTools bool

	// Check to see if we need to install fd and/or ripgrep
	hasFdFind := r.findFdCommand()
	hasRg := r.findRgCommand()

	missingTools := !hasFdFind || !hasRg
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Install optional tools?").
				Description("fdfind/fd and/or ripgrep/rg were not found. Would you like to install them?").
				Value(&installTools).
				Affirmative("Install").
				Negative("Skip"),
		).WithHideFunc(func() bool {
			// This is a bit confusing. The HideFunc should return true if we want to hide this. So we invert
			// this test. For example, assuming missingTools is true. Returning true means hiding this. So we
			// want to return !missingTools (ie, false for this case) to get this to show.
			return !missingTools
		}),
		huh.NewGroup(
			huh.NewNote().
				Title("Optional tools found.").
				Description("Both fdfind/fd and ripgrep/rg found!").
				Next(true).
				NextLabel("Next"),
		).WithHideFunc(func() bool {
			// As above, this is a bit confusing. The HideFunc should return true if we want to hide this. So if
			// there are missing tools, then we hide this.
			return missingTools
		}),
	)

	if err := form.Run(); err != nil {
		return false, err
	}

	return installTools, nil
}

func (r *setupRunner) runConfigureProjectLocationsForm() (bool, error) {
	var configureProjectLocations bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Configure project locations?").
				Description("Would you like to configure where your projects are located on your computer?").
				Value(&configureProjectLocations).
				Affirmative("Configure").
				Negative("Skip"),
		))

	if err := form.Run(); err != nil {
		return false, err
	}

	return configureProjectLocations, nil
}

func (r *setupRunner) promptProjectLocations() ([]string, error) {
	var locations []string

	for {
		var location string

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Project location").
					Description("Enter a directory containing Materials Commons projects, or press Enter when finished.").
					Placeholder("/path/to/projects").
					Value(&location),
			),
		)

		if err := form.Run(); err != nil {
			return nil, err
		}

		location = strings.TrimSpace(location)
		if location == "" {
			break
		}

		locations = append(locations, location)
	}

	return locations, nil
}

func (r *setupRunner) installToolsFunc() error {
	if r.rgCommandPath == "" {
		r.installRgCommand()
	}

	if r.fdCommandPath == "" {
		r.installFdCommand()
	}

	return nil
}

func (r *setupRunner) installRgCommand() error {
	repoSlug := selfupdate.ParseSlug("BurntSushi/ripgrep")
	fmt.Println("repoSlug = ", repoSlug)
	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		fmt.Println("Error creating GitHub source: ", err)
		return err
	}
	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source: source,
	})

	if err != nil {
		fmt.Println("Error creating updater: ", err)
		return err
	}

	ctx := context.Background()
	latest, found, err := updater.DetectLatest(ctx, repoSlug)
	if err != nil {
		fmt.Println("Error detecting latest release: ", err)
		return err
	}

	if !found {
		fmt.Println("No release found")
		return errors.New("no release found")
	}

	fmt.Printf("I want to download %s\n", latest.AssetURL)
	return nil
}

func (r *setupRunner) installFdCommand() {
}

func (r *setupRunner) findCommand(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	return path, nil
}

func (r *setupRunner) findFdCommand() bool {
	path, err := r.findCommand("fdfind")
	if err == nil {
		r.fdCommandPath = path
		return true
	}

	path, err = r.findCommand("fd")
	if err == nil {
		r.fdCommandPath = path
		return true
	}

	return false
}

func (r *setupRunner) findRgCommand() bool {
	path, err := r.findCommand("rg")
	if err == nil {
		r.rgCommandPath = path
		return true
	}

	return false
}
