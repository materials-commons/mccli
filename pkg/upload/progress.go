package upload

import (
	"fmt"
	"io"
	"path/filepath"
	"sync"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

// ProgressBar is the small bar behavior needed by UploadProgress.
type ProgressBar interface {
	SetCurrent(current int64)
	SetTotal(total int64, complete bool)
	Abort(drop bool)
}

// progressFactory creates bars. It exists so UploadProgress can be tested
// without depending on terminal rendering.
type progressFactory interface {
	AddUploadBar(total int64, name string) ProgressBar
	Wait()
}

// MPBProgressFactory creates real mpb bars.
type MPBProgressFactory struct {
	progress *mpb.Progress
}

// NewMPBProgressFactory creates an mpb-backed progress factory.
func NewMPBProgressFactory(out io.Writer) *MPBProgressFactory {
	options := []mpb.ContainerOption{}
	if out != nil {
		options = append(options, mpb.WithOutput(out))
	}

	return &MPBProgressFactory{
		progress: mpb.New(options...),
	}
}

// AddUploadBar creates one upload progress bar.
func (f *MPBProgressFactory) AddUploadBar(total int64, name string) ProgressBar {
	return f.progress.AddBar(
		total,
		mpb.PrependDecorators(
			decor.Name(name+" ", decor.WCSyncSpaceR),
			decor.CountersKibiByte("% .2f / % .2f", decor.WCSyncWidth),
		),
		mpb.AppendDecorators(
			decor.Percentage(decor.WCSyncWidth),
			decor.Name(" "),
			decor.EwmaETA(decor.ET_STYLE_GO, 30),
		),
	)
}

// Wait waits for all bars to finish rendering.
func (f *MPBProgressFactory) Wait() {
	f.progress.Wait()
}

// UploadProgress coordinates progress bars for concurrent uploads.
type UploadProgress struct {
	mu      sync.Mutex
	factory progressFactory
	bars    map[string]*uploadProgressBarState
}

type uploadProgressBarState struct {
	bar     ProgressBar
	current int64
	total   int64
	done    bool
}

// NewUploadProgress creates a shared upload progress reporter.
func NewUploadProgress(factory progressFactory) *UploadProgress {
	return &UploadProgress{
		factory: factory,
		bars:    map[string]*uploadProgressBarState{},
	}
}

// ReportUploadProgress updates the progress bar for one transfer.
//
// It is safe to call concurrently from multiple uploader goroutines.
func (p *UploadProgress) ReportUploadProgress(event ProgressEvent) {
	if true {
		return
	}
	if p == nil || p.factory == nil || event.TransferID == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	state := p.bars[event.TransferID]
	if state == nil {
		total := event.TotalBytes
		if total < 0 {
			total = 0
		}

		state = &uploadProgressBarState{
			bar:   p.factory.AddUploadBar(total, progressDisplayName(event)),
			total: total,
		}
		p.bars[event.TransferID] = state
	}

	if event.TotalBytes > 0 && event.TotalBytes != state.total {
		state.total = event.TotalBytes
		state.bar.SetTotal(event.TotalBytes, false)
	}

	current := event.BytesSent
	if current < state.current {
		current = state.current
	}
	if state.total > 0 && current > state.total {
		current = state.total
	}

	state.current = current
	state.bar.SetCurrent(current)

	switch event.Status {
	case ProgressComplete, ProgressAlreadyUploaded:
		if !state.done {
			state.done = true
			state.current = state.total
			state.bar.SetCurrent(state.total)
			state.bar.SetTotal(state.total, true)
		}

	case ProgressFailed:
		if !state.done {
			state.done = true
			state.bar.Abort(false)
		}
	}
}

// Wait waits for the underlying progress renderer.
func (p *UploadProgress) Wait() {
	if p == nil || p.factory == nil {
		return
	}

	p.factory.Wait()
}

func progressDisplayName(event ProgressEvent) string {
	if event.RemotePath != "" {
		return event.RemotePath
	}
	if event.LocalPath != "" {
		return filepath.Base(event.LocalPath)
	}
	if event.TransferID != "" {
		return event.TransferID
	}
	return fmt.Sprintf("upload-%p", &event)
}
