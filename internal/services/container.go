package services

import (
	"context"
	"fmt"
	"io"

	"github.com/materials-commons/mccli/internal/config"
	"github.com/materials-commons/mccli/internal/di"
	"github.com/materials-commons/mccli/internal/mc"
	"github.com/materials-commons/mccli/internal/transfer"
	"github.com/materials-commons/mccli/internal/wsclient"
)

// Container constructs and stores command services.
//
// Services can be eagerly initialized by passing options to NewContainer, or
// lazily initialized by calling the non-Must accessor methods.
type Container struct {
	deps di.Dependencies

	projectConfig config.Project
	globalConfig  config.Global
	projectSet    bool
	globalSet     bool

	projectRoot string

	store                 di.Store
	remote                di.RemoteClient
	projectPathTranslator mc.ProjectPathTranslator

	sendQueue *wsclient.Queue[wsclient.OutboundMessage]

	uploadManager   di.UploadManager
	downloadManager di.DownloadManager
	websocket       di.WebSocketRunner

	progressOut io.Writer

	uploadProgressWait func()
}

type ContainerOption func(*containerOptions)

type containerOptions struct {
	workingDir string

	loadProjectConfig         bool
	loadGlobalConfig          bool
	resolveProjectRoot        bool
	loadStore                 bool
	loadRemote                bool
	loadProjectPathTranslator bool
}

// WithWorkingDir sets the directory used to find the project config/root.
// If omitted, "." is used.
func WithWorkingDir(workingDir string) ContainerOption {
	return func(opts *containerOptions) {
		opts.workingDir = workingDir
	}
}

// WithProjectConfig loads the local project config.
func WithProjectConfig() ContainerOption {
	return func(opts *containerOptions) {
		opts.loadProjectConfig = true
	}
}

// WithGlobalConfig loads the global config.
func WithGlobalConfig() ContainerOption {
	return func(opts *containerOptions) {
		opts.loadGlobalConfig = true
	}
}

// WithProjectRoot resolves the local project root.
//
// This implies WithProjectConfig.
func WithProjectRoot() ContainerOption {
	return func(opts *containerOptions) {
		opts.resolveProjectRoot = true
		opts.loadProjectConfig = true
	}
}

// WithStore opens the local project store.
//
// This implies WithProjectRoot and WithProjectConfig.
func WithStore() ContainerOption {
	return func(opts *containerOptions) {
		opts.loadStore = true
		opts.resolveProjectRoot = true
		opts.loadProjectConfig = true
	}
}

// WithRemote creates the remote client.
//
// This implies WithProjectConfig and WithGlobalConfig.
func WithRemote() ContainerOption {
	return func(opts *containerOptions) {
		opts.loadRemote = true
		opts.loadProjectConfig = true
		opts.loadGlobalConfig = true
	}
}

// WithProjectPathTranslator creates the project path translator.
//
// This implies WithProjectRoot and WithProjectConfig.
func WithProjectPathTranslator() ContainerOption {
	return func(opts *containerOptions) {
		opts.loadProjectPathTranslator = true
		opts.resolveProjectRoot = true
		opts.loadProjectConfig = true
	}
}

// WithCommandServices loads the common command context pieces:
// project config, global config, and project root.
func WithCommandServices() ContainerOption {
	return func(opts *containerOptions) {
		opts.loadProjectConfig = true
		opts.loadGlobalConfig = true
		opts.resolveProjectRoot = true
	}
}

// WithDifferenceServices loads the common services used by commands that compare
// or synchronize local and remote state.
func WithDifferenceServices() ContainerOption {
	return func(opts *containerOptions) {
		opts.loadProjectConfig = true
		opts.loadGlobalConfig = true
		opts.resolveProjectRoot = true
		opts.loadStore = true
		opts.loadRemote = true
		opts.loadProjectPathTranslator = true
	}
}

// NewContainer creates and initializes a service container for the requested options.
func NewContainer(ctx context.Context, deps di.Dependencies, options ...ContainerOption) (*Container, error) {
	opts := containerOptions{
		workingDir: ".",
	}
	for _, option := range options {
		option(&opts)
	}

	c := &Container{
		deps: di.WithDefaults(deps),
	}

	if err := c.initialize(ctx, opts); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *Container) initialize(ctx context.Context, opts containerOptions) error {
	if opts.workingDir == "" {
		opts.workingDir = "."
	}

	if opts.loadProjectConfig {
		projectCfg, err := c.deps.LoadProjectConfig(ctx, opts.workingDir)
		if err != nil {
			return fmt.Errorf("load project config: %w", err)
		}

		c.projectConfig = projectCfg
		c.projectSet = true
	}

	if opts.resolveProjectRoot {
		if !c.projectSet {
			return fmt.Errorf("resolve project root: project config has not been loaded")
		}

		projectRoot := c.projectConfig.ProjectRoot()
		if projectRoot == "" {
			var err error
			projectRoot, err = mc.FindProjectRoot(ctx, opts.workingDir)
			if err != nil {
				return fmt.Errorf("resolve project root: %w", err)
			}
		}

		c.projectRoot = projectRoot
	}

	if opts.loadGlobalConfig {
		globalCfg, err := c.deps.LoadGlobalConfig(ctx, "")
		if err != nil {
			return fmt.Errorf("load global config: %w", err)
		}

		c.globalConfig = globalCfg
		c.globalSet = true
	}

	if opts.loadStore {
		if _, err := c.Store(ctx); err != nil {
			return fmt.Errorf("load store: %w", err)
		}
	}

	if opts.loadProjectPathTranslator {
		if _, err := c.ProjectPathTranslator(); err != nil {
			return fmt.Errorf("load project path translator: %w", err)
		}
	}

	if opts.loadRemote {
		if _, err := c.Remote(); err != nil {
			return fmt.Errorf("load remote: %w", err)
		}
	}

	return nil
}

