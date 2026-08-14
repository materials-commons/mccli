package services

import (
	"context"
	"fmt"
	"io"

	"github.com/materials-commons/mccli/pkg/config"
	"github.com/materials-commons/mccli/pkg/di"
	"github.com/materials-commons/mccli/pkg/download"
	"github.com/materials-commons/mccli/pkg/projectpath"
	"github.com/materials-commons/mccli/pkg/upload"
	"github.com/materials-commons/mccli/pkg/wsclient"
)

// Container lazily constructs command services.
//
// It is intentionally small and explicit. Tests can inject di.Dependencies and
// assert which services were actually requested.
type Container struct {
	deps di.Dependencies

	project    config.Project
	global     config.Global
	projectSet bool
	globalSet  bool

	projectRoot string

	store      di.Store
	remote     di.RemoteClient
	translator projectpath.Translator

	sendQueue *wsclient.Queue[wsclient.OutboundMessage]

	uploadManager   di.UploadManager
	downloadManager di.DownloadManager
	websocket       di.WebSocketRunner

	progressOut io.Writer

	uploadProgressWait func()
}

// NewContainer creates a lazy service container.
func NewContainer(deps di.Dependencies) *Container {
	return &Container{
		deps: di.WithDefaults(deps),
	}
}

// LoadCommandContext loads config and resolves the project root shared by most commands.
func (c *Container) LoadCommandContext(ctx context.Context, workingDir string) (*CommandContext, error) {
	if workingDir == "" {
		workingDir = "."
	}

	projectCfg, err := c.deps.LoadProject(ctx, workingDir)
	if err != nil {
		return nil, err
	}

	projectRoot := projectCfg.ProjectRoot()
	if projectRoot == "" {
		projectRoot, err = projectpath.FindRoot(ctx, workingDir)
		if err != nil {
			return nil, err
		}
	}

	globalCfg, err := c.deps.LoadGlobal(ctx, "")
	if err != nil {
		return nil, err
	}

	c.project = projectCfg
	c.global = globalCfg
	c.projectSet = true
	c.globalSet = true
	c.projectRoot = projectRoot

	return &CommandContext{
		Container:   c,
		Project:     projectCfg,
		Global:      globalCfg,
		ProjectRoot: projectRoot,
	}, nil
}

func (c *Container) Project() (config.Project, error) {
	if !c.projectSet {
		return config.Project{}, fmt.Errorf("project config has not been loaded")
	}
	return c.project, nil
}

func (c *Container) Global() (config.Global, error) {
	if !c.globalSet {
		return config.Global{}, fmt.Errorf("global config has not been loaded")
	}
	return c.global, nil
}

func (c *Container) Store(ctx context.Context) (di.Store, error) {
	if c.store != nil {
		return c.store, nil
	}
	if c.projectRoot == "" {
		return nil, fmt.Errorf("project root has not been resolved")
	}

	store, err := c.deps.OpenStore(ctx, c.projectRoot)
	if err != nil {
		return nil, err
	}

	c.store = store
	return c.store, nil
}

func (c *Container) Remote() (di.RemoteClient, error) {
	if c.remote != nil {
		return c.remote, nil
	}
	if !c.projectSet {
		return nil, fmt.Errorf("project config has not been loaded")
	}
	if !c.globalSet {
		return nil, fmt.Errorf("global config has not been loaded")
	}

	remote, err := c.deps.NewRemote(c.project, c.global)
	if err != nil {
		return nil, err
	}

	c.remote = remote
	return c.remote, nil
}

func (c *Container) Translator() (projectpath.Translator, error) {
	if c.projectRoot == "" {
		return projectpath.Translator{}, fmt.Errorf("project root has not been resolved")
	}

	translator, err := projectpath.New(c.projectRoot)
	if err != nil {
		return projectpath.Translator{}, err
	}

	c.translator = translator
	return c.translator, nil
}

