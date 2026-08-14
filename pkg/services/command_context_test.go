package services

import (
	"testing"

	"github.com/materials-commons/mccli/pkg/config"
)

func TestCommandContextRequireClientUUID(t *testing.T) {
	cmdCtx := CommandContext{
		Global: config.Global{},
	}

	err := cmdCtx.RequireClientUUID("websocket uploads")
	if err == nil {
		t.Fatal("RequireClientUUID() error = nil, want error")
	}
}

func TestCommandContextRequireClientUUIDAcceptsConfiguredValue(t *testing.T) {
	cmdCtx := CommandContext{
		Global: config.Global{
			ClientUUID: "client-1",
		},
	}

	if err := cmdCtx.RequireClientUUID("websocket uploads"); err != nil {
		t.Fatalf("RequireClientUUID() error = %v", err)
	}
}

func TestRequireConfiguredRemoteReturnsMatchingRemote(t *testing.T) {
	remote, err := RequireConfiguredRemote(
		config.Project{
			Remote: config.Remote{
				Email: "user@example.test",
				MCURL: "https://example.test/api",
			},
		},
		config.Global{
			DefaultRemote: config.Remote{
				Email:  "user@example.test",
				MCURL:  "https://example.test/api",
				APIKey: "token",
			},
		},
	)
	if err != nil {
		t.Fatalf("RequireConfiguredRemote() error = %v", err)
	}
	if remote.APIKey != "token" {
		t.Fatalf("APIKey = %q, want token", remote.APIKey)
	}
}
