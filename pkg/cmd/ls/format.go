package ls

import (
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/materials-commons/mccli/pkg/reconcile"
	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
)

var fullHeaders = []string{
	"l_updated_at",
	"l_size",
	"l_type",
	"l_id",
	"r_updated_at",
	"r_size",
	"r_type",
	"r_id",
	"eq",
	"name",
}

var actionHeaders = []string{
	"name",
	"l_type",
	"r_type",
	"local/remote",
	"action",
	"reason",
}

func printStates(out io.Writer, states map[string]reconcile.FileState, action bool) error {
	rows := sortedStates(states)
	if action {
		return printActionTable(out, rows)
	}
	return printFullTable(out, rows)
}

func printFullTable(out io.Writer, states []reconcile.FileState) error {
	table := tablewriter.NewWriter(out)
	table.Options(tablewriter.WithHeaderAutoFormat(tw.Off))
	table.Header(fullHeaders)

	for _, state := range states {
		if err := table.Append(fullRow(state)); err != nil {
			return fmt.Errorf("render ls full table row: %w", err)
		}
	}

	if err := table.Render(); err != nil {
		return fmt.Errorf("render ls full table: %w", err)
	}

	return nil
}

func printActionTable(out io.Writer, states []reconcile.FileState) error {
	table := tablewriter.NewWriter(out)
	table.Options(tablewriter.WithHeaderAutoFormat(tw.Off))
	table.Header(actionHeaders)

	for _, state := range states {
		if err := table.Append(actionRow(state)); err != nil {
			return fmt.Errorf("render ls action table row: %w", err)
		}
	}

	if err := table.Render(); err != nil {
		return fmt.Errorf("render ls action table: %w", err)
	}

	return nil
}

func fullRow(state reconcile.FileState) []string {
	obs := state.Observation

	lUpdated := "-"
	lSize := "-"
	lType := "-"
	lID := "-"

	if obs.LocalEntry != nil {
		lUpdated = formatUnixNano(obs.LocalEntry.MTimeNS)
		lSize = humanize(obs.LocalEntry.Size)
		lType = kindCode(obs.LocalEntry.Kind)
	}

	rUpdated := "-"
	rSize := "-"
	rType := "-"
	rID := "-"

	if obs.RemoteEntry != nil {
		rUpdated = formatUnixNano(obs.RemoteEntry.MTimeNS)
		rSize = humanize(obs.RemoteEntry.Size)
		rType = kindCode(obs.RemoteEntry.Kind)
		rID = remoteFileIDString(obs.RemoteEntry)
	}

	return []string{
		lUpdated,
		lSize,
		lType,
		lID,
		rUpdated,
		rSize,
		rType,
		rID,
		"-",
		obs.Name,
	}
}

func actionRow(state reconcile.FileState) []string {
	obs := state.Observation

	lType := "-"
	if obs.LocalEntry != nil {
		lType = kindCode(obs.LocalEntry.Kind)
	}

	rType := "-"
	if obs.RemoteEntry != nil {
		rType = kindCode(obs.RemoteEntry.Kind)
	}

	action := displayAction(state.Decision.Action)
	reason := state.Decision.Reason

	if obs.LocalEntry != nil && obs.RemoteEntry != nil &&
		obs.LocalEntry.Kind == reconcile.KindDir &&
		obs.RemoteEntry.Kind == reconcile.KindDir {
		action = string(reconcile.ActionSkip)
		reason = "local and remote directories exist"
	}

	if obs.LocalEntry != nil && obs.RemoteEntry == nil && obs.LocalEntry.Kind == reconcile.KindFile {
		action = string(reconcile.ActionUpload)
	}

	return []string{
		obs.Name,
		lType,
		rType,
		localRemoteLabel(state),
		action,
		reason,
	}
}

func formatUnixNano(ns int64) string {
	if ns == 0 {
		return "-"
	}
	return time.Unix(0, ns).Local().Format("Jan 02  2006")
}

func humanize(size int64) string {
	if size < 0 {
		return "-"
	}

	units := []struct {
		suffix string
		shift  uint
	}{
		{"B", 0},
		{"K", 10},
		{"M", 20},
		{"G", 30},
		{"T", 40},
	}

	for _, unit := range units {
		value := size >> unit.shift
		if value < 1000 || unit.suffix == "T" {
			return strconv.FormatInt(value, 10) + unit.suffix
		}
	}

	return fmt.Sprintf("%dB", size)
}
