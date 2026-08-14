package services

import (
	"fmt"

	"github.com/materials-commons/mccli/pkg/config"
)

// CommandContext contains command-wide shared initialization.
type CommandContext struct {
	Container   *Container
	Project     config.Project
	Global      config.Global
	ProjectRoot string
}

// RequireClientUUID validates commands that need websocket/client identity.
func (c CommandContext) RequireClientUUID(reason string) error {
	if c.Global.ClientUUID == "" {
		if reason == "" {
			reason = "command"
		}
		return fmt.Errorf("global config client_uuid is required for %s", reason)
	}
	return nil
}

// RequireConfiguredRemote returns the configured remote matching the project.
func RequireConfiguredRemote(project config.Project, global config.Global) (config.Remote, error) {
	remoteCfg, ok := global.FindRemote(project.Remote.Email, project.Remote.MCURL)
	if !ok {
		return config.Remote{}, fmt.Errorf("remote %s %s is not configured in global config", project.Remote.Email, project.Remote.MCURL)
	}
	if remoteCfg.APIKey == "" {
		return config.Remote{}, fmt.Errorf("remote %s %s is missing an API key", project.Remote.Email, project.Remote.MCURL)
	}
	return remoteCfg, nil
}
