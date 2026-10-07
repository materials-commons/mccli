package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/materials-commons/mccli/internal/mc/file"
)

type fakeFileTyper struct {
	dir bool
}

func (f fakeFileTyper) IsDir() bool {
	return f.dir
}

func TestIsDir(t *testing.T) {
	tests := []struct {
		name string
		info fileTyper
		want bool
	}{
		{
			name: "nil",
			info: nil,
			want: false,
		},
		{
			name: "file",
			info: fakeFileTyper{dir: false},
			want: false,
		},
		{
			name: "directory",
			info: fakeFileTyper{dir: true},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDir(tt.info); got != tt.want {
				t.Fatalf("isDir() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsFile(t *testing.T) {
	tests := []struct {
		name string
		info fileTyper
		want bool
	}{
		{
			name: "nil",
			info: nil,
			want: false,
		},
		{
			name: "file",
			info: fakeFileTyper{dir: false},
			want: true,
		},
		{
			name: "directory",
			info: fakeFileTyper{dir: true},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isFile(tt.info); got != tt.want {
				t.Fatalf("isFile() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunMvCmdRequiresAtLeastSourceAndDestination(t *testing.T) {
	err := runMvCmd(context.Background(), file.MoverOpts{}, []string{"src.txt"})
	if err == nil {
		t.Fatal("runMvCmd() error = nil, want argument validation error")
	}

	if !strings.Contains(err.Error(), "mv requires at least source and dest arguments") {
		t.Fatalf("runMvCmd() error = %v, want argument validation error", err)
	}
}

func TestMoveRunnerDoMovesRejectsMultipleSourcesToExistingFileDestination(t *testing.T) {
	dest := "dest.txt"

	mr := moveRunner{}
	err := mr.doMoves(context.Background(), []string{"src1.txt", "src2.txt", dest}, func(path string) (fileTyper, error) {
		if path == dest {
			return fakeFileTyper{dir: false}, nil
		}

		return fakeFileTyper{dir: false}, nil
	})

	if err == nil {
		t.Fatal("doMoves() error = nil, want destination file error")
	}

	if !strings.Contains(err.Error(), "destination file already exists") {
		t.Fatalf("doMoves() error = %v, want destination file error", err)
	}
}

func TestMoveRunnerDoMovesReturnsSourceLookupError(t *testing.T) {
	wantErr := errors.New("source lookup failed")

	mr := moveRunner{}
	err := mr.doMoves(context.Background(), []string{"missing.txt", "dest"}, func(path string) (fileTyper, error) {
		switch path {
		case "dest":
			return nil, os.ErrNotExist
		case "missing.txt":
			return nil, wantErr
		default:
			t.Fatalf("unexpected path lookup %q", path)
			return nil, nil
		}
	})

	if !errors.Is(err, wantErr) {
		t.Fatalf("doMoves() error = %v, want %v", err, wantErr)
	}
}

func TestMoveRunnerDoMovesRejectsFileOverExistingFile(t *testing.T) {
	mr := moveRunner{}
	err := mr.doMoves(context.Background(), []string{"src.txt", "dest.txt"}, func(path string) (fileTyper, error) {
		switch path {
		case "dest.txt":
			return fakeFileTyper{dir: false}, nil
		case "src.txt":
			return fakeFileTyper{dir: false}, nil
		default:
			t.Fatalf("unexpected path lookup %q", path)
			return nil, nil
		}
	})

	if err == nil {
		t.Fatal("doMoves() error = nil, want existing file error")
	}

	if !strings.Contains(err.Error(), "cannot move over an existing file") {
		t.Fatalf("doMoves() error = %v, want existing file error", err)
	}
}

func TestMoveRunnerDoMovesRejectsDirectoryToExistingFile(t *testing.T) {
	mr := moveRunner{}
	err := mr.doMoves(context.Background(), []string{"src-dir", "dest.txt"}, func(path string) (fileTyper, error) {
		switch path {
		case "dest.txt":
			return fakeFileTyper{dir: false}, nil
		case "src-dir":
			return fakeFileTyper{dir: true}, nil
		default:
			t.Fatalf("unexpected path lookup %q", path)
			return nil, nil
		}
	})

	if err == nil {
		t.Fatal("doMoves() error = nil, want directory to file error")
	}

	if !strings.Contains(err.Error(), "cannot move a directory to a file") {
		t.Fatalf("doMoves() error = %v, want directory to file error", err)
	}
}

func TestMoveRunnerRunUsesLocalStatWhenNotRemoteOnly(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "src.txt")
	dest := filepath.Join(tmpDir, "dest.txt")

	if err := os.WriteFile(src, []byte("contents"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(dest, []byte("contents"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	mr := moveRunner{}
	err := mr.run(context.Background(), file.MoverOpts{MoveLocalOnly: true}, []string{src, dest})
	if err == nil {
		t.Fatal("run() error = nil, want validation error from local stat")
	}

	if !strings.Contains(err.Error(), "cannot move over an existing file") {
		t.Fatalf("run() error = %v, want existing file validation error", err)
	}
}

func TestMoveRunnerRunUsesLocalStatWhenMovingBoth(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src-dir")
	dest := filepath.Join(tmpDir, "dest.txt")

	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(dest, []byte("contents"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	mr := moveRunner{}
	err := mr.run(context.Background(), file.MoverOpts{MoveBoth: true}, []string{srcDir, dest})
	if err == nil {
		t.Fatal("run() error = nil, want validation error from local stat")
	}

	if !strings.Contains(err.Error(), "cannot move a directory to a file") {
		t.Fatalf("run() error = %v, want directory to file validation error", err)
	}
}
