// Package ls implements the mc2 ls command.
package ls

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	mcapi "github.com/materials-commons/gomcapi"
	"github.com/materials-commons/mccli/pkg/config"
	"github.com/materials-commons/mccli/pkg/filedb"
	"github.com/materials-commons/mccli/pkg/projectpath"
	"github.com/materials-commons/mccli/pkg/reconcile"
)

// Options contains user-facing mc2 ls command options.
type Options struct {
	// WorkingDir is the directory used to resolve relative path arguments and to
	// discover the current Materials Commons project.
	WorkingDir string

	// Paths are file or directory paths to list. If empty, WorkingDir is listed.
	Paths []string

	// Action switches output to action/reason rows.
	Action bool

	// Out receives command output. If nil, os.Stdout is used.
	Out io.Writer
}

// RecordStore is the subset of filedb.Store used by ls.
type RecordStore interface {
	reconcile.DirectoryRecordGetter
	reconcile.FileRecordGetter
	Close(ctx context.Context) error
}

// RemoteClient is the subset of gomcapi.Client used by ls.
type RemoteClient interface {
	reconcile.RemoteDirectoryLister
	reconcile.RemoteFileGetter
}

// Dependencies contains injectable command dependencies.
type Dependencies struct {
	LoadProject func(ctx context.Context, start string) (config.Project, error)
	LoadGlobal  func(ctx context.Context, path string) (config.Global, error)
	OpenStore   func(ctx context.Context, projectRoot string) (RecordStore, error)
	NewRemote   func(project config.Project, global config.Global) (RemoteClient, error)
	Now         func() time.Time
}

// Runner executes ls with injected dependencies.
type Runner struct {
	Deps Dependencies
}

// Run executes mc2 ls using production dependencies.
func Run(ctx context.Context, opts Options) error {
	return Runner{Deps: ProductionDependencies()}.Run(ctx, opts)
}

// ProductionDependencies returns the default command dependencies.
func ProductionDependencies() Dependencies {
	return Dependencies{
		LoadProject: config.LoadProject,
		LoadGlobal:  config.LoadGlobal,
		OpenStore: func(ctx context.Context, projectRoot string) (RecordStore, error) {
			return filedb.Open(ctx, projectRoot)
		},
		NewRemote: newRemoteClient,
		Now:       time.Now,
	}
}

// Run executes the ls command.
func (r Runner) Run(ctx context.Context, opts Options) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	opts = normalizeOptions(opts)

	deps := r.Deps
	if deps.LoadProject == nil {
		deps.LoadProject = config.LoadProject
	}
	if deps.LoadGlobal == nil {
		deps.LoadGlobal = config.LoadGlobal
	}
	if deps.OpenStore == nil {
		deps.OpenStore = func(ctx context.Context, projectRoot string) (RecordStore, error) {
			return filedb.Open(ctx, projectRoot)
		}
	}
	if deps.NewRemote == nil {
		deps.NewRemote = newRemoteClient
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}

	projectCfg, err := deps.LoadProject(ctx, opts.WorkingDir)
	if err != nil {
		return err
	}

	projectRoot := projectCfg.ProjectRoot()
	if projectRoot == "" {
		projectRoot, err = projectpath.FindRoot(ctx, opts.WorkingDir)
		if err != nil {
			return err
		}
	}

	globalCfg, err := deps.LoadGlobal(ctx, "")
	if err != nil {
		return err
	}

	store, err := deps.OpenStore(ctx, projectCfg.ProjectRoot())
	if err != nil {
		return err
	}
	defer func() {
		_ = store.Close(ctx)
	}()

	remote, err := deps.NewRemote(projectCfg, globalCfg)
	if err != nil {
		return err
	}

	translator, err := projectpath.New(projectCfg.ProjectRoot())
	if err != nil {
		return err
	}

	reconciler := reconcile.New(reconcile.ModeStatus).WithChecksumFunc(func(ctx context.Context, localPath string) (string, error) {
		return "", fmt.Errorf("internal error: mc2 ls must not compute checksums")
	})

	for _, inputPath := range opts.Paths {
		localPath, err := resolveInputPath(opts.WorkingDir, inputPath)
		if err != nil {
			return err
		}

		if err := r.listPath(ctx, listRequest{
			opts:       opts,
			project:    projectCfg,
			translator: translator,
			store:      store,
			remote:     remote,
			reconciler: reconciler,
			localPath:  localPath,
			now:        deps.Now,
		}); err != nil {
			return err
		}
	}

	return nil
}

type listRequest struct {
	opts       Options
	project    config.Project
	translator projectpath.Translator
	store      RecordStore
	remote     RemoteClient
	reconciler *reconcile.Reconciler
	localPath  string
	now        func() time.Time
}