func (c *Container) SendQueue() *wsclient.Queue[wsclient.OutboundMessage] {
	if c.sendQueue == nil {
		c.sendQueue = wsclient.NewQueue[wsclient.OutboundMessage]()
	}
	return c.sendQueue
}

type UploadManagerOptions struct {
	Out           io.Writer
	MaxConcurrent int
}

func (c *Container) UploadManager(ctx context.Context, opts UploadManagerOptions) (di.UploadManager, error) {
	if c.uploadManager != nil {
		return c.uploadManager, nil
	}

	store, err := c.Store(ctx)
	if err != nil {
		return nil, err
	}
	if !c.globalSet {
		return nil, fmt.Errorf("global config has not been loaded")
	}

	maxConcurrent := opts.MaxConcurrent
	if maxConcurrent == 0 {
		maxConcurrent = 3
	}

	progressFactory := upload.NewMPBProgressFactory(opts.Out)
	progress := upload.NewUploadProgress(progressFactory)
	c.uploadProgressWait = progress.Wait

	manager, err := c.deps.NewUploadManager(upload.Config{
		SendQueue:     c.SendQueue(),
		Store:         store,
		ClientID:      c.global.ClientUUID,
		MaxConcurrent: maxConcurrent,
		Progress:      progress,
	})
	if err != nil {
		return nil, err
	}

	c.uploadManager = manager
	return c.uploadManager, nil
}

type DownloadManagerOptions struct {
	Out           io.Writer
	MaxConcurrent int
}

func (c *Container) DownloadManager(ctx context.Context, opts DownloadManagerOptions) (di.DownloadManager, error) {
	if c.downloadManager != nil {
		return c.downloadManager, nil
	}

	store, err := c.Store(ctx)
	if err != nil {
		return nil, err
	}
	if !c.globalSet {
		return nil, fmt.Errorf("global config has not been loaded")
	}

	maxConcurrent := opts.MaxConcurrent
	if maxConcurrent == 0 {
		maxConcurrent = 3
	}

	progressFactory := upload.NewMPBProgressFactory(opts.Out)
	progress := upload.NewUploadProgress(progressFactory)
	c.uploadProgressWait = progress.Wait

	manager, err := c.deps.NewDownloadManager(download.Config{
		Store:         store,
		ClientID:      c.global.ClientUUID,
		MaxConcurrent: maxConcurrent,
		Progress:      progress,
	})
	if err != nil {
		return nil, err
	}

	c.downloadManager = manager
	return c.downloadManager, nil
}

type WebSocketOptions struct {
	URL    string
	Handle wsclient.Handler
}

func (c *Container) WebSocket(opts WebSocketOptions) (di.WebSocketRunner, error) {
	if c.websocket != nil {
		return c.websocket, nil
	}
	if !c.projectSet {
		return nil, fmt.Errorf("project config has not been loaded")
	}
	if !c.globalSet {
		return nil, fmt.Errorf("global config has not been loaded")
	}

	remoteCfg, err := RequireConfiguredRemote(c.project, c.global)
	if err != nil {
		return nil, err
	}

	wsURL := opts.URL
	if wsURL == "" {
		wsURL, err = config.ToWebSocketURLFromRemoteURL(c.project.Remote.MCURL)
		if err != nil {
			return nil, fmt.Errorf("invalid remote MCURL (%s) can't construct websocket URL: %w", c.project.Remote.MCURL, err)
		}
	}

	c.websocket = c.deps.NewWebSocket(di.WebSocketConfig{
		URL:        wsURL,
		Token:      remoteCfg.APIKey,
		ClientID:   c.global.ClientUUID,
		Outbound:   c.SendQueue(),
		Handle:     opts.Handle,
		ProjectIDs: []int{c.project.ProjectID},
	})

	return c.websocket, nil
}

func (c *Container) Close(ctx context.Context) error {
	if c.uploadProgressWait != nil {
		c.uploadProgressWait()
		c.uploadProgressWait = nil
	}

	if c.store != nil {
		err := c.store.Close(ctx)
		c.store = nil
		return err
	}

	return nil
}
