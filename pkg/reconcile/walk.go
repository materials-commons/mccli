package reconcile

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"time"

	mcapi "github.com/materials-commons/gomcapi"
	"github.com/materials-commons/hydra/pkg/mcdb/mcmodel"
	"github.com/materials-commons/mccli/pkg/filedb"
	"github.com/materials-commons/mccli/pkg/projectpath"
)

// IgnoreFunc reports whether pathValue should be skipped.
//
// pathValue is a local filesystem path when one exists. For remote-only entries,
// pathValue is the synthesized local path where that remote path would be
// materialized.
type IgnoreFunc func(pathValue string, isDir bool) bool

// ListDirFunc lists one local directory and returns observations for its
// immediate children.
type ListDirFunc func(ctx context.Context, localDir string) ([]Observation, error)

// WalkFunc is called once for each listed local directory.
type WalkFunc func(ctx context.Context, localDir string, observations []Observation) error

// WalkNode identifies one directory in a project walk.
//
// LocalPath is the local filesystem path for the node when it exists or can be
// synthesized. RemotePath is the Materials Commons project path for the same
// node. RemotePath always starts with "/".
type WalkNode struct {
	LocalPath  string
	RemotePath string
}

// NodeListDirFunc lists one walk node and returns observations for its immediate
// children.
type NodeListDirFunc func(ctx context.Context, node WalkNode) ([]Observation, error)

// WalkNodeFunc is called once for each listed node.
type WalkNodeFunc func(ctx context.Context, node WalkNode, observations []Observation) error

// RemoteDirectoryLister lists a remote Materials Commons project directory.
//
// *mcapi.Client satisfies this interface.
type RemoteDirectoryLister interface {
	ListDirectoryByPath(projectID int, remotePath string) ([]mcmodel.File, error)
}

// DirectoryRecordGetter loads persisted file state for a remote directory path.
type DirectoryRecordGetter interface {
	ListByDir(ctx context.Context, dir string) ([]filedb.FileRecord, error)
}

// WalkOptions configures Walk and WalkNodes.
type WalkOptions struct {
	Recursive bool
	Ignore    IgnoreFunc

	// Translator is used to synthesize local paths for remote-only entries
	// during node-based recursive walks. It is optional for local-only walks,
	// but recommended for remote-only and merged walks.
	Translator projectpath.Translator
}

// Walk walks a directory tree using listDir to observe each directory.
//
// Walk is local-path oriented. Use WalkNodes for remote-only or mixed local/
// remote walks.
func Walk(ctx context.Context, root string, listDir ListDirFunc, options WalkOptions, fn WalkFunc) error {
	if listDir == nil {
		return fmt.Errorf("list directory function is required")
	}
	if fn == nil {
		return fmt.Errorf("walk callback is required")
	}

	return WalkNodes(ctx, WalkNode{LocalPath: root}, func(ctx context.Context, node WalkNode) ([]Observation, error) {
		return listDir(ctx, node.LocalPath)
	}, options, func(ctx context.Context, node WalkNode, observations []Observation) error {
		return fn(ctx, node.LocalPath, observations)
	})
}

// WalkNodes walks a directory tree using explicit local/remote walk nodes.
//
// This supports local-only, remote-only, and merged local/remote recursive
// traversal.
func WalkNodes(ctx context.Context, root WalkNode, listDir NodeListDirFunc, options WalkOptions, fn WalkNodeFunc) error {
	if listDir == nil {
		return fmt.Errorf("node list directory function is required")
	}
	if fn == nil {
		return fmt.Errorf("walk node callback is required")
	}

	stack := []WalkNode{root}

	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}

		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if options.Ignore != nil && current.LocalPath != "" && options.Ignore(current.LocalPath, true) {
			continue
		}

		observations, err := listDir(ctx, current)
		if err != nil {
			return err
		}

		filtered := filterObservations(observations, options.Ignore, options.Translator)

		if err := fn(ctx, current, filtered); err != nil {
			return err
		}

		if !options.Recursive {
			continue
		}

		for i := len(filtered) - 1; i >= 0; i-- {
			obs := filtered[i]
			if !observationIsDir(obs) {
				continue
			}

			child, err := walkNodeFromObservation(obs, options.Translator)
			if err != nil {
				return err
			}
			if child.LocalPath == "" && child.RemotePath == "" {
				continue
			}

			stack = append(stack, child)
		}
	}

	return nil
}

