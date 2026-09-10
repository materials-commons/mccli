package cmds

import (
	"context"
	"fmt"

	"github.com/materials-commons/mccli/pkg/di"
)

type configRunner struct {
	deps di.Dependencies
}

type ConfigOpts struct {
	// Show only global config
	ShowGlobal bool

	// Show only project config
	ShowProject bool

	// Show the APIKey (normally hidden)
	ShowAPIKey bool

	// Show both global and project config
	ShowAll bool
}

func RunConfigCmd(ctx context.Context, opts ConfigOpts) error {
	return configRunner{deps: di.Production()}.run(ctx, opts)
}

func (r configRunner) run(ctx context.Context, opts ConfigOpts) error {
	switch {
	case opts.ShowAll:
		return r.showAll(ctx, opts.ShowAPIKey)
	case opts.ShowGlobal:
		return r.showGlobal(ctx, opts.ShowAPIKey)
	case opts.ShowProject:
		return r.showProject(ctx)
	default:
		return r.showGlobal(ctx, opts.ShowAPIKey)
	}
}

func (r configRunner) showAll(ctx context.Context, showAPIKey bool) error {
	_ = r.showGlobal(ctx, showAPIKey)
	fmt.Println("")
	_ = r.showProject(ctx)
	return nil
}

func (r configRunner) showGlobal(ctx context.Context, showAPIKey bool) error {
	cfg, err := r.deps.LoadGlobal(ctx, "")
	if err != nil {
		fmt.Printf("No global config - Please login to set up\n")
		return err
	}

	fmt.Printf("=== Global config ===\n")
	fmt.Printf("  Global Config Path: %s\n", cfg.Path())
	fmt.Printf("  Default Remote\n")
	fmt.Printf("    Email             : %s\n", cfg.DefaultRemote.Email)
	if showAPIKey {
		fmt.Printf("    APIKey            : %s\n", cfg.DefaultRemote.APIKey)
	}
	fmt.Printf("    Default Remote URL: %s\n", cfg.DefaultRemote.MCURL)
	fmt.Printf("\n  Known Remotes\n")

	numRemotes := len(cfg.Remotes)
	for idx, remote := range cfg.Remotes {
		fmt.Printf("    Email             : %s\n", remote.Email)
		if showAPIKey {
			fmt.Printf("    APIKey            : %s\n", remote.APIKey)
		}
		fmt.Printf("    Default Remote URL: %s\n", remote.MCURL)
		if idx != numRemotes-1 {
			fmt.Printf("\n")
		}
	}

	fmt.Printf("  Client UUID: %s\n", cfg.ClientUUID)

	return nil
}

func (r configRunner) showProject(ctx context.Context) error {
	cfg, err := r.deps.LoadProject(ctx, "")
	if err != nil {
		fmt.Printf("No project config - Are you in a project?\n")
		return err
	}

	fmt.Printf("=== Project config ===\n")
	fmt.Printf("  Project Config Path: %s\n", cfg.Path())
	fmt.Printf("  Remote Email       : %s\n", cfg.Remote.Email)
	fmt.Printf("  Remote URL         : %s\n", cfg.Remote.MCURL)
	fmt.Printf("  Remote Project ID  : %d\n", cfg.ProjectID)

	return nil
}
