package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcapi "github.com/materials-commons/gomcapi"
	"github.com/materials-commons/hydra/pkg/mcdb/mcmodel"
	"github.com/materials-commons/mccli/internal/config"
	"github.com/materials-commons/mccli/internal/di"
	"github.com/materials-commons/mccli/internal/filedb"
	"github.com/materials-commons/mccli/internal/mc"
	"github.com/materials-commons/mccli/internal/reconcile"
)

func TestRmRunnerRunRejectsNoArgsBeforeCreatingRemover(t *testing.T) {
	err := rmRunner{}.run(context.Background(), rmOpts{}, nil)
	if err == nil {
		t.Fatal("run() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "at least one path argument") {
		t.Fatalf("run() error = %v, want missing path error", err)
	}
}

func TestRmRunnerRunRejectsRemoteOnlyAndLocalOnly(t *testing.T) {
	err := rmRunner{}.run(context.Background(), rmOpts{
		RemoteOnly: true,
		LocalOnly:  true,
	}, []string{"file.txt"})

	if err == nil {
		t.Fatal("run() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "cannot specify both --remote-only and --local-only") {
		t.Fatalf("run() error = %v, want conflicting flags error", err)
	}
}

func TestRmRunnerRunReturnsCancelledContextBeforeValidation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := rmRunner{}.run(ctx, rmOpts{}, []string{"file.txt"})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run() error = %v, want context.Canceled", err)
	}
}

func TestRmRunnerNormalizeOptionsDefaults(t *testing.T) {
	got := rmRunner{}.normalizeOptions(rmOpts{})

	if got.WorkingDir != "." {
		t.Fatalf("WorkingDir = %q, want .", got.WorkingDir)
	}
	if got.Out == nil {
		t.Fatal("Out = nil, want default writer")
	}
}