// LocalListDir returns a ListDirFunc that lists local filesystem entries.
func LocalListDir(translator projectpath.Translator, now func() time.Time) ListDirFunc {
	if now == nil {
		now = time.Now
	}

	return func(ctx context.Context, localDir string) ([]Observation, error) {
		entries, err := os.ReadDir(localDir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, nil
			}
			return nil, fmt.Errorf("list local directory %q: %w", localDir, err)
		}

		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name() < entries[j].Name()
		})

		observations := make([]Observation, 0, len(entries))
		for _, entry := range entries {
			localPath := filepath.Join(localDir, entry.Name())

			localEntry, err := ObserveLocal(ctx, translator, localPath, now())
			if err != nil {
				return nil, err
			}
			if localEntry == nil {
				continue
			}

			observations = append(observations, Observation{
				RemotePath: localEntry.RemotePath,
				Name:       localEntry.Name,
				Dir:        localEntry.Dir,
				LocalEntry: localEntry,
			})
		}

		return observations, nil
	}
}

// LocalNodeListDir adapts LocalListDir for WalkNodes.
func LocalNodeListDir(translator projectpath.Translator, now func() time.Time) NodeListDirFunc {
	local := LocalListDir(translator, now)

	return func(ctx context.Context, node WalkNode) ([]Observation, error) {
		if node.LocalPath == "" {
			return nil, nil
		}
		return local(ctx, node.LocalPath)
	}
}

// RemoteListDir returns a ListDirFunc that lists remote Materials Commons
// directory entries by translating localDir to its remote project path.
func RemoteListDir(projectID int, translator projectpath.Translator, remote RemoteDirectoryLister) ListDirFunc {
	remoteOnly := RemoteOnlyListDir(projectID, translator, remote)

	return func(ctx context.Context, localDir string) ([]Observation, error) {
		remoteDir, err := translator.LocalToRemote(localDir)
		if err != nil {
			return nil, err
		}

		return remoteOnly(ctx, WalkNode{
			LocalPath:  localDir,
			RemotePath: remoteDir,
		})
	}
}

