package reconcile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/materials-commons/mccli/pkg/projectpath"
)

// ObserveLocal observes localPath and converts it into a LocalEntry.
//
// If localPath does not exist, ObserveLocal returns nil, nil.
func ObserveLocal(ctx context.Context, translator projectpath.Translator, localPath string, now time.Time) (*LocalEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	info, err := os.Lstat(localPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat local path %q: %w", localPath, err)
	}

	remotePath, err := translator.LocalToRemote(localPath)
	if err != nil {
		return nil, err
	}

	kind := KindUnknown
	if info.Mode().IsRegular() {
		kind = KindFile
	} else if info.IsDir() {
		kind = KindDir
	}

	remoteDir := path.Dir(remotePath)
	if remoteDir == "." {
		remoteDir = "/"
	}

	return &LocalEntry{
		Path:       localPath,
		RemotePath: remotePath,
		Name:       path.Base(remotePath),
		Dir:        remoteDir,
		Kind:       kind,
		IsSymlink:  info.Mode()&os.ModeSymlink != 0,

		Size:       info.Size(),
		MTimeNS:    info.ModTime().UnixNano(),
		CTimeNS:    ctimeNS(info),
		LastSeenTS: now.Unix(),
	}, nil
}