func TestRemoverNormalizeRemotePathAdversarial(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "empty maps to root", input: "", want: "/"},
		{name: "dot maps to root", input: ".", want: "/"},
		{name: "slash maps to root", input: "/", want: "/"},
		{name: "relative gets leading slash", input: "file.txt", want: "/file.txt"},
		{name: "absolute remains absolute", input: "/file.txt", want: "/file.txt"},
		{name: "duplicate slashes are cleaned", input: "Dir//Sub///file.txt", want: "/Dir/Sub/file.txt"},
		{name: "dot elements are cleaned", input: "./Dir/./file.txt", want: "/Dir/file.txt"},
		{name: "parent elements are cleaned", input: "/Dir/Sub/../file.txt", want: "/Dir/file.txt"},
		{name: "above root collapses to root child", input: "../../escape.txt", want: "/escape.txt"},
		{name: "windows separators are converted", input: `Dir\Sub\file.txt`, want: "/Dir/Sub/file.txt"},
		{name: "windows absolute drive is normalized as project path", input: `C:\Temp\file.txt`, want: "/C:/Temp/file.txt"},
		{name: "trailing slash directory is cleaned", input: "/Dir/Sub/", want: "/Dir/Sub"},
		{name: "spaces are preserved", input: "Dir With Spaces/file name.txt", want: "/Dir With Spaces/file name.txt"},
	}

	r := &remover{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.normalizeRemotePath(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("normalizeRemotePath() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeRemotePath() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalizeRemotePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestShouldRemoveFileAllFlagCombinations(t *testing.T) {
	actions := []reconcile.Action{
		reconcile.ActionDownload,
		reconcile.ActionConflict,
		reconcile.ActionUpload,
		reconcile.ActionDBUpdate,
		reconcile.ActionSkip,
		reconcile.Action("new-unhandled-action"),
	}

	for mask := 0; mask < 16; mask++ {
		opts := rmOpts{
			Recursive:  mask&1 != 0,
			RemoteOnly: mask&2 != 0,
			LocalOnly:  mask&4 != 0,
			Force:      mask&8 != 0,
		}

		for _, action := range actions {
			for _, hasRemote := range []bool{false, true} {
				name := fmt.Sprintf("mask=%04b/action=%s/remote=%t", mask, action, hasRemote)
				t.Run(name, func(t *testing.T) {
					state := reconcile.FileState{
						Decision: reconcile.Decision{
							Action: action,
						},
					}
					if hasRemote {
						state.Observation.RemoteEntry = &reconcile.RemoteEntry{
							Path: "/file.txt",
							Name: "file.txt",
							Dir:  "/",
							Kind: reconcile.KindFile,
						}
					}

					got := shouldRemoveFile(opts, state)

					want := false
					switch {
					case opts.Force:
						want = true
					case action == reconcile.ActionDownload:
						want = true
					case action == reconcile.ActionSkip && hasRemote:
						want = true
					case action == reconcile.ActionSkip && opts.LocalOnly == true:
						want = true
					default:
						want = false
					}

					if got != want {
						t.Fatalf("shouldRemoveFile(%+v, action=%s, hasRemote=%t) = %t, want %t",
							opts, action, hasRemote, got, want)
					}
				})
			}
		}
	}
}

func TestStateIsFileAdversarial(t *testing.T) {
	tests := []struct {
		name  string
		state reconcile.FileState
		want  bool
	}{
		{
			name:  "empty state is not file",
			state: reconcile.FileState{},
			want:  false,
		},
		{
			name: "local file is file",
			state: reconcile.FileState{
				Observation: reconcile.Observation{
					LocalEntry: &reconcile.LocalEntry{Kind: reconcile.KindFile},
				},
			},
			want: true,
		},
		{
			name: "remote file is file",
			state: reconcile.FileState{
				Observation: reconcile.Observation{
					RemoteEntry: &reconcile.RemoteEntry{Kind: reconcile.KindFile},
				},
			},
			want: true,
		},
		{
			name: "local directory only is not file",
			state: reconcile.FileState{
				Observation: reconcile.Observation{
					LocalEntry: &reconcile.LocalEntry{Kind: reconcile.KindDir},
				},
			},
			want: false,
		},
		{
			name: "remote directory only is not file",
			state: reconcile.FileState{
				Observation: reconcile.Observation{
					RemoteEntry: &reconcile.RemoteEntry{Kind: reconcile.KindDir},
				},
			},
			want: false,
		},
		{
			name: "local directory with remote file is file",
			state: reconcile.FileState{
				Observation: reconcile.Observation{
					LocalEntry:  &reconcile.LocalEntry{Kind: reconcile.KindDir},
					RemoteEntry: &reconcile.RemoteEntry{Kind: reconcile.KindFile},
				},
			},
			want: true,
		},
		{
			name: "local file with remote directory is file",
			state: reconcile.FileState{
				Observation: reconcile.Observation{
					LocalEntry:  &reconcile.LocalEntry{Kind: reconcile.KindFile},
					RemoteEntry: &reconcile.RemoteEntry{Kind: reconcile.KindDir},
				},
			},
			want: true,
		},
		{
			name: "unknown kinds are not file",
			state: reconcile.FileState{
				Observation: reconcile.Observation{
					LocalEntry:  &reconcile.LocalEntry{Kind: reconcile.KindUnknown},
					RemoteEntry: &reconcile.RemoteEntry{Kind: reconcile.KindUnknown},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stateIsFile(tt.state)
			if got != tt.want {
				t.Fatalf("stateIsFile() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestRemoveRemoteFile(t *testing.T) {
	ctx := context.Background()

	t.Run("cancelled context returns cancellation and does not delete", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		deleter := &fakeRmRemote{}
		r := &remover{
			project:           testRmProject(),
			remoteFileDeleter: deleter,
		}

		err := r.removeRemoteFile(ctx, reconcile.FileState{
			Observation: reconcile.Observation{
				RemotePath: "/file.txt",
				RemoteEntry: &reconcile.RemoteEntry{
					RemoteFileID: int64Ptr(10),
				},
			},
		})

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("removeRemoteFile() error = %v, want context.Canceled", err)
		}
		if len(deleter.deletedFileIDs) != 0 {
			t.Fatalf("deletedFileIDs = %#v, want none", deleter.deletedFileIDs)
		}
	})

	t.Run("nil remote entry is a no-op", func(t *testing.T) {
		deleter := &fakeRmRemote{}
		r := &remover{
			project:           testRmProject(),
			remoteFileDeleter: deleter,
		}

		err := r.removeRemoteFile(ctx, reconcile.FileState{
			Observation: reconcile.Observation{
				RemotePath: "/missing.txt",
			},
		})

		if err != nil {
			t.Fatalf("removeRemoteFile() error = %v", err)
		}
		if len(deleter.deletedFileIDs) != 0 {
			t.Fatalf("deletedFileIDs = %#v, want none", deleter.deletedFileIDs)
		}
	})

	t.Run("missing remote file id errors", func(t *testing.T) {
		deleter := &fakeRmRemote{}
		r := &remover{
			project:           testRmProject(),
			remoteFileDeleter: deleter,
		}

		err := r.removeRemoteFile(ctx, reconcile.FileState{
			Observation: reconcile.Observation{
				RemotePath:  "/bad.txt",
				RemoteEntry: &reconcile.RemoteEntry{},
			},
		})

		if err == nil {
			t.Fatal("removeRemoteFile() error = nil, want error")
		}
		if !strings.Contains(err.Error(), "remote file id is missing") {
			t.Fatalf("removeRemoteFile() error = %v, want missing id error", err)
		}
		if len(deleter.deletedFileIDs) != 0 {
			t.Fatalf("deletedFileIDs = %#v, want none", deleter.deletedFileIDs)
		}
	})

	t.Run("delete error is wrapped", func(t *testing.T) {
		deleteErr := errors.New("remote delete failed")
		deleter := &fakeRmRemote{deleteErr: deleteErr}
		r := &remover{
			project:           testRmProject(),
			remoteFileDeleter: deleter,
		}

		err := r.removeRemoteFile(ctx, reconcile.FileState{
			Observation: reconcile.Observation{
				RemotePath: "/file.txt",
				RemoteEntry: &reconcile.RemoteEntry{
					RemoteFileID: int64Ptr(55),
				},
			},
		})

		if !errors.Is(err, deleteErr) {
			t.Fatalf("removeRemoteFile() error = %v, want wrapped deleteErr", err)
		}
	})

	t.Run("deletes remote file by id", func(t *testing.T) {
		deleter := &fakeRmRemote{}
		r := &remover{
			project:           testRmProject(),
			remoteFileDeleter: deleter,
		}

		err := r.removeRemoteFile(ctx, reconcile.FileState{
			Observation: reconcile.Observation{
				RemotePath: "/file.txt",
				RemoteEntry: &reconcile.RemoteEntry{
					RemoteFileID: int64Ptr(77),
				},
			},
		})

		if err != nil {
			t.Fatalf("removeRemoteFile() error = %v", err)
		}
		if deleter.gotProjectID != 123 {
			t.Fatalf("gotProjectID = %d, want 123", deleter.gotProjectID)
		}
		if len(deleter.deletedFileIDs) != 1 || deleter.deletedFileIDs[0] != 77 {
			t.Fatalf("deletedFileIDs = %#v, want [77]", deleter.deletedFileIDs)
		}
	})
}

func TestRemoveFileFromStateAllFlagCombinations(t *testing.T) {
	for mask := 0; mask < 16; mask++ {
		opts := rmOpts{
			Recursive:  mask&1 != 0,
			RemoteOnly: mask&2 != 0,
			LocalOnly:  mask&4 != 0,
			Force:      mask&8 != 0,
			DryRun:     false,
		}

		t.Run(fmt.Sprintf("mask=%04b", mask), func(t *testing.T) {

			projectRoot := t.TempDir()
			localPath := filepath.Join(projectRoot, "file.txt")
			if err := os.WriteFile(localPath, []byte("contents"), 0o644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			translator, err := mc.NewProjectPathTranslator(projectRoot)
			if err != nil {
				t.Fatalf("NewProjectPathTranslator() error = %v", err)
			}

			store := &fakeRmStore{}
			remote := &fakeRmRemote{}
			out := &strings.Builder{}

			r := &remover{
				opts:              opts,
				project:           testRmProject(),
				store:             store,
				remoteFileDeleter: remote,
				translator:        translator,
			}

			state := reconcile.FileState{
				Observation: reconcile.Observation{
					RemotePath: "/file.txt",
					Name:       "file.txt",
					Dir:        "/",
					LocalEntry: &reconcile.LocalEntry{
						Path:       localPath,
						RemotePath: "/file.txt",
						Name:       "file.txt",
						Dir:        "/",
						Kind:       reconcile.KindFile,
					},
					RemoteEntry: &reconcile.RemoteEntry{
						Path:         "/file.txt",
						Name:         "file.txt",
						Dir:          "/",
						Kind:         reconcile.KindFile,
						RemoteFileID: int64Ptr(91),
					},
				},
				Decision: reconcile.Decision{
					Action: reconcile.ActionDownload,
					Reason: "safe to remove",
				},
			}
			r.opts.Out = out

			err = r.removeFileFromState(context.Background(), state)
			if err != nil {
				t.Fatalf("removeFileFromState() error = %v", err)
			}

			_, statErr := os.Stat(localPath)
			localRemoved := errors.Is(statErr, os.ErrNotExist)
			remoteRemoved := len(remote.deletedFileIDs) == 1
			dbDeleted := len(store.deletedPaths) == 1

			wantLocalRemoved := opts.LocalOnly || (!opts.LocalOnly && !opts.RemoteOnly)
			wantRemoteRemoved := opts.RemoteOnly || (!opts.LocalOnly && !opts.RemoteOnly)

			if localRemoved != wantLocalRemoved {
				t.Fatalf("localRemoved = %t, want %t for opts %+v", localRemoved, wantLocalRemoved, opts)
			}
			if remoteRemoved != wantRemoteRemoved {
				t.Fatalf("remoteRemoved = %t, want %t for opts %+v", remoteRemoved, wantRemoteRemoved, opts)
			}
			if !dbDeleted {
				t.Fatalf("dbDeleted = false, want true for opts %+v", opts)
			}
			if store.deletedPaths[0] != "/file.txt" {
				t.Fatalf("deletedPaths = %#v, want [/file.txt]", store.deletedPaths)
			}
			if !strings.Contains(out.String(), "Removed /file.txt") {
				t.Fatalf("output = %q, want Removed message", out.String())
			}
		})
	}
}

func TestRemoveFileFromStateDryRunAllFlagCombinationsDoesNotMutate(t *testing.T) {
	for mask := 0; mask < 16; mask++ {
		opts := rmOpts{
			Recursive:  mask&1 != 0,
			RemoteOnly: mask&2 != 0,
			LocalOnly:  mask&4 != 0,
			Force:      mask&8 != 0,
			DryRun:     true,
		}

		t.Run(fmt.Sprintf("mask=%04b", mask), func(t *testing.T) {
			projectRoot := t.TempDir()
			localPath := filepath.Join(projectRoot, "file.txt")
			if err := os.WriteFile(localPath, []byte("contents"), 0o644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			translator, err := mc.NewProjectPathTranslator(projectRoot)
			if err != nil {
				t.Fatalf("NewProjectPathTranslator() error = %v", err)
			}

			store := &fakeRmStore{}
			remote := &fakeRmRemote{}
			out := &strings.Builder{}

			r := &remover{
				opts:              opts,
				project:           testRmProject(),
				store:             store,
				remoteFileDeleter: remote,
				translator:        translator,
			}
			r.opts.Out = out

			err = r.removeFileFromState(context.Background(), reconcile.FileState{
				Observation: reconcile.Observation{
					RemotePath: "/file.txt",
					RemoteEntry: &reconcile.RemoteEntry{
						Kind:         reconcile.KindFile,
						RemoteFileID: int64Ptr(91),
					},
				},
				Decision: reconcile.Decision{
					Action: reconcile.ActionDownload,
				},
			})
			if err != nil {
				t.Fatalf("removeFileFromState() error = %v", err)
			}

			if _, err := os.Stat(localPath); err != nil {
				t.Fatalf("local file stat error = %v, want file preserved", err)
			}
			if len(remote.deletedFileIDs) != 0 {
				t.Fatalf("deletedFileIDs = %#v, want none", remote.deletedFileIDs)
			}
			if len(store.deletedPaths) != 0 {
				t.Fatalf("deletedPaths = %#v, want none", store.deletedPaths)
			}
			if !strings.Contains(out.String(), "Would remove /file.txt") {
				t.Fatalf("output = %q, want dry-run message", out.String())
			}
		})
	}
}

func TestRemoveFileFromStateSkipsUnsafeDecisionWithoutMutating(t *testing.T) {
	projectRoot := t.TempDir()
	localPath := filepath.Join(projectRoot, "file.txt")
	if err := os.WriteFile(localPath, []byte("contents"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	translator, err := mc.NewProjectPathTranslator(projectRoot)
	if err != nil {
		t.Fatalf("NewProjectPathTranslator() error = %v", err)
	}

	store := &fakeRmStore{}
	remote := &fakeRmRemote{}
	out := &strings.Builder{}

	r := &remover{
		opts: rmOpts{
			LocalOnly: true,
			Out:       out,
		},
		project:           testRmProject(),
		store:             store,
		remoteFileDeleter: remote,
		translator:        translator,
	}

	err = r.removeFileFromState(context.Background(), reconcile.FileState{
		Observation: reconcile.Observation{
			RemotePath: "/file.txt",
			LocalEntry: &reconcile.LocalEntry{
				Path: localPath,
				Kind: reconcile.KindFile,
			},
		},
		Decision: reconcile.Decision{
			Action: reconcile.ActionUpload,
			Reason: "local changes would be lost",
		},
	})
	if err != nil {
		t.Fatalf("removeFileFromState() error = %v", err)
	}

	if _, err := os.Stat(localPath); err != nil {
		t.Fatalf("local file stat error = %v, want file preserved", err)
	}
	if len(remote.deletedFileIDs) != 0 {
		t.Fatalf("deletedFileIDs = %#v, want none", remote.deletedFileIDs)
	}
	if len(store.deletedPaths) != 0 {
		t.Fatalf("deletedPaths = %#v, want none", store.deletedPaths)
	}
	if !strings.Contains(out.String(), "Skipping /file.txt - local changes would be lost") {
		t.Fatalf("output = %q, want skip reason", out.String())
	}
}

func TestRemoveFileFromStateRecordNotFoundIsIgnoredRegression(t *testing.T) {
	projectRoot := t.TempDir()
	translator, err := mc.NewProjectPathTranslator(projectRoot)
	if err != nil {
		t.Fatalf("NewProjectPathTranslator() error = %v", err)
	}

	r := &remover{
		opts: rmOpts{
			RemoteOnly: true,
			Out:        &strings.Builder{},
		},
		project: testRmProject(),
		store: &fakeRmStore{
			deleteErr: filedb.ErrRecordNotFound,
		},
		remoteFileDeleter: &fakeRmRemote{},
		translator:        translator,
	}

	err = r.removeFileFromState(context.Background(), reconcile.FileState{
		Observation: reconcile.Observation{
			RemotePath: "/file.txt",
			RemoteEntry: &reconcile.RemoteEntry{
				Path:         "/file.txt",
				Name:         "file.txt",
				Dir:          "/",
				Kind:         reconcile.KindFile,
				RemoteFileID: int64Ptr(7),
			},
		},
		Decision: reconcile.Decision{
			Action: reconcile.ActionDownload,
		},
	})

	if err != nil {
		t.Fatalf("removeFileFromState() error = %v, want nil when DB record is already absent", err)
	}
}

func TestRemoveLocalOnlyReconciledFileFlagMatrix(t *testing.T) {
	for mask := 0; mask < 8; mask++ {
		opts := rmOpts{
			LocalOnly: mask&1 != 0,
			Force:     mask&2 != 0,
			DryRun:    mask&4 != 0,
		}

		t.Run(fmt.Sprintf("mask=%03b", mask), func(t *testing.T) {
			projectRoot := t.TempDir()
			localPath := filepath.Join(projectRoot, "local-only.txt")
			if err := os.WriteFile(localPath, []byte("contents"), 0o644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			store := &fakeRmStore{}
			out := &strings.Builder{}

			r := &remover{
				opts:  opts,
				store: store,
			}
			r.opts.Out = out

			err := r.removeLocalOnlyReconciledFile(context.Background(), reconcile.FileState{
				Observation: reconcile.Observation{
					RemotePath: "/local-only.txt",
					LocalEntry: &reconcile.LocalEntry{
						Path: localPath,
						Kind: reconcile.KindFile,
					},
				},
				Decision: reconcile.Decision{
					Action: reconcile.ActionSkip,
					Reason: "safe local-only removal",
				},
			})
			if err != nil {
				t.Fatalf("removeLocalOnlyReconciledFile() error = %v", err)
			}

			_, statErr := os.Stat(localPath)
			localRemoved := errors.Is(statErr, os.ErrNotExist)

			shouldAttemptRemoval := opts.LocalOnly || opts.Force
			wantRemoved := shouldAttemptRemoval && !opts.DryRun

			if localRemoved != wantRemoved {
				t.Fatalf("localRemoved = %t, want %t for opts %+v", localRemoved, wantRemoved, opts)
			}

			if !shouldAttemptRemoval {
				if !strings.Contains(out.String(), "Skipping") {
					t.Fatalf("output = %q, want Skipping", out.String())
				}
				if len(store.deletedPaths) != 0 {
					t.Fatalf("deletedPaths = %#v, want none", store.deletedPaths)
				}
				return
			}

			if opts.DryRun {
				if !strings.Contains(out.String(), "Would remove /local-only.txt") {
					t.Fatalf("output = %q, want dry-run message", out.String())
				}
				if len(store.deletedPaths) != 0 {
					t.Fatalf("deletedPaths = %#v, want none", store.deletedPaths)
				}
				return
			}

			if len(store.deletedPaths) != 1 || store.deletedPaths[0] != "/local-only.txt" {
				t.Fatalf("deletedPaths = %#v, want [/local-only.txt]", store.deletedPaths)
			}
			if !strings.Contains(out.String(), "Removed /local-only.txt") {
				t.Fatalf("output = %q, want Removed message", out.String())
			}
		})
	}
}

func TestRemoveLocalOnlyReconciledFileDoesNotRemoveEmptyDecisionRegression(t *testing.T) {
	projectRoot := t.TempDir()
	localPath := filepath.Join(projectRoot, "empty-decision.txt")
	if err := os.WriteFile(localPath, []byte("contents"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	store := &fakeRmStore{}
	out := &strings.Builder{}
	r := &remover{
		opts: rmOpts{
			LocalOnly: true,
			Out:       out,
		},
		store: store,
	}

	err := r.removeLocalOnlyReconciledFile(context.Background(), reconcile.FileState{
		Observation: reconcile.Observation{
			RemotePath: "/empty-decision.txt",
			LocalEntry: &reconcile.LocalEntry{
				Path: localPath,
				Kind: reconcile.KindFile,
			},
		},
		Decision: reconcile.Decision{},
	})
	if err != nil {
		t.Fatalf("removeLocalOnlyReconciledFile() error = %v", err)
	}

	if _, err := os.Stat(localPath); err != nil {
		t.Fatalf("local file stat error = %v, want file preserved", err)
	}
	if len(store.deletedPaths) != 0 {
		t.Fatalf("deletedPaths = %#v, want none", store.deletedPaths)
	}
	if !strings.Contains(out.String(), "Skipping") {
		t.Fatalf("output = %q, want skip message", out.String())
	}
}

func TestRemoveLocalFile(t *testing.T) {
	t.Run("empty path is no-op", func(t *testing.T) {
		r := &remover{}

		if err := r.removeLocalFile(""); err != nil {
			t.Fatalf("removeLocalFile() error = %v", err)
		}
	})

	t.Run("missing file is no-op", func(t *testing.T) {
		r := &remover{}

		err := r.removeLocalFile(filepath.Join(t.TempDir(), "missing.txt"))
		if err != nil {
			t.Fatalf("removeLocalFile() error = %v", err)
		}
	})

	t.Run("existing file is removed", func(t *testing.T) {
		localPath := filepath.Join(t.TempDir(), "file.txt")
		if err := os.WriteFile(localPath, []byte("contents"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		r := &remover{}

		err := r.removeLocalFile(localPath)
		if err != nil {
			t.Fatalf("removeLocalFile() error = %v", err)
		}

		if _, err := os.Stat(localPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Stat() error = %v, want os.ErrNotExist", err)
		}
	})
}

func TestRemoveFileFromStateDefaultRemovesLocalAndRemoteRegression(t *testing.T) {
	projectRoot := t.TempDir()
	localPath := filepath.Join(projectRoot, "file.txt")
	if err := os.WriteFile(localPath, []byte("contents"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	translator, err := mc.NewProjectPathTranslator(projectRoot)
	if err != nil {
		t.Fatalf("NewProjectPathTranslator() error = %v", err)
	}

	store := &fakeRmStore{}
	remote := &fakeRmRemote{}
	out := &strings.Builder{}

	r := &remover{
		opts: rmOpts{
			Out: out,
		},
		project:           testRmProject(),
		store:             store,
		remoteFileDeleter: remote,
		translator:        translator,
	}

	err = r.removeFileFromState(context.Background(), reconcile.FileState{
		Observation: reconcile.Observation{
			RemotePath: "/file.txt",
			Name:       "file.txt",
			Dir:        "/",
			LocalEntry: &reconcile.LocalEntry{
				Path:       localPath,
				RemotePath: "/file.txt",
				Name:       "file.txt",
				Dir:        "/",
				Kind:       reconcile.KindFile,
			},
			RemoteEntry: &reconcile.RemoteEntry{
				Path:         "/file.txt",
				Name:         "file.txt",
				Dir:          "/",
				Kind:         reconcile.KindFile,
				RemoteFileID: int64Ptr(44),
			},
		},
		Decision: reconcile.Decision{
			Action: reconcile.ActionDownload,
			Reason: "safe to remove",
		},
	})
	if err != nil {
		t.Fatalf("removeFileFromState() error = %v", err)
	}

	if _, err := os.Stat(localPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("local file still exists or stat error = %v, want removed", err)
	}
	if len(remote.deletedFileIDs) != 1 || remote.deletedFileIDs[0] != 44 {
		t.Fatalf("deletedFileIDs = %#v, want [44]", remote.deletedFileIDs)
	}
	if len(store.deletedPaths) != 1 || store.deletedPaths[0] != "/file.txt" {
		t.Fatalf("deletedPaths = %#v, want [/file.txt]", store.deletedPaths)
	}
	if !strings.Contains(out.String(), "Removed /file.txt") {
		t.Fatalf("output = %q, want Removed message", out.String())
	}
}

func TestRemoveFileFromStateRemoteOnlyDoesNotRequireLocalTranslationRegression(t *testing.T) {
	store := &fakeRmStore{}
	remote := &fakeRmRemote{}
	out := &strings.Builder{}

	r := &remover{
		opts: rmOpts{
			RemoteOnly: true,
			Out:        out,
		},
		project:           testRmProject(),
		store:             store,
		remoteFileDeleter: remote,
		translator:        mc.ProjectPathTranslator{},
	}

	err := r.removeFileFromState(context.Background(), reconcile.FileState{
		Observation: reconcile.Observation{
			RemotePath: "/remote-only.txt",
			RemoteEntry: &reconcile.RemoteEntry{
				Path:         "/remote-only.txt",
				Name:         "remote-only.txt",
				Dir:          "/",
				Kind:         reconcile.KindFile,
				RemoteFileID: int64Ptr(45),
			},
		},
		Decision: reconcile.Decision{
			Action: reconcile.ActionDownload,
			Reason: "safe to remove",
		},
	})
	if err != nil {
		t.Fatalf("removeFileFromState() error = %v, want nil for remote-only removal without local translation", err)
	}

	if len(remote.deletedFileIDs) != 1 || remote.deletedFileIDs[0] != 45 {
		t.Fatalf("deletedFileIDs = %#v, want [45]", remote.deletedFileIDs)
	}
}

func TestRemoveFileFromStateLocalOnlyDBPolicyRegression(t *testing.T) {
	projectRoot := t.TempDir()
	localPath := filepath.Join(projectRoot, "file.txt")
	if err := os.WriteFile(localPath, []byte("contents"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	translator, err := mc.NewProjectPathTranslator(projectRoot)
	if err != nil {
		t.Fatalf("NewProjectPathTranslator() error = %v", err)
	}

	store := &fakeRmStore{}
	r := &remover{
		opts: rmOpts{
			LocalOnly: true,
			Out:       &strings.Builder{},
		},
		project:    testRmProject(),
		store:      store,
		translator: translator,
	}

	err = r.removeFileFromState(context.Background(), reconcile.FileState{
		Observation: reconcile.Observation{
			RemotePath: "/file.txt",
			LocalEntry: &reconcile.LocalEntry{
				Path: localPath,
				Kind: reconcile.KindFile,
			},
			RemoteEntry: &reconcile.RemoteEntry{
				Path:         "/file.txt",
				Name:         "file.txt",
				Dir:          "/",
				Kind:         reconcile.KindFile,
				RemoteFileID: int64Ptr(46),
			},
		},
		Decision: reconcile.Decision{
			Action: reconcile.ActionDownload,
		},
	})
	if err != nil {
		t.Fatalf("removeFileFromState() error = %v", err)
	}

	if len(store.deletedPaths) != 0 {
		t.Fatalf("deletedPaths = %#v, want DB record preserved for local-only removal while remote still exists", store.deletedPaths)
	}
}

func TestRemoveFileFromStateRemoteOnlyDBPolicyRegression(t *testing.T) {
	projectRoot := t.TempDir()
	translator, err := mc.NewProjectPathTranslator(projectRoot)
	if err != nil {
		t.Fatalf("NewProjectPathTranslator() error = %v", err)
	}

	store := &fakeRmStore{}
	remote := &fakeRmRemote{}
	r := &remover{
		opts: rmOpts{
			RemoteOnly: true,
			Out:        &strings.Builder{},
		},
		project:           testRmProject(),
		store:             store,
		remoteFileDeleter: remote,
		translator:        translator,
	}

	err = r.removeFileFromState(context.Background(), reconcile.FileState{
		Observation: reconcile.Observation{
			RemotePath: "/file.txt",
			RemoteEntry: &reconcile.RemoteEntry{
				Path:         "/file.txt",
				Name:         "file.txt",
				Dir:          "/",
				Kind:         reconcile.KindFile,
				RemoteFileID: int64Ptr(47),
			},
		},
		Decision: reconcile.Decision{
			Action: reconcile.ActionDownload,
		},
	})
	if err != nil {
		t.Fatalf("removeFileFromState() error = %v", err)
	}

	if len(store.deletedPaths) != 0 {
		t.Fatalf("deletedPaths = %#v, want DB record policy not to blindly delete metadata for remote-only removal", store.deletedPaths)
	}
}

func TestRemoveFileFromStateDryRunDefaultReportsLocalAndRemoteRegression(t *testing.T) {
	projectRoot := t.TempDir()
	translator, err := mc.NewProjectPathTranslator(projectRoot)
	if err != nil {
		t.Fatalf("NewProjectPathTranslator() error = %v", err)
	}

	out := &strings.Builder{}
	r := &remover{
		opts: rmOpts{
			DryRun: true,
			Out:    out,
		},
		project:           testRmProject(),
		store:             &fakeRmStore{},
		remoteFileDeleter: &fakeRmRemote{},
		translator:        translator,
	}

	err = r.removeFileFromState(context.Background(), reconcile.FileState{
		Observation: reconcile.Observation{
			RemotePath: "/file.txt",
			RemoteEntry: &reconcile.RemoteEntry{
				Path:         "/file.txt",
				Name:         "file.txt",
				Dir:          "/",
				Kind:         reconcile.KindFile,
				RemoteFileID: int64Ptr(48),
			},
		},
		Decision: reconcile.Decision{
			Action: reconcile.ActionDownload,
		},
	})
	if err != nil {
		t.Fatalf("removeFileFromState() error = %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "local") || !strings.Contains(got, "remote") {
		t.Fatalf("output = %q, want dry-run output to make default local+remote removal explicit", got)
	}
}

func TestRemoveDirectoryRecursiveRemovesDirectoryNodesRegression(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()

	translator, err := mc.NewProjectPathTranslator(projectRoot)
	if err != nil {
		t.Fatalf("NewProjectPathTranslator() error = %v", err)
	}

	remote := &fakeRmRemote{
		listByPath: map[string][]mcmodel.File{
			"/Dir": {
				rmDirModel(101, "/Dir/Sub", "Sub"),
			},
			"/Dir/Sub": {
				rmFileModel(102, "/Dir/Sub/file.txt", "file.txt"),
			},
		},
	}
	store := &fakeRmStore{
		recordsByPath: map[string]filedb.FileRecord{
			"/Dir/Sub/file.txt": {
				Path:         "/Dir/Sub/file.txt",
				Dir:          "/Dir/Sub",
				Name:         "file.txt",
				RemoteFileID: int64Ptr(102),
			},
			"/Dir/Sub": {
				Path:         "/Dir/Sub",
				Dir:          "/Dir",
				Name:         "Sub",
				RemoteFileID: int64Ptr(101),
			},
		},
	}
	out := &strings.Builder{}

	r := &remover{
		opts: rmOpts{
			Recursive:  true,
			RemoteOnly: true,
			Out:        out,
		},
		project:           testRmProject(),
		store:             store,
		remoteGetter:      remote,
		remoteFileDeleter: remote,
		translator:        translator,
		reconciler:        reconcile.New(reconcile.ModeDownload),
	}

	err = r.removeDirectory(ctx, "/Dir")
	if err != nil {
		t.Fatalf("removeDirectory() error = %v", err)
	}

	if !containsInt(remote.deletedFileIDs, 102) {
		t.Fatalf("deletedFileIDs = %#v, want file id 102 removed", remote.deletedFileIDs)
	}
	if !containsInt(remote.deletedFileIDs, 101) {
		t.Fatalf("deletedFileIDs = %#v, want directory id 101 removed too", remote.deletedFileIDs)
	}
}

func TestRemovePathRecursiveRemovesRequestedRootDirectoryRegression(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()

	translator, err := mc.NewProjectPathTranslator(projectRoot)
	if err != nil {
		t.Fatalf("NewProjectPathTranslator() error = %v", err)
	}

	remote := &fakeRmRemote{
		filesByPath: map[string]*mcmodel.File{
			"/Dir": mcModelPtr(rmDirModel(100, "/Dir", "Dir")),
		},
		listByPath: map[string][]mcmodel.File{
			"/Dir": {
				rmDirModel(101, "/Dir/Sub", "Sub"),
			},
			"/Dir/Sub": {
				rmFileModel(102, "/Dir/Sub/file.txt", "file.txt"),
			},
		},
	}
	store := &fakeRmStore{
		recordsByPath: map[string]filedb.FileRecord{
			"/Dir": {
				Path:         "/Dir",
				Dir:          "/",
				Name:         "Dir",
				RemoteFileID: int64Ptr(100),
			},
			"/Dir/Sub": {
				Path:         "/Dir/Sub",
				Dir:          "/Dir",
				Name:         "Sub",
				RemoteFileID: int64Ptr(101),
			},
			"/Dir/Sub/file.txt": {
				Path:         "/Dir/Sub/file.txt",
				Dir:          "/Dir/Sub",
				Name:         "file.txt",
				RemoteFileID: int64Ptr(102),
			},
		},
	}

	r := &remover{
		opts: rmOpts{
			Recursive:  true,
			RemoteOnly: true,
			Out:        &strings.Builder{},
		},
		project:           testRmProject(),
		store:             store,
		remoteGetter:      remote,
		remoteFileDeleter: remote,
		translator:        translator,
		reconciler:        reconcile.New(reconcile.ModeDownload),
	}

	err = r.removePath(ctx, "/Dir")
	if err != nil {
		t.Fatalf("removePath() error = %v", err)
	}

	if !containsInt(remote.deletedFileIDs, 102) {
		t.Fatalf("deletedFileIDs = %#v, want file id 102 removed", remote.deletedFileIDs)
	}
	if !containsInt(remote.deletedFileIDs, 101) {
		t.Fatalf("deletedFileIDs = %#v, want child directory id 101 removed", remote.deletedFileIDs)
	}
	if !containsInt(remote.deletedFileIDs, 100) {
		t.Fatalf("deletedFileIDs = %#v, want requested root directory id 100 removed", remote.deletedFileIDs)
	}
}

func TestRemoveDirectoryRecursiveDefaultRemovesLocalDirectoriesRegression(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	localDir := filepath.Join(projectRoot, "Dir")
	if err := os.MkdirAll(filepath.Join(localDir, "Sub"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(localDir, "Sub", "file.txt"), []byte("contents"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	translator, err := mc.NewProjectPathTranslator(projectRoot)
	if err != nil {
		t.Fatalf("NewProjectPathTranslator() error = %v", err)
	}

	remote := &fakeRmRemote{
		listByPath: map[string][]mcmodel.File{
			"/Dir": {
				rmDirModel(201, "/Dir/Sub", "Sub"),
			},
			"/Dir/Sub": {
				rmFileModel(202, "/Dir/Sub/file.txt", "file.txt"),
			},
		},
	}
	store := &fakeRmStore{
		recordsByPath: map[string]filedb.FileRecord{},
	}

	r := &remover{
		opts: rmOpts{
			Recursive: true,
			Out:       &strings.Builder{},
		},
		project:           testRmProject(),
		store:             store,
		remoteGetter:      remote,
		remoteFileDeleter: remote,
		translator:        translator,
		reconciler:        reconcile.New(reconcile.ModeDownload),
	}

	err = r.removeDirectory(ctx, "/Dir")
	if err != nil {
		t.Fatalf("removeDirectory() error = %v", err)
	}

	if _, err := os.Stat(localDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("local directory stat error = %v, want recursive local directory removed", err)
	}
}

func TestRemoveLocalOnlyReconciledFileDoesNotRemoveUnsafeDecisionRegression(t *testing.T) {
	projectRoot := t.TempDir()
	localPath := filepath.Join(projectRoot, "unsafe-local-only.txt")
	if err := os.WriteFile(localPath, []byte("contents"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	store := &fakeRmStore{}
	out := &strings.Builder{}
	r := &remover{
		opts: rmOpts{
			LocalOnly: true,
			Out:       out,
		},
		store: store,
	}

	err := r.removeLocalOnlyReconciledFile(context.Background(), reconcile.FileState{
		Observation: reconcile.Observation{
			RemotePath: "/unsafe-local-only.txt",
			LocalEntry: &reconcile.LocalEntry{
				Path: localPath,
				Kind: reconcile.KindFile,
			},
		},
		Decision: reconcile.Decision{
			Action: reconcile.ActionUpload,
			Reason: "local-only file has not been uploaded",
		},
	})
	if err != nil {
		t.Fatalf("removeLocalOnlyReconciledFile() error = %v", err)
	}

	if _, err := os.Stat(localPath); err != nil {
		t.Fatalf("local file stat error = %v, want unsafe local-only file preserved", err)
	}
	if len(store.deletedPaths) != 0 {
		t.Fatalf("deletedPaths = %#v, want none", store.deletedPaths)
	}
	if !strings.Contains(out.String(), "Skipping") {
		t.Fatalf("output = %q, want skip message", out.String())
	}
}

func TestRemoveLocalOnlyReconciledDirectoryRequiresRecursiveRegression(t *testing.T) {
	projectRoot := t.TempDir()
	localDir := filepath.Join(projectRoot, "LocalDir")
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	r := &remover{
		opts: rmOpts{
			LocalOnly: true,
			Out:       &strings.Builder{},
		},
		store: &fakeRmStore{},
	}

	err := r.removeLocalOnlyReconciledFile(context.Background(), reconcile.FileState{
		Observation: reconcile.Observation{
			RemotePath: "/LocalDir",
			LocalEntry: &reconcile.LocalEntry{
				Path: localDir,
				Kind: reconcile.KindDir,
			},
		},
		Decision: reconcile.Decision{
			Action: reconcile.ActionDownload,
		},
	})

	if err == nil {
		t.Fatal("removeLocalOnlyReconciledFile() error = nil, want directory removal to require --recursive")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Fatalf("removeLocalOnlyReconciledFile() error = %v, want directory error", err)
	}
	if _, statErr := os.Stat(localDir); statErr != nil {
		t.Fatalf("local dir stat error = %v, want directory preserved", statErr)
	}
}

func TestRemoveLocalOnlyReconciledDirectoryRecursiveRemovesTreeRegression(t *testing.T) {
	projectRoot := t.TempDir()
	localDir := filepath.Join(projectRoot, "LocalDir")
	if err := os.MkdirAll(filepath.Join(localDir, "Sub"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(localDir, "Sub", "file.txt"), []byte("contents"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	r := &remover{
		opts: rmOpts{
			LocalOnly: true,
			Recursive: true,
			Out:       &strings.Builder{},
		},
		store: &fakeRmStore{},
	}

	err := r.removeLocalOnlyReconciledFile(context.Background(), reconcile.FileState{
		Observation: reconcile.Observation{
			RemotePath: "/LocalDir",
			LocalEntry: &reconcile.LocalEntry{
				Path: localDir,
				Kind: reconcile.KindDir,
			},
		},
		Decision: reconcile.Decision{
			Action: reconcile.ActionDownload,
		},
	})
	if err != nil {
		t.Fatalf("removeLocalOnlyReconciledFile() error = %v", err)
	}

	if _, statErr := os.Stat(localDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("local dir stat error = %v, want recursive directory tree removed", statErr)
	}
}

func TestNewRemoverDoesNotCloseStoreBeforeUseRegression(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectRoot, ".mc"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.mc) error = %v", err)
	}

	project := config.Project{
		ProjectID: 123,
		Remote: config.Remote{
			MCURL: "https://example.test/api",
			Email: "test@example.test",
		},
	}
	if err := config.SaveProject(ctx, projectRoot, project); err != nil {
		t.Fatalf("SaveProject() error = %v", err)
	}
	loadedProject, err := config.LoadProject(ctx, projectRoot)
	if err != nil {
		t.Fatalf("LoadProject() error = %v", err)
	}

	store := &fakeRmStore{}
	remote := &fakeRmRemote{}
	deps := di.Dependencies{
		LoadProject: func(ctx context.Context, start string) (config.Project, error) {
			return loadedProject, nil
		},
		LoadGlobal: func(ctx context.Context, path string) (config.Global, error) {
			return config.Global{
				DefaultRemote: config.Remote{
					MCURL:  "https://example.test/api",
					Email:  "test@example.test",
					APIKey: "secret",
				},
			}, nil
		},
		OpenStore: func(ctx context.Context, projectRoot string) (di.Store, error) {
			return store, nil
		},
		NewRemoteClient: func(project config.Project, global config.Global) (di.RemoteClient, error) {
			return remote, nil
		},
		Now: time.Now,
	}

	remover, err := newRemover(deps, ctx, rmOpts{
		WorkingDir: projectRoot,
		Out:        &strings.Builder{},
	})
	if err != nil {
		t.Fatalf("newRemover() error = %v", err)
	}
	if remover == nil {
		t.Fatal("newRemover() = nil, want remover")
	}

	if store.closed {
		t.Fatal("store was closed before returned remover could use it")
	}
}

func TestRemovePathLocalOnlyForceIgnoresRemoteServerErrorRegression(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	localPath := filepath.Join(projectRoot, "local.txt")
	if err := os.WriteFile(localPath, []byte("contents"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	translator, err := mc.NewProjectPathTranslator(projectRoot)
	if err != nil {
		t.Fatalf("NewProjectPathTranslator() error = %v", err)
	}

	r := &remover{
		opts: rmOpts{
			LocalOnly: true,
			Force:     true,
			Out:       &strings.Builder{},
		},
		project: testRmProject(),
		store:   &fakeRmStore{},
		remoteGetter: &fakeRmRemote{
			getErr: &mcapi.APIError{
				StatusCode: http.StatusInternalServerError,
				Status:     "500 Internal Server Error",
			},
		},
		translator: translator,
		reconciler: reconcile.New(reconcile.ModeDownload),
	}

	err = r.removePath(ctx, "/local.txt")
	if err != nil {
		t.Fatalf("removePath() error = %v, want local-only force removal to ignore remote lookup failure", err)
	}

	if _, statErr := os.Stat(localPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("local file stat error = %v, want local file removed", statErr)
	}
}

func TestRemovePathLocalOnlyIgnoresRemoteServerErrorRegression(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	localPath := filepath.Join(projectRoot, "local.txt")
	if err := os.WriteFile(localPath, []byte("contents"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	translator, err := mc.NewProjectPathTranslator(projectRoot)
	if err != nil {
		t.Fatalf("NewProjectPathTranslator() error = %v", err)
	}

	r := &remover{
		opts: rmOpts{
			LocalOnly: true,
			Out:       &strings.Builder{},
		},
		project: testRmProject(),
		store:   &fakeRmStore{},
		remoteGetter: &fakeRmRemote{
			getErr: &mcapi.APIError{
				StatusCode: http.StatusInternalServerError,
				Status:     "500 Internal Server Error",
			},
		},
		translator: translator,
		reconciler: reconcile.New(reconcile.ModeDownload),
	}

	err = r.removePath(ctx, "/local.txt")
	if err == nil {
		t.Fatalf("removePath() returned nil, want local-only removal without force to fail when remote lookup fails")
	}

	if _, statErr := os.Stat(localPath); statErr != nil {
		t.Fatalf("local file stat error = %v, file should still exist when local-only, force is false, and remote lookup failed", statErr)
	}
}

func TestNormalizeRemotePathAbsoluteLocalPathUnderProjectRegression(t *testing.T) {
	projectRoot := t.TempDir()
	localPath := filepath.Join(projectRoot, "Dir", "file.txt")

	translator, err := mc.NewProjectPathTranslator(projectRoot)
	if err != nil {
		t.Fatalf("NewProjectPathTranslator() error = %v", err)
	}

	r := &remover{
		opts: rmOpts{
			WorkingDir: projectRoot,
		},
		translator: translator,
	}

	got, err := r.normalizeRemotePath(localPath)
	if err != nil {
		t.Fatalf("normalizeRemotePath() error = %v", err)
	}

	if got != "/Dir/file.txt" {
		t.Fatalf("normalizeRemotePath(%q) = %q, want /Dir/file.txt for absolute local path under project", localPath, got)
	}
}

func TestNormalizeRemotePathRefusesProjectRootWithoutForceRegression(t *testing.T) {
	r := &remover{
		opts: rmOpts{
			Recursive: true,
			Force:     false,
		},
	}

	got, err := r.normalizeRemotePath("/")
	if err == nil {
		t.Fatalf("normalizeRemotePath() = %q, nil error; want refusal to remove project root without --force", got)
	}
}

func TestRemoveDirectoryRefusesProjectRootWithoutForceRegression(t *testing.T) {
	projectRoot := t.TempDir()
	translator, err := mc.NewProjectPathTranslator(projectRoot)
	if err != nil {
		t.Fatalf("NewProjectPathTranslator() error = %v", err)
	}

	r := &remover{
		opts: rmOpts{
			Recursive: true,
			Force:     false,
			Out:       &strings.Builder{},
		},
		project:      testRmProject(),
		store:        &fakeRmStore{},
		remoteGetter: &fakeRmRemote{},
		translator:   translator,
		reconciler:   reconcile.New(reconcile.ModeDownload),
	}

	err = r.removeDirectory(context.Background(), "/")
	if err == nil {
		t.Fatal("removeDirectory() error = nil, want refusal to remove project root without --force")
	}
}

func TestRemoveFileFromStateForceDefaultRemovesLocalAndRemoteRegression(t *testing.T) {
	projectRoot := t.TempDir()
	localPath := filepath.Join(projectRoot, "force.txt")
	if err := os.WriteFile(localPath, []byte("contents"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	translator, err := mc.NewProjectPathTranslator(projectRoot)
	if err != nil {
		t.Fatalf("NewProjectPathTranslator() error = %v", err)
	}

	remote := &fakeRmRemote{}
	store := &fakeRmStore{}
	r := &remover{
		opts: rmOpts{
			Force: true,
			Out:   &strings.Builder{},
		},
		project:           testRmProject(),
		store:             store,
		remoteFileDeleter: remote,
		translator:        translator,
	}

	err = r.removeFileFromState(context.Background(), reconcile.FileState{
		Observation: reconcile.Observation{
			RemotePath: "/force.txt",
			LocalEntry: &reconcile.LocalEntry{
				Path: localPath,
				Kind: reconcile.KindFile,
			},
			RemoteEntry: &reconcile.RemoteEntry{
				Path:         "/force.txt",
				Name:         "force.txt",
				Dir:          "/",
				Kind:         reconcile.KindFile,
				RemoteFileID: int64Ptr(88),
			},
		},
		Decision: reconcile.Decision{
			Action: reconcile.ActionConflict,
			Reason: "conflict would normally skip",
		},
	})
	if err != nil {
		t.Fatalf("removeFileFromState() error = %v", err)
	}

	if _, statErr := os.Stat(localPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("local file stat error = %v, want removed by --force", statErr)
	}
	if len(remote.deletedFileIDs) != 1 || remote.deletedFileIDs[0] != 88 {
		t.Fatalf("deletedFileIDs = %#v, want [88]", remote.deletedFileIDs)
	}
}

func TestRemoveLocalOnlyReconciledFileDBDeleteErrorIsContextualRegression(t *testing.T) {
	projectRoot := t.TempDir()
	localPath := filepath.Join(projectRoot, "file.txt")
	if err := os.WriteFile(localPath, []byte("contents"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	dbErr := errors.New("db is locked")
	r := &remover{
		opts: rmOpts{
			LocalOnly: true,
			Out:       &strings.Builder{},
		},
		store: &fakeRmStore{
			deleteErr: dbErr,
		},
	}

	err := r.removeLocalOnlyReconciledFile(context.Background(), reconcile.FileState{
		Observation: reconcile.Observation{
			RemotePath: "/file.txt",
			LocalEntry: &reconcile.LocalEntry{
				Path: localPath,
				Kind: reconcile.KindFile,
			},
		},
		Decision: reconcile.Decision{
			Action: reconcile.ActionDownload,
		},
	})
	if !errors.Is(err, dbErr) {
		t.Fatalf("removeLocalOnlyReconciledFile() error = %v, want wrapped dbErr", err)
	}
	if !strings.Contains(err.Error(), "/file.txt") {
		t.Fatalf("removeLocalOnlyReconciledFile() error = %v, want path context", err)
	}
}

func rmFileModel(id int, remotePath, name string) mcmodel.File {
	return mcmodel.File{
		ID:        id,
		Path:      remotePath,
		Name:      name,
		Size:      8,
		MimeType:  "text/plain",
		Checksum:  "checksum",
		CreatedAt: time.Unix(10, 0),
		UpdatedAt: time.Unix(20, 0),
	}
}

func rmDirModel(id int, remotePath, name string) mcmodel.File {
	return mcmodel.File{
		ID:        id,
		Path:      remotePath,
		Name:      name,
		MimeType:  "directory",
		CreatedAt: time.Unix(10, 0),
		UpdatedAt: time.Unix(20, 0),
	}
}

func containsInt(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type fakeRmStore struct {
	recordsByPath map[string]filedb.FileRecord
	getErr        error
	listErr       error
	deleteErr     error
	upsertErr     error

	deletedPaths []string
	upserts      []filedb.FileRecord
	closed       bool
}

func (s *fakeRmStore) GetByPath(ctx context.Context, filePath string) (filedb.FileRecord, error) {
	if s.getErr != nil {
		return filedb.FileRecord{}, s.getErr
	}

	record, ok := s.recordsByPath[filePath]
	if !ok {
		return filedb.FileRecord{}, filedb.ErrRecordNotFound
	}

	return record, nil
}

func (s *fakeRmStore) ListByDir(ctx context.Context, dir string) ([]filedb.FileRecord, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}

	var records []filedb.FileRecord
	for _, record := range s.recordsByPath {
		if record.Dir == dir {
			records = append(records, record)
		}
	}

	return records, nil
}

func (s *fakeRmStore) DeleteByPath(ctx context.Context, filePath string) error {
	s.deletedPaths = append(s.deletedPaths, filePath)
	if s.deleteErr != nil {
		return s.deleteErr
	}
	return nil
}

func (s *fakeRmStore) Close(ctx context.Context) error {
	s.closed = true
	return nil
}

func (s *fakeRmStore) Upsert(ctx context.Context, record filedb.FileRecord) error {
	s.upserts = append(s.upserts, record)
	if s.upsertErr != nil {
		return s.upsertErr
	}
	return nil
}

type fakeRmRemote struct {
	filesByPath map[string]*mcmodel.File
	listByPath  map[string][]mcmodel.File

	getErr    error
	listErr   error
	deleteErr error

	gotProjectID   int
	gotGetPath     string
	gotListPath    string
	deletedFileIDs []int
}

func (r *fakeRmRemote) GetFile(projectID, fileID int) (*mcmodel.File, error) {
	return nil, fmt.Errorf("GetFile should not be called by rm tests")
}

func (r *fakeRmRemote) GetFileByPath(projectID int, path string) (*mcmodel.File, error) {
	r.gotProjectID = projectID
	r.gotGetPath = path

	if r.getErr != nil {
		return nil, r.getErr
	}

	file, ok := r.filesByPath[path]
	if !ok {
		return nil, fmt.Errorf("file not configured for path %q", path)
	}

	return file, nil
}

func (r *fakeRmRemote) ListDirectoryByPath(projectID int, path string) ([]mcmodel.File, error) {
	r.gotProjectID = projectID
	r.gotListPath = path

	if r.listErr != nil {
		return nil, r.listErr
	}

	return r.listByPath[path], nil
}

func (r *fakeRmRemote) DeleteFile(projectID int, fileID int) error {
	r.gotProjectID = projectID
	r.deletedFileIDs = append(r.deletedFileIDs, fileID)

	if r.deleteErr != nil {
		return r.deleteErr
	}

	return nil
}

func testRmProject() config.Project {
	return config.Project{ProjectID: 123}
}

func int64Ptr(v int64) *int64 {
	return &v
}

func mcModelPtr(file mcmodel.File) *mcmodel.File {
	return &file
}
