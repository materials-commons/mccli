package cmds

import (
	"context"

	"github.com/materials-commons/mccli/pkg/di"
)

type RmOpts struct {
	Recursive  bool
	RemoteOnly bool
}

type rmRunner struct {
	deps di.Dependencies
}

func RunRmCmd(ctx context.Context, opts RmOpts, args []string) error {
	return rmRunner{deps: di.Production()}.run(ctx, opts, args)
}

func (r rmRunner) run(ctx context.Context, opts RmOpts, args []string) error {
	return nil
}
