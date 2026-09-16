package cmds

import (
	"os/exec"
	"strings"

	"charm.land/huh/v2"
	"github.com/materials-commons/mccli/pkg/config"
	"github.com/materials-commons/mccli/pkg/di"
)

type setupRunner struct {
	deps          di.Dependencies
	fdCommandPath string
	rgCommandPath string
}

func RunSetupCmd(config config.Global, cfgLoadErr error) error {
	return (&setupRunner{deps: di.Production()}).run(config, cfgLoadErr)
}

func (r *setupRunner) run(config config.Global, cfgLoadErr error) error {
	// Show the user the initial setup form. This will prompt them to install
	// any missing tools.
	installTools, err := r.runInitialSetupForm()
	if err != nil {
		return err
	}

	if installTools {
		if err := r.installToolsFunc(); err != nil {
			return err
		}
	}

	// Check if fd and ripgrep are installed. If they are then we can prompt to configure project locations.
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

func (r *setupRunner) runInitialSetupForm() (bool, error) {
	var installTools bool

	// Check to see if we need to install fd and/or ripgrep
	hasFdFind := r.findFdCommand()
	hasRg := r.findRgCommand()

	missingTools := !hasFdFind || !hasRg

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
		huh.NewGroup(huh.NewNote().
			Title("Materials Commons CLI Setup").
			Description(welcome).
			Next(true).
			NextLabel("Next")),
		huh.NewGroup(
			huh.NewConfirm().
				Title("Install optional tools?").
				Description("fdfind/fd and/or ripgrep/rg were not found. Would you like to install them?").
				Value(&installTools).
				Affirmative("Install").
				Negative("Skip"),
		).WithHideFunc(func() bool {
			return !missingTools
		}),
	)

	if err := form.Run(); err != nil {
		return false, err
	}

	if !missingTools {
		return false, nil
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

func (r *setupRunner) promptForAuth() error {
	var apiKey string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("API Key").
				Description("Enter your API Key").
				Placeholder("show existing api key here if there is one").
				Value(&apiKey),
		),
	)

	if err := form.Run(); err != nil {
		return err
	}

	return nil
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
	return nil
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
