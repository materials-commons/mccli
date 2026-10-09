package file

import (
	"context"
	"fmt"

	"github.com/materials-commons/mccli/internal/config"
	"github.com/materials-commons/mccli/internal/di"
	"github.com/materials-commons/mccli/internal/mc"
	"github.com/materials-commons/mccli/internal/reconcile"
	"github.com/materials-commons/mccli/internal/services"
)

type Remover2 struct {
	opts                  RemoverOpts
	projectConfig         config.Project
	store                 di.Store
	projectPathTranslator mc.ProjectPathTranslator
	remoteGetter          mc.FileDirectoryGetter
	remoteDeleter         mc.FileDeleter
	reconciler            *reconcile.Reconciler
	container             *services.Container // TODO: Remove if we don't need this
}

func NewRemover2(ctx context.Context, deps di.Dependencies, opts RemoverOpts) (*Remover2, error) {
	var (
		r   Remover2
		err error
	)

	r.opts = opts

	// The mode for the reconciler is set to download. This ensures that any file we are looking to remove
	// has already been uploaded. That way we aren't accidentally deleting files that the user may not realize
	// haven't been uploaded. That is we want to safely delete files, not delete files the user created but
	// hasn't done anything with.
	r.reconciler = reconcile.New(reconcile.ModeDownload)

	r.container, err = services.NewContainer(ctx, deps,
		services.WithProjectConfig(),
		services.WithProjectRoot(),
		services.WithGlobalConfig(),
		services.WithRemote(),
		services.WithProjectPathTranslator(),
		services.WithStore())
	if err != nil {
		return nil, err
	}

	r.projectConfig = r.container.MustProjectConfig()
	r.store = r.container.MustStore()
	r.projectPathTranslator = r.container.MustProjectPathTranslator()

	if r.remoteGetter, r.remoteDeleter, err = getRemoverRemotes(r.container); err != nil {
		return nil, fmt.Errorf("failed to load remote removers: %w", err)
	}

	return &r, nil
}

type deleteFunc func(r *Remover2, fpath string) error
type removerDryrunFunc func(r *Remover2, fpath string)

type removerFuncs struct {
	remoteDeleteFunc deleteFunc
	localDeleteFunc  deleteFunc
	dryrunFunc       removerDryrunFunc
}

func (r *Remover2) RemoveFile(fpath string) error {
	funcs := removerFuncs{
		remoteDeleteFunc: nil,
		localDeleteFunc:  nil,
		dryrunFunc:       nil,
	}
	return r.do(fpath, funcs)
}

func (r *Remover2) RemoveDir(fpath string) error {
	funcs := removerFuncs{
		remoteDeleteFunc: nil,
		localDeleteFunc:  nil,
		dryrunFunc:       nil,
	}
	return r.do(fpath, funcs)
}

func (r *Remover2) do(fpath string, params removerFuncs) error {
	switch {
	case r.opts.DryRun:
		params.dryrunFunc(r, fpath)
		return nil
	case r.opts.LocalOnly:
		return params.localDeleteFunc(r, fpath)

	case r.opts.RemoteOnly:
		return params.remoteDeleteFunc(r, fpath)

	case r.opts.RemoveBoth:
		if err := params.remoteDeleteFunc(r, fpath); err != nil {
			return err
		}
		return params.localDeleteFunc(r, fpath)

	default:
		return fmt.Errorf("invalid remover options")
	}
}
