package reconcile

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	mcapi "github.com/materials-commons/gomcapi"
	"github.com/materials-commons/hydra/pkg/mcdb/mcmodel"
	"github.com/materials-commons/mccli/pkg/filedb"
)

func TestLocalListDir(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	writeTestFile(t, filepath.Join(projectRoot, "a.txt"), "a")
	if err := os.Mkdir(filepath.Join(projectRoot, "Dir1"), 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	translator := mustTranslator(t, projectRoot)
	listDir := LocalListDir(translator, fixedNow)

	observations, err := listDir(ctx, projectRoot)
	if err != nil {
		t.Fatalf("LocalListDir() error = %v", err)
	}

	if len(observations) != 2 {
		t.Fatalf("len(observations) = %d, want 2", len(observations))
	}

	if observations[0].Name != "Dir1" {
		t.Fatalf("observations[0].Name = %q, want Dir1", observations[0].Name)
	}
	if observations[0].LocalEntry == nil || observations[0].LocalEntry.Kind != KindDir {
		t.Fatalf("observations[0].LocalEntry = %#v, want directory", observations[0].LocalEntry)
	}

	if observations[1].Name != "a.txt" {
		t.Fatalf("observations[1].Name = %q, want a.txt", observations[1].Name)
	}
	if observations[1].RemotePath != "/a.txt" {
		t.Fatalf("observations[1].RemotePath = %q, want /a.txt", observations[1].RemotePath)
	}
}

func TestWalkLocalRecursive(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	writeTestFile(t, filepath.Join(projectRoot, "root.txt"), "root")
	writeTestFile(t, filepath.Join(projectRoot, "Dir1", "child.txt"), "child")

	translator := mustTranslator(t, projectRoot)
	listDir := LocalListDir(translator, fixedNow)

	var visited []string
	err := Walk(ctx, projectRoot, listDir, WalkOptions{
		Recursive:  true,
		Ignore:     ChainIgnore(nil),
		Translator: translator,
	}, func(ctx context.Context, localDir string, observations []Observation) error {
		visited = append(visited, localDir)
		return nil
	})
	if err != nil {
		t.Fatalf("Walk() error = %v", err)
	}

	if len(visited) != 2 {
		t.Fatalf("len(visited) = %d, want 2: %#v", len(visited), visited)
	}
	if visited[0] != projectRoot {
		t.Fatalf("visited[0] = %q, want project root", visited[0])
	}
	if visited[1] != filepath.Join(projectRoot, "Dir1") {
		t.Fatalf("visited[1] = %q, want Dir1", visited[1])
	}
}

func TestWalkFiltersDefaultIgnoredFiles(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	writeTestFile(t, filepath.Join(projectRoot, ".DS_Store"), "ignored")
	if err := os.Mkdir(filepath.Join(projectRoot, ".mc"), 0o755); err != nil {
		t.Fatalf("Mkdir(.mc) error = %v", err)
	}
	writeTestFile(t, filepath.Join(projectRoot, "keep.txt"), "keep")

	translator := mustTranslator(t, projectRoot)
	listDir := LocalListDir(translator, fixedNow)

	err := Walk(ctx, projectRoot, listDir, WalkOptions{
		Recursive:  false,
		Ignore:     ChainIgnore(nil),
		Translator: translator,
	}, func(ctx context.Context, localDir string, observations []Observation) error {
		if len(observations) != 1 {
			t.Fatalf("len(observations) = %d, want 1", len(observations))
		}
		if observations[0].Name != "keep.txt" {
			t.Fatalf("observations[0].Name = %q, want keep.txt", observations[0].Name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Walk() error = %v", err)
	}
}

func TestRemoteListDir(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	translator := mustTranslator(t, projectRoot)

	remote := &fakeRemoteDirectoryLister{
		files: []mcmodel.File{
			{
				ID:        10,
				Path:      "/remote.txt",
				Name:      "remote.txt",
				Size:      5,
				MimeType:  "text/plain",
				Checksum:  "remote-md5",
				CreatedAt: time.Unix(10, 0),
				UpdatedAt: time.Unix(20, 0),
			},
		},
	}

	listDir := RemoteListDir(123, translator, remote)

	observations, err := listDir(ctx, projectRoot)
	if err != nil {
		t.Fatalf("RemoteListDir() error = %v", err)
	}

	if remote.gotProjectID != 123 {
		t.Fatalf("gotProjectID = %d, want 123", remote.gotProjectID)
	}
	if remote.gotRemotePath != "/" {
		t.Fatalf("gotRemotePath = %q, want /", remote.gotRemotePath)
	}
	if len(observations) != 1 {
		t.Fatalf("len(observations) = %d, want 1", len(observations))
	}
	if observations[0].RemoteEntry == nil {
		t.Fatal("RemoteEntry = nil, want remote entry")
	}
	if observations[0].Name != "remote.txt" {
		t.Fatalf("Name = %q, want remote.txt", observations[0].Name)
	}
}

func TestRemoteListDirNotFoundReturnsEmpty(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	translator := mustTranslator(t, projectRoot)

	remote := &fakeRemoteDirectoryLister{
		err: &mcapi.APIError{StatusCode: http.StatusNotFound, Status: "404 Not Found"},
	}

	listDir := RemoteListDir(123, translator, remote)

	observations, err := listDir(ctx, projectRoot)
	if err != nil {
		t.Fatalf("RemoteListDir() error = %v", err)
	}
	if len(observations) != 0 {
		t.Fatalf("len(observations) = %d, want 0", len(observations))
	}
}

func TestMergedListDir(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	writeTestFile(t, filepath.Join(projectRoot, "local.txt"), "local")
	writeTestFile(t, filepath.Join(projectRoot, "both.txt"), "both")

	translator := mustTranslator(t, projectRoot)

	localListDir := LocalListDir(translator, fixedNow)
	remoteListDir := RemoteListDir(123, translator, &fakeRemoteDirectoryLister{
		files: []mcmodel.File{
			{
				ID:        10,
				Path:      "/remote.txt",
				Name:      "remote.txt",
				Size:      5,
				MimeType:  "text/plain",
				Checksum:  "remote-md5",
				CreatedAt: time.Unix(10, 0),
				UpdatedAt: time.Unix(20, 0),
			},
			{
				ID:        11,
				Path:      "/both.txt",
				Name:      "both.txt",
				Size:      4,
				MimeType:  "text/plain",
				Checksum:  "both-md5",
				CreatedAt: time.Unix(10, 0),
				UpdatedAt: time.Unix(20, 0),
			},
		},
	})

	merged := MergedListDir(translator, localListDir, remoteListDir)

	observations, err := merged(ctx, projectRoot)
	if err != nil {
		t.Fatalf("MergedListDir() error = %v", err)
	}

	byName := observationsByName(observations)

	if byName["local.txt"].LocalEntry == nil || byName["local.txt"].RemoteEntry != nil {
		t.Fatalf("local.txt observation = %#v, want local only", byName["local.txt"])
	}
	if byName["remote.txt"].LocalEntry != nil || byName["remote.txt"].RemoteEntry == nil {
		t.Fatalf("remote.txt observation = %#v, want remote only", byName["remote.txt"])
	}
	if byName["both.txt"].LocalEntry == nil || byName["both.txt"].RemoteEntry == nil {
		t.Fatalf("both.txt observation = %#v, want local and remote", byName["both.txt"])
	}
}

func TestWalkAndReconcile(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	writeTestFile(t, filepath.Join(projectRoot, "file.txt"), "hello")

	translator := mustTranslator(t, projectRoot)
	listDir := LocalListDir(translator, fixedNow)

	records := fakeDirectoryRecordStore{
		recordsByDir: map[string][]filedb.FileRecord{
			"/": {
				{
					Path:             "/file.txt",
					Dir:              "/",
					Name:             "file.txt",
					IsCleanLocalCopy: false,
					LocalSize:        1,
					LocalMTimeNS:     2,
					LocalCTimeNS:     3,
				},
			},
		},
	}

	reconciler := New(ModeUpload).WithChecksumFunc(fakeChecksum("local-md5"))

	var gotStates map[string]FileState
	err := WalkAndReconcile(ctx, projectRoot, listDir, translator, records, reconciler, WalkOptions{
		Recursive: false,
		Ignore:    ChainIgnore(nil),
	}, func(ctx context.Context, localDir string, states map[string]FileState) error {
		gotStates = states
		return nil
	})
	if err != nil {
		t.Fatalf("WalkAndReconcile() error = %v", err)
	}

	state, ok := gotStates["file.txt"]
	if !ok {
		t.Fatal("state for file.txt not found")
	}
	if state.Observation.FileRecord == nil {
		t.Fatal("FileRecord = nil, want file record")
	}
	if state.Decision.Action != ActionUpload {
		t.Fatalf("Decision.Action = %q, want upload", state.Decision.Action)
	}
}

func TestWalkNodesRemoteOnlyRecursive(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	translator := mustTranslator(t, projectRoot)

	remote := &fakeRemoteDirectoryLister{
		filesByPath: map[string][]mcmodel.File{
			"/": {
				{
					ID:        10,
					Path:      "/RemoteDir",
					Name:      "RemoteDir",
					MimeType:  "directory",
					CreatedAt: time.Unix(10, 0),
					UpdatedAt: time.Unix(20, 0),
				},
			},
			"/RemoteDir": {
				{
					ID:        11,
					Path:      "/RemoteDir/file.txt",
					Name:      "file.txt",
					Size:      5,
					MimeType:  "text/plain",
					Checksum:  "remote-md5",
					CreatedAt: time.Unix(10, 0),
					UpdatedAt: time.Unix(20, 0),
				},
			},
		},
	}

	listDir := RemoteOnlyListDir(123, translator, remote)

	var visited []string
	err := WalkNodes(ctx, WalkNode{
		LocalPath:  projectRoot,
		RemotePath: "/",
	}, listDir, WalkOptions{
		Recursive:  true,
		Ignore:     ChainIgnore(nil),
		Translator: translator,
	}, func(ctx context.Context, node WalkNode, observations []Observation) error {
		visited = append(visited, node.RemotePath)
		return nil
	})
	if err != nil {
		t.Fatalf("WalkNodes() error = %v", err)
	}

	if len(visited) != 2 {
		t.Fatalf("len(visited) = %d, want 2: %#v", len(visited), visited)
	}
	if visited[0] != "/" {
		t.Fatalf("visited[0] = %q, want /", visited[0])
	}
	if visited[1] != "/RemoteDir" {
		t.Fatalf("visited[1] = %q, want /RemoteDir", visited[1])
	}
}

func TestWalkNodesAndReconcileRemoteOnly(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	translator := mustTranslator(t, projectRoot)

	remote := &fakeRemoteDirectoryLister{
		filesByPath: map[string][]mcmodel.File{
			"/": {
				{
					ID:        10,
					Path:      "/remote.txt",
					Name:      "remote.txt",
					Size:      5,
					MimeType:  "text/plain",
					Checksum:  "remote-md5",
					CreatedAt: time.Unix(10, 0),
					UpdatedAt: time.Unix(20, 0),
				},
			},
		},
	}

	listDir := RemoteOnlyListDir(123, translator, remote)
	records := fakeDirectoryRecordStore{}
	reconciler := New(ModeDownload)

	var gotStates map[string]FileState
	err := WalkNodesAndReconcile(ctx, WalkNode{
		LocalPath:  projectRoot,
		RemotePath: "/",
	}, listDir, records, reconciler, WalkOptions{
		Recursive:  false,
		Ignore:     ChainIgnore(nil),
		Translator: translator,
	}, func(ctx context.Context, node WalkNode, states map[string]FileState) error {
		gotStates = states
		return nil
	})
	if err != nil {
		t.Fatalf("WalkNodesAndReconcile() error = %v", err)
	}

	state, ok := gotStates["remote.txt"]
	if !ok {
		t.Fatal("state for remote.txt not found")
	}
	if state.Observation.LocalEntry != nil {
		t.Fatalf("LocalEntry = %#v, want nil", state.Observation.LocalEntry)
	}
	if state.Observation.RemoteEntry == nil {
		t.Fatal("RemoteEntry = nil, want remote entry")
	}
	if state.Decision.Action != ActionDownload {
		t.Fatalf("Decision.Action = %q, want download", state.Decision.Action)
	}
}

func TestMergedNodeListDirRecursesIntoRemoteOnlyDirectory(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	translator := mustTranslator(t, projectRoot)

	localListDir := LocalNodeListDir(translator, fixedNow)
	remoteListDir := RemoteOnlyListDir(123, translator, &fakeRemoteDirectoryLister{
		filesByPath: map[string][]mcmodel.File{
			"/": {
				{
					ID:        10,
					Path:      "/RemoteDir",
					Name:      "RemoteDir",
					MimeType:  "directory",
					CreatedAt: time.Unix(10, 0),
					UpdatedAt: time.Unix(20, 0),
				},
			},
			"/RemoteDir": {
				{
					ID:        11,
					Path:      "/RemoteDir/file.txt",
					Name:      "file.txt",
					Size:      5,
					MimeType:  "text/plain",
					Checksum:  "remote-md5",
					CreatedAt: time.Unix(10, 0),
					UpdatedAt: time.Unix(20, 0),
				},
			},
		},
	})

	merged := MergedNodeListDir(translator, localListDir, remoteListDir)

	var visited []string
	err := WalkNodes(ctx, WalkNode{
		LocalPath:  projectRoot,
		RemotePath: "/",
	}, merged, WalkOptions{
		Recursive:  true,
		Ignore:     ChainIgnore(nil),
		Translator: translator,
	}, func(ctx context.Context, node WalkNode, observations []Observation) error {
		visited = append(visited, node.RemotePath)
		return nil
	})
	if err != nil {
		t.Fatalf("WalkNodes() error = %v", err)
	}

	if len(visited) != 2 {
		t.Fatalf("len(visited) = %d, want 2: %#v", len(visited), visited)
	}
	if visited[0] != "/" {
		t.Fatalf("visited[0] = %q, want /", visited[0])
	}
	if visited[1] != "/RemoteDir" {
		t.Fatalf("visited[1] = %q, want /RemoteDir", visited[1])
	}
}

func TestWalkNodesEmptyRootReturnsInvalidWalkNode(t *testing.T) {
	err := WalkNodes(context.Background(), WalkNode{}, func(ctx context.Context, node WalkNode) ([]Observation, error) {
		return nil, nil
	}, WalkOptions{}, func(ctx context.Context, node WalkNode, observations []Observation) error {
		return nil
	})

	if !errors.Is(err, ErrInvalidWalkNode) {
		t.Fatalf("WalkNodes() error = %v, want ErrInvalidWalkNode", err)
	}
}

func TestWalkNodesRelativeRemotePathReturnsInvalidWalkNode(t *testing.T) {
	err := WalkNodes(context.Background(), WalkNode{
		RemotePath: "Dir1",
	}, func(ctx context.Context, node WalkNode) ([]Observation, error) {
		return nil, nil
	}, WalkOptions{}, func(ctx context.Context, node WalkNode, observations []Observation) error {
		return nil
	})

	if !errors.Is(err, ErrInvalidWalkNode) {
		t.Fatalf("WalkNodes() error = %v, want ErrInvalidWalkNode", err)
	}
}

func TestWalkNodesSynthesizesLocalPathForRemoteOnlyNode(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	translator := mustTranslator(t, projectRoot)

	var gotNode WalkNode
	err := WalkNodes(ctx, WalkNode{
		RemotePath: "/RemoteDir",
	}, func(ctx context.Context, node WalkNode) ([]Observation, error) {
		return nil, nil
	}, WalkOptions{
		Translator: translator,
	}, func(ctx context.Context, node WalkNode, observations []Observation) error {
		gotNode = node
		return nil
	})
	if err != nil {
		t.Fatalf("WalkNodes() error = %v", err)
	}

	wantLocalPath := filepath.Join(projectRoot, "RemoteDir")
	if gotNode.LocalPath != wantLocalPath {
		t.Fatalf("LocalPath = %q, want %q", gotNode.LocalPath, wantLocalPath)
	}
	if gotNode.RemotePath != "/RemoteDir" {
		t.Fatalf("RemotePath = %q, want /RemoteDir", gotNode.RemotePath)
	}
}

func TestWalkNodesIgnoreUsesSynthesizedLocalPathForRemoteOnlyEntry(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	translator := mustTranslator(t, projectRoot)

	listDir := func(ctx context.Context, node WalkNode) ([]Observation, error) {
		return []Observation{
			{
				RemotePath: "/SkipMe",
				Name:       "SkipMe",
				Dir:        "/",
				RemoteEntry: &RemoteEntry{
					Path: "/SkipMe",
					Name: "SkipMe",
					Dir:  "/",
					Kind: KindDir,
				},
			},
			{
				RemotePath: "/KeepMe",
				Name:       "KeepMe",
				Dir:        "/",
				RemoteEntry: &RemoteEntry{
					Path: "/KeepMe",
					Name: "KeepMe",
					Dir:  "/",
					Kind: KindDir,
				},
			},
		}, nil
	}

	var got []Observation
	err := WalkNodes(ctx, WalkNode{
		LocalPath:  projectRoot,
		RemotePath: "/",
	}, listDir, WalkOptions{
		Recursive:  false,
		Translator: translator,
		Ignore: func(pathValue string, isDir bool) bool {
			return filepath.Base(pathValue) == "SkipMe"
		},
	}, func(ctx context.Context, node WalkNode, observations []Observation) error {
		got = observations
		return nil
	})
	if err != nil {
		t.Fatalf("WalkNodes() error = %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("len(observations) = %d, want 1", len(got))
	}
	if got[0].Name != "KeepMe" {
		t.Fatalf("remaining observation = %q, want KeepMe", got[0].Name)
	}
}

func TestRemoteOnlyListDirRejectsMalformedRemoteEntry(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	translator := mustTranslator(t, projectRoot)

	remote := &fakeRemoteDirectoryLister{
		filesByPath: map[string][]mcmodel.File{
			"/": {
				{
					ID:   10,
					Path: "relative/path.txt",
					Name: "path.txt",
				},
			},
		},
	}

	listDir := RemoteOnlyListDir(123, translator, remote)

	_, err := listDir(ctx, WalkNode{
		LocalPath:  projectRoot,
		RemotePath: "/",
	})
	if !errors.Is(err, ErrInvalidObservation) {
		t.Fatalf("RemoteOnlyListDir() error = %v, want ErrInvalidObservation", err)
	}
}

func TestWalkNodesAndReconcileMissingRemotePathReturnsInvalidWalkNode(t *testing.T) {
	ctx := context.Background()

	err := WalkNodesAndReconcile(
		ctx,
		WalkNode{LocalPath: "/tmp/project"},
		func(ctx context.Context, node WalkNode) ([]Observation, error) {
			return nil, nil
		},
		fakeDirectoryRecordStore{},
		New(ModeStatus),
		WalkOptions{},
		func(ctx context.Context, node WalkNode, states map[string]FileState) error {
			return nil
		},
	)

	if !errors.Is(err, ErrInvalidWalkNode) {
		t.Fatalf("WalkNodesAndReconcile() error = %v, want ErrInvalidWalkNode", err)
	}
}

func TestWalkNodesContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := WalkNodes(ctx, WalkNode{RemotePath: "/"}, func(ctx context.Context, node WalkNode) ([]Observation, error) {
		t.Fatal("listDir should not be called after context cancellation")
		return nil, nil
	}, WalkOptions{}, func(ctx context.Context, node WalkNode, observations []Observation) error {
		return nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WalkNodes() error = %v, want context.Canceled", err)
	}
}

func TestWalkNodesCallbackErrorIsReturned(t *testing.T) {
	ctx := context.Background()
	callbackErr := fmt.Errorf("callback failed")

	err := WalkNodes(ctx, WalkNode{RemotePath: "/"}, func(ctx context.Context, node WalkNode) ([]Observation, error) {
		return nil, nil
	}, WalkOptions{}, func(ctx context.Context, node WalkNode, observations []Observation) error {
		return callbackErr
	})

	if !errors.Is(err, callbackErr) {
		t.Fatalf("WalkNodes() error = %v, want callbackErr", err)
	}
}

type fakeRemoteDirectoryLister struct {
	files       []mcmodel.File
	filesByPath map[string][]mcmodel.File
	err         error

	gotProjectID  int
	gotRemotePath string
}

func (l *fakeRemoteDirectoryLister) ListDirectoryByPath(projectID int, remotePath string) ([]mcmodel.File, error) {
	l.gotProjectID = projectID
	l.gotRemotePath = remotePath

	if l.err != nil {
		return nil, l.err
	}

	if l.filesByPath != nil {
		return l.filesByPath[remotePath], nil
	}

	return l.files, nil
}

type fakeDirectoryRecordStore struct {
	recordsByDir map[string][]filedb.FileRecord
	err          error
}

func (s fakeDirectoryRecordStore) ListByDir(ctx context.Context, dir string) ([]filedb.FileRecord, error) {
	if s.err != nil {
		return nil, s.err
	}

	return s.recordsByDir[dir], nil
}

func observationsByName(observations []Observation) map[string]Observation {
	byName := make(map[string]Observation, len(observations))
	for _, obs := range observations {
		byName[obs.Name] = obs
	}
	return byName
}