// RemoteOnlyListDir returns a NodeListDirFunc that lists remote Materials
// Commons directory entries using WalkNode.RemotePath.
//
// This function supports remote-only recursive walking.
func RemoteOnlyListDir(projectID int, translator projectpath.Translator, remote RemoteDirectoryLister) NodeListDirFunc {
	return func(ctx context.Context, node WalkNode) ([]Observation, error) {
		if remote == nil {
			return nil, fmt.Errorf("remote directory lister is required")
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		remoteDir := node.RemotePath
		if remoteDir == "" {
			if node.LocalPath == "" {
				return nil, fmt.Errorf("walk node requires local or remote path")
			}

			var err error
			remoteDir, err = translator.LocalToRemote(node.LocalPath)
			if err != nil {
				return nil, err
			}
		}

		files, err := remote.ListDirectoryByPath(projectID, remoteDir)
		if err != nil {
			if IsRemoteNotFound(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("list remote directory %q: %w", remoteDir, err)
		}

		observations := make([]Observation, 0, len(files))
		for i := range files {
			remoteEntry, err := RemoteEntryFromMCFile(&files[i])
			if err != nil {
				return nil, err
			}
			if remoteEntry == nil {
				continue
			}

			observations = append(observations, Observation{
				RemotePath:  remoteEntry.Path,
				Name:        remoteEntry.Name,
				Dir:         remoteEntry.Dir,
				RemoteEntry: remoteEntry,
				// LocalEntry intentionally remains nil for remote-only entries.
				// The corresponding local path is synthesized later in WalkNode
				// traversal using WalkOptions.Translator.
				LocalEntry: nil,
			})
		}

		sortObservationsByName(observations)

		return observations, nil
	}
}

// MergedListDir returns a ListDirFunc that merges local and remote directory
// entries by name.
func MergedListDir(translator projectpath.Translator, localListDir ListDirFunc, remoteListDir ListDirFunc) ListDirFunc {
	mergedNode := MergedNodeListDir(
		translator,
		func(ctx context.Context, node WalkNode) ([]Observation, error) {
			if node.LocalPath == "" {
				return nil, nil
			}
			return localListDir(ctx, node.LocalPath)
		},
		func(ctx context.Context, node WalkNode) ([]Observation, error) {
			if node.LocalPath == "" {
				return nil, nil
			}
			return remoteListDir(ctx, node.LocalPath)
		},
	)

	return func(ctx context.Context, localDir string) ([]Observation, error) {
		remoteDir, err := translator.LocalToRemote(localDir)
		if err != nil {
			return nil, err
		}

		return mergedNode(ctx, WalkNode{
			LocalPath:  localDir,
			RemotePath: remoteDir,
		})
	}
}

// MergedNodeListDir returns a NodeListDirFunc that merges local and remote
// directory entries by name.
//
// Unlike MergedListDir, this supports remote-only nodes because the remote side
// can list using WalkNode.RemotePath even when WalkNode.LocalPath does not exist.
func MergedNodeListDir(translator projectpath.Translator, localListDir NodeListDirFunc, remoteListDir NodeListDirFunc) NodeListDirFunc {
	return func(ctx context.Context, node WalkNode) ([]Observation, error) {
		if localListDir == nil {
			return nil, fmt.Errorf("local node list directory function is required")
		}
		if remoteListDir == nil {
			return nil, fmt.Errorf("remote node list directory function is required")
		}

		if node.RemotePath == "" {
			if node.LocalPath == "" {
				return nil, fmt.Errorf("walk node requires local or remote path")
			}

			remotePath, err := translator.LocalToRemote(node.LocalPath)
			if err != nil {
				return nil, err
			}
			node.RemotePath = remotePath
		}

		if node.LocalPath == "" {
			localPath, err := translator.RemoteToLocal(node.RemotePath)
			if err != nil {
				return nil, err
			}
			node.LocalPath = localPath
		}

		localObservations, err := localListDir(ctx, node)
		if err != nil {
			return nil, err
		}

		remoteObservations, err := remoteListDir(ctx, node)
		if err != nil {
			return nil, err
		}

		localByName := map[string]Observation{}
		for _, obs := range localObservations {
			localByName[obs.Name] = obs
		}

		remoteByName := map[string]Observation{}
		for _, obs := range remoteObservations {
			remoteByName[obs.Name] = obs
		}

		names := make([]string, 0, len(localByName)+len(remoteByName))
		seen := map[string]bool{}

		for name := range localByName {
			if !seen[name] {
				names = append(names, name)
				seen[name] = true
			}
		}
		for name := range remoteByName {
			if !seen[name] {
				names = append(names, name)
				seen[name] = true
			}
		}

		sort.Strings(names)

		merged := make([]Observation, 0, len(names))
		for _, name := range names {
			localObs, hasLocal := localByName[name]
			remoteObs, hasRemote := remoteByName[name]

			remotePath := path.Join(node.RemotePath, name)
			if node.RemotePath == "/" {
				remotePath = "/" + name
			}

			obs := Observation{
				RemotePath: remotePath,
				Name:       name,
				Dir:        node.RemotePath,
			}

			if hasLocal {
				obs.LocalEntry = localObs.LocalEntry
				obs.RemotePath = localObs.RemotePath
				obs.Dir = localObs.Dir
			}

			if hasRemote {
				obs.RemoteEntry = remoteObs.RemoteEntry
				obs.RemotePath = remoteObs.RemotePath
				obs.Dir = remoteObs.Dir
			}

			merged = append(merged, obs)
		}

		return merged, nil
	}
}

// WalkAndReconcile walks from root using listDir and reconciles each observed
// entry.
//
// The callback is invoked once per directory with a map keyed by entry name.
func WalkAndReconcile(
	ctx context.Context,
	root string,
	listDir ListDirFunc,
	translator projectpath.Translator,
	records DirectoryRecordGetter,
	reconciler *Reconciler,
	options WalkOptions,
	fn func(ctx context.Context, localDir string, states map[string]FileState) error,
) error {
	remoteRoot, err := translator.LocalToRemote(root)
	if err != nil {
		return err
	}

	return WalkNodesAndReconcile(
		ctx,
		WalkNode{LocalPath: root, RemotePath: remoteRoot},
		func(ctx context.Context, node WalkNode) ([]Observation, error) {
			return listDir(ctx, node.LocalPath)
		},
		records,
		reconciler,
		options,
		func(ctx context.Context, node WalkNode, states map[string]FileState) error {
			return fn(ctx, node.LocalPath, states)
		},
	)
}

// WalkNodesAndReconcile walks from root using node-aware listDir and reconciles
// each observed entry.
//
// This supports remote-only recursive reconciliation.
func WalkNodesAndReconcile(
	ctx context.Context,
	root WalkNode,
	listDir NodeListDirFunc,
	records DirectoryRecordGetter,
	reconciler *Reconciler,
	options WalkOptions,
	fn func(ctx context.Context, node WalkNode, states map[string]FileState) error,
) error {
	if records == nil {
		return fmt.Errorf("directory record getter is required")
	}
	if reconciler == nil {
		return fmt.Errorf("reconciler is required")
	}
	if fn == nil {
		return fmt.Errorf("walk reconcile callback is required")
	}

	return WalkNodes(ctx, root, listDir, options, func(ctx context.Context, node WalkNode, observations []Observation) error {
		remoteDir := node.RemotePath
		if remoteDir == "" {
			return fmt.Errorf("walk node missing remote path")
		}

		fileRecords, err := records.ListByDir(ctx, remoteDir)
		if err != nil {
			return fmt.Errorf("list file records by dir %q: %w", remoteDir, err)
		}

		recordsByName := make(map[string]filedb.FileRecord, len(fileRecords))
		for _, record := range fileRecords {
			recordsByName[record.Name] = record
		}

		states := make(map[string]FileState, len(observations))
		for _, obs := range observations {
			record, ok := recordsByName[obs.Name]
			if ok {
				recordCopy := record
				obs.FileRecord = &recordCopy
			}

			decision, err := reconciler.Reconcile(ctx, obs)
			if err != nil {
				return fmt.Errorf("reconcile %q: %w", obs.RemotePath, err)
			}

			states[obs.Name] = FileState{
				Observation: obs,
				Decision:    decision,
			}
		}

		return fn(ctx, node, states)
	})
}

// DefaultIgnore reports whether pathValue should always be ignored.
func DefaultIgnore(pathValue string, isDir bool) bool {
	name := filepath.Base(pathValue)
	return name == ".DS_Store" || name == ".mc"
}

// ChainIgnore combines DefaultIgnore with an optional caller-supplied ignore
// function.
func ChainIgnore(extra IgnoreFunc) IgnoreFunc {
	return func(pathValue string, isDir bool) bool {
		if DefaultIgnore(pathValue, isDir) {
			return true
		}
		if extra == nil {
			return false
		}
		return extra(pathValue, isDir)
	}
}

func filterObservations(observations []Observation, ignore IgnoreFunc, translator projectpath.Translator) []Observation {
	filtered := make([]Observation, 0, len(observations))

	for _, obs := range observations {
		localPath := observationLocalPath(obs)
		if localPath == "" && obs.RemotePath != "" && translator.ProjectRoot() != "" {
			if synthesized, err := translator.RemoteToLocal(obs.RemotePath); err == nil {
				localPath = synthesized
			}
		}

		isDir := observationIsDir(obs)

		if localPath != "" && DefaultIgnore(localPath, isDir) {
			continue
		}

		if ignore != nil && localPath != "" && ignore(localPath, isDir) {
			continue
		}

		filtered = append(filtered, obs)
	}

	sortObservationsByName(filtered)

	return filtered
}

func observationLocalPath(obs Observation) string {
	if obs.LocalEntry != nil {
		return obs.LocalEntry.Path
	}

	return ""
}

func observationIsDir(obs Observation) bool {
	if obs.LocalEntry != nil && obs.LocalEntry.Kind == KindDir {
		return true
	}
	return obs.RemoteEntry != nil && obs.RemoteEntry.Kind == KindDir
}

func walkNodeFromObservation(obs Observation, translator projectpath.Translator) (WalkNode, error) {
	node := WalkNode{
		RemotePath: obs.RemotePath,
	}

	if obs.LocalEntry != nil {
		node.LocalPath = obs.LocalEntry.Path
	}
	if obs.RemoteEntry != nil && node.RemotePath == "" {
		node.RemotePath = obs.RemoteEntry.Path
	}

	if node.LocalPath == "" && node.RemotePath != "" && translator.ProjectRoot() != "" {
		localPath, err := translator.RemoteToLocal(node.RemotePath)
		if err != nil {
			return WalkNode{}, err
		}
		node.LocalPath = localPath
	}

	return node, nil
}

func sortObservationsByName(observations []Observation) {
	sort.Slice(observations, func(i, j int) bool {
		return observations[i].Name < observations[j].Name
	})
}

// IsRemoteNotFound reports whether err is a Materials Commons 404 API error.
func IsRemoteNotFound(err error) bool {
	var apiErr *mcapi.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusNotFound
	}

	return false
}