// LoadCommandContext loads config and resolves the project root shared by most commands.
//
// Deprecated: prefer NewContainer(ctx, deps, WithWorkingDir(...), WithCommandServices()).
func (c *Container) LoadCommandContext(ctx context.Context, workingDir string) (*CommandContext, error) {
	if workingDir == "" {
		workingDir = "."
	}

	projectCfg, err := c.deps.LoadProjectConfig(ctx, workingDir)
	if err != nil {
		return nil, err
	}

	projectRoot := projectCfg.ProjectRoot()
	if projectRoot == "" {
		projectRoot, err = mc.FindProjectRoot(ctx, workingDir)
		if err != nil {
			return nil, err
		}
	}

	globalCfg, err := c.deps.LoadGlobalConfig(ctx, "")
	if err != nil {
		return nil, err
	}

	c.projectConfig = projectCfg
	c.globalConfig = globalCfg
	c.projectSet = true
	c.globalSet = true
	c.projectRoot = projectRoot

	return &CommandContext{
		Container:     c,
		ProjectConfig: projectCfg,
		GlobalConfig:  globalCfg,
		ProjectRoot:   projectRoot,
	}, nil
}

func (c *Container) ProjectConfig() (config.Project, error) {
	if !c.projectSet {
		return config.Project{}, fmt.Errorf("project config has not been loaded")
	}
	return c.projectConfig, nil
}

func (c *Container) MustProjectConfig() config.Project {
	if !c.projectSet {
		panic("services.Container: project config was not requested")
	}
	return c.projectConfig
}

func (c *Container) GlobalConfig() (config.Global, error) {
	if !c.globalSet {
		return config.Global{}, fmt.Errorf("global config has not been loaded")
	}
	return c.globalConfig, nil
}

func (c *Container) MustGlobalConfig() config.Global {
	if !c.globalSet {
		panic("services.Container: global config was not requested")
	}
	return c.globalConfig
}

func (c *Container) ProjectRoot() (string, error) {
	if c.projectRoot == "" {
		return "", fmt.Errorf("project root has not been resolved")
	}
	return c.projectRoot, nil
}

func (c *Container) MustProjectRoot() string {
	if c.projectRoot == "" {
		panic("services.Container: project root was not requested")
	}
	return c.projectRoot
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

func (c *Container) MustStore() di.Store {
	if c.store == nil {
		panic("services.Container: store was not requested")
	}
	return c.store
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

	remote, err := c.deps.NewRemoteClient(c.projectConfig, c.globalConfig)
	if err != nil {
		return nil, err
	}

	c.remote = remote
	return c.remote, nil
}

func (c *Container) MustRemote() di.RemoteClient {
	if c.remote == nil {
		panic("services.Container: remote was not requested")
	}
	return c.remote
}

// RequireRemoteAs returns the configured remote client as the requested interface type.
//
// This should be used after constructing the container with WithRemote or another
// option set that implies WithRemote, such as WithDifferenceServices.
func RequireRemoteAs[T any](c *Container, name string) (T, error) {
	var zero T

	remote := c.MustRemote()
	typed, ok := remote.(T)
	if !ok {
		return zero, fmt.Errorf("remote does not implement %s", name)
	}

	return typed, nil
}

func (c *Container) ProjectPathTranslator() (mc.ProjectPathTranslator, error) {
	if c.projectPathTranslator.ProjectRoot() != "" {
		return c.projectPathTranslator, nil
	}

	if c.projectRoot == "" {
		return mc.ProjectPathTranslator{}, fmt.Errorf("project root has not been resolved")
	}

	translator, err := mc.NewProjectPathTranslator(c.projectRoot)
	if err != nil {
		return mc.ProjectPathTranslator{}, err
	}

	c.projectPathTranslator = translator
	return c.projectPathTranslator, nil
}

func (c *Container) MustProjectPathTranslator() mc.ProjectPathTranslator {
	if c.projectPathTranslator.ProjectRoot() == "" {
		panic("services.Container: project path translator was not requested")
	}
	return c.projectPathTranslator
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

	progressFactory := transfer.NewMPBProgressFactory(opts.Out)
	progress := transfer.NewUploadProgress(progressFactory)
	c.uploadProgressWait = progress.Wait

	manager, err := c.deps.NewUploadManager(transfer.UploadConfig{
		SendQueue:     c.SendQueue(),
		Store:         store,
		ClientID:      c.globalConfig.ClientUUID,
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

	progressFactory := transfer.NewMPBProgressFactory(opts.Out)
	progress := transfer.NewUploadProgress(progressFactory)
	c.uploadProgressWait = progress.Wait

	manager, err := c.deps.NewDownloadManager(transfer.DownloadConfig{
		Store:         store,
		ClientID:      c.globalConfig.ClientUUID,
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

	remoteCfg, err := RequireConfiguredRemote(c.projectConfig, c.globalConfig)
	if err != nil {
		return nil, err
	}

	wsURL := opts.URL
	if wsURL == "" {
		wsURL, err = config.ToWebSocketURLFromRemoteURL(c.projectConfig.Remote.MCURL)
		if err != nil {
			return nil, fmt.Errorf("invalid remote MCURL (%s) can't construct websocket URL: %w", c.projectConfig.Remote.MCURL, err)
		}
	}

	c.websocket = c.deps.NewWebSocket(di.WebSocketConfig{
		URL:        wsURL,
		Token:      remoteCfg.APIKey,
		ClientID:   c.globalConfig.ClientUUID,
		Outbound:   c.SendQueue(),
		Handle:     opts.Handle,
		ProjectIDs: []int{c.projectConfig.ProjectID},
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