func (r Runner) listPath(ctx context.Context, req listRequest) error {
	info, err := os.Stat(req.localPath)
	if err == nil && info.IsDir() {
		return listDirectory(ctx, req)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat %q: %w", req.localPath, err)
	}

	return listSinglePath(ctx, req)
}

func listDirectory(ctx context.Context, req listRequest) error {
	localList := reconcile.LocalNodeListDir(req.translator, req.now)
	remoteList := reconcile.RemoteOnlyListDir(req.project.ProjectID, req.translator, req.remote)
	mergedList := reconcile.MergedNodeListDir(req.translator, localList, remoteList)

	node := reconcile.WalkNode{
		LocalPath: req.localPath,
	}

	remotePath, err := req.translator.LocalToRemote(req.localPath)
	if err != nil {
		return err
	}
	node.RemotePath = remotePath

	options := reconcile.WalkOptions{
		Recursive:  false,
		Ignore:     nil,
		Translator: req.translator,
	}

	return reconcile.WalkNodesAndReconcile(
		ctx,
		node,
		mergedList,
		req.store,
		req.reconciler,
		options,
		func(ctx context.Context, node reconcile.WalkNode, states map[string]reconcile.FileState) error {
			return printStates(req.opts.Out, states, req.opts.Action)
		},
	)
}

func listSinglePath(ctx context.Context, req listRequest) error {
	runner := reconcile.NewObservationRunner(
		req.project.ProjectID,
		req.translator,
		req.store,
		req.remote,
		reconcile.ModeStatus,
	)
	runner.Reconciler = req.reconciler
	runner.Now = req.now

	state, err := runner.ObserveAndReconcile(ctx, req.localPath)
	if err != nil {
		return err
	}

	if state.Observation.LocalEntry == nil && state.Observation.RemoteEntry == nil {
		rel, relErr := filepath.Rel(req.opts.WorkingDir, req.localPath)
		if relErr != nil {
			rel = req.localPath
		}
		fmt.Fprintf(req.opts.Out, "%s: No such file or directory\n", rel)
		return nil
	}

	return printStates(req.opts.Out, map[string]reconcile.FileState{
		state.Observation.Name: state,
	}, req.opts.Action)
}

func normalizeOptions(opts Options) Options {
	if opts.WorkingDir == "" {
		opts.WorkingDir = "."
	}
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	if len(opts.Paths) == 0 {
		opts.Paths = []string{opts.WorkingDir}
	}
	return opts
}

func resolveInputPath(workingDir, inputPath string) (string, error) {
	if inputPath == "" {
		inputPath = "."
	}

	if filepath.IsAbs(inputPath) {
		return filepath.Clean(inputPath), nil
	}

	return filepath.Abs(filepath.Join(workingDir, inputPath))
}

func newRemoteClient(project config.Project, global config.Global) (RemoteClient, error) {
	remoteCfg, ok := global.FindRemote(project.Remote.Email, project.Remote.MCURL)
	if !ok {
		return nil, fmt.Errorf("remote %s %s is not configured in global config", project.Remote.Email, project.Remote.MCURL)
	}
	if remoteCfg.APIKey == "" {
		return nil, fmt.Errorf("remote %s %s is missing an API key", project.Remote.Email, project.Remote.MCURL)
	}

	fmt.Printf("APIKey %s, BaseURL %s\n", remoteCfg.APIKey, remoteCfg.MCURL)
	return mcapi.NewClient(&mcapi.ClientArgs{
		APIKey:  remoteCfg.APIKey,
		BaseURL: remoteCfg.MCURL,
	}), nil
}

func sortedStates(states map[string]reconcile.FileState) []reconcile.FileState {
	names := make([]string, 0, len(states))
	for name := range states {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]reconcile.FileState, 0, len(names))
	for _, name := range names {
		out = append(out, states[name])
	}
	return out
}

func remoteFileIDString(entry *reconcile.RemoteEntry) string {
	if entry == nil || entry.RemoteFileID == nil {
		return "-"
	}
	return fmt.Sprintf("%d", *entry.RemoteFileID)
}

func kindCode(kind reconcile.Kind) string {
	switch kind {
	case reconcile.KindFile:
		return "F"
	case reconcile.KindDir:
		return "D"
	default:
		return "-"
	}
}

func localRemoteLabel(state reconcile.FileState) string {
	hasLocal := state.Observation.LocalEntry != nil
	hasRemote := state.Observation.RemoteEntry != nil

	switch {
	case hasLocal && hasRemote:
		return "L/R"
	case hasLocal:
		return "L"
	case hasRemote:
		return "R"
	default:
		return "-"
	}
}

func displayAction(action reconcile.Action) string {
	if action == reconcile.ActionDBUpdate {
		return "preserve"
	}
	return string(action)
}
