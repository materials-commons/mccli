// Package di contains shared dependency wiring for mc2 command packages.
//
// Command packages should stay focused on command behavior. This package owns
// production dependency construction and small interfaces used by multiple
// commands.
package di

import (
	"context"
	"fmt"
	"time"

	mcapi "github.com/materials-commons/gomcapi"
	"github.com/materials-commons/mccli/pkg/config"
	"github.com/materials-commons/mccli/pkg/download"
	"github.com/materials-commons/mccli/pkg/filedb"
	"github.com/materials-commons/mccli/pkg/reconcile"
	"github.com/materials-commons/mccli/pkg/upload"
	"github.com/materials-commons/mccli/pkg/wsclient"
)

// Store is the project file database behavior used by command packages.
//
// It intentionally includes the union of methods currently needed by ls and up.
// Commands should still use only the subset they need.
type Store interface {
	reconcile.DirectoryRecordGetter
	reconcile.FileRecordGetter

	Close(ctx context.Context) error
	Upsert(ctx context.Context, record filedb.FileRecord) error
}

// RemoteClient is the Materials Commons API behavior used by command packages.
//
// *gomcapi.Client satisfies this interface.
type RemoteClient interface {
	reconcile.RemoteDirectoryLister
	reconcile.RemoteFileGetter
}

// UploadManager queues and runs websocket uploads.
type UploadManager interface {
	StartWorkers(ctx context.Context)
	StopWorkers()
	QueueUpload(req upload.Request) (string, error)
	HandleMessage(msg wsclient.TextMessage)
	Result(transferID string) (upload.Result, bool)
}

// DownloadManager queues and runs HTTP Range downloads.
type DownloadManager interface {
	StartWorkers(ctx context.Context)
	StopWorkers()
	QueueDownload(req download.Request) (string, error)
	Result(transferID string) (download.Result, bool)
}

// WebSocketRunner runs a websocket client.
type WebSocketRunner interface {
	Run(ctx context.Context) error
}

// WebSocketConfig configures the websocket runner dependency.
type WebSocketConfig struct {
	URL        string
	Token      string
	ClientID   string
	Outbound   *wsclient.Queue[wsclient.OutboundMessage]
	Handle     wsclient.Handler
	ProjectIDs []int
}

// Dependencies contains injectable command dependencies shared by command
// packages.
//
// These are constructors only. They should not be interpreted as a fully
// initialized command dependency graph. Higher-level packages decide which
// services are needed and request them lazily.
type Dependencies struct {
	LoadProject func(ctx context.Context, start string) (config.Project, error)
	LoadGlobal  func(ctx context.Context, path string) (config.Global, error)
	OpenStore   func(ctx context.Context, projectRoot string) (Store, error)
	NewRemote   func(project config.Project, global config.Global) (RemoteClient, error)

	NewUploadManager   func(cfg upload.Config) (UploadManager, error)
	NewDownloadManager func(cfg download.Config) (DownloadManager, error)
	NewWebSocket       func(cfg WebSocketConfig) WebSocketRunner

	Now func() time.Time
}

// Production returns the default production factory set.
//
// Nothing returned here is constructed eagerly except the function values
// themselves. Command-specific service construction belongs in pkg/services.
func Production() Dependencies {
	return Dependencies{
		LoadProject: config.LoadProject,
		LoadGlobal:  config.LoadGlobal,
		OpenStore: func(ctx context.Context, projectRoot string) (Store, error) {
			return filedb.Open(ctx, projectRoot)
		},
		NewRemote: NewRemoteClient,
		NewUploadManager: func(cfg upload.Config) (UploadManager, error) {
			return upload.NewManager(cfg)
		},
		NewDownloadManager: func(cfg download.Config) (DownloadManager, error) {
			return download.NewManager(cfg)
		},
		NewWebSocket: func(cfg WebSocketConfig) WebSocketRunner {
			return &wsclient.Client{
				URL:        cfg.URL,
				Token:      cfg.Token,
				ClientID:   cfg.ClientID,
				Outbound:   cfg.Outbound,
				Handle:     cfg.Handle,
				ProjectIDs: cfg.ProjectIDs,
			}
		},
		Now: time.Now,
	}
}

// WithDefaults fills any nil dependency fields with production defaults.
func WithDefaults(deps Dependencies) Dependencies {
	prod := Production()

	if deps.LoadProject == nil {
		deps.LoadProject = prod.LoadProject
	}
	if deps.LoadGlobal == nil {
		deps.LoadGlobal = prod.LoadGlobal
	}
	if deps.OpenStore == nil {
		deps.OpenStore = prod.OpenStore
	}
	if deps.NewRemote == nil {
		deps.NewRemote = prod.NewRemote
	}
	if deps.NewUploadManager == nil {
		deps.NewUploadManager = prod.NewUploadManager
	}
	if deps.NewDownloadManager == nil {
		deps.NewDownloadManager = prod.NewDownloadManager
	}
	if deps.NewWebSocket == nil {
		deps.NewWebSocket = prod.NewWebSocket
	}
	if deps.Now == nil {
		deps.Now = prod.Now
	}

	return deps
}

// NewRemoteClient creates a gomcapi client for the project's configured remote.
func NewRemoteClient(project config.Project, global config.Global) (RemoteClient, error) {
	remoteCfg, ok := global.FindRemote(project.Remote.Email, project.Remote.MCURL)
	if !ok {
		return nil, fmt.Errorf("remote %s %s is not configured in global config", project.Remote.Email, project.Remote.MCURL)
	}
	if remoteCfg.APIKey == "" {
		return nil, fmt.Errorf("remote %s %s is missing an API key", project.Remote.Email, project.Remote.MCURL)
	}

	return mcapi.NewClient(&mcapi.ClientArgs{
		APIKey:  remoteCfg.APIKey,
		BaseURL: remoteCfg.MCURL,
	}), nil
}

// Ensure gomcapi client satisfies the remote interface.
var _ RemoteClient = (*mcapi.Client)(nil)
