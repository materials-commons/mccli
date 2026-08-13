package di

import (
	"context"
	"testing"
	"time"

	"github.com/materials-commons/mccli/pkg/config"
)

func TestWithDefaultsFillsMissingDependencies(t *testing.T) {
	deps := WithDefaults(Dependencies{})

	if deps.LoadProject == nil {
		t.Fatal("LoadProject is nil")
	}
	if deps.LoadGlobal == nil {
		t.Fatal("LoadGlobal is nil")
	}
	if deps.OpenStore == nil {
		t.Fatal("OpenStore is nil")
	}
	if deps.NewRemote == nil {
		t.Fatal("NewRemote is nil")
	}
	if deps.NewUploadManager == nil {
		t.Fatal("NewUploadManager is nil")
	}
	if deps.NewWebSocket == nil {
		t.Fatal("NewWebSocket is nil")
	}
	if deps.Now == nil {
		t.Fatal("Now is nil")
	}
}

func TestWithDefaultsPreservesProvidedDependencies(t *testing.T) {
	now := func() time.Time {
		return time.Unix(123, 0)
	}

	loadGlobal := func(ctx context.Context, path string) (config.Global, error) {
		return config.Global{ClientUUID: "test-client"}, nil
	}

	deps := WithDefaults(Dependencies{
		Now:        now,
		LoadGlobal: loadGlobal,
	})

	if got := deps.Now(); got.Unix() != 123 {
		t.Fatalf("Now() = %v, want Unix 123", got)
	}

	global, err := deps.LoadGlobal(context.Background(), "")
	if err != nil {
		t.Fatalf("LoadGlobal() error = %v", err)
	}
	if global.ClientUUID != "test-client" {
		t.Fatalf("ClientUUID = %q, want test-client", global.ClientUUID)
	}
}

func TestNewRemoteClientRejectsMissingRemote(t *testing.T) {
	_, err := NewRemoteClient(
		config.Project{
			Remote: config.Remote{
				MCURL: "https://example.test/api",
				Email: "user@example.test",
			},
		},
		config.Global{},
	)
	if err == nil {
		t.Fatal("NewRemoteClient() error = nil, want error")
	}
}

func TestNewRemoteClientRejectsMissingAPIKey(t *testing.T) {
	_, err := NewRemoteClient(
		config.Project{
			Remote: config.Remote{
				MCURL: "https://example.test/api",
				Email: "user@example.test",
			},
		},
		config.Global{
			DefaultRemote: config.Remote{
				MCURL: "https://example.test/api",
				Email: "user@example.test",
			},
		},
	)
	if err == nil {
		t.Fatal("NewRemoteClient() error = nil, want error")
	}
}
