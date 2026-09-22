package sync

import (
	"context"
	"errors"

	"github.com/DisMosGit/triadsim/internal/model"
)

// refreshSyncE re-runs the source selection and writes the selected source and
// its quality level when they changed. The global switch and the per-interface
// SSM flags are configuration, so the selection is recomputed on every tick and
// follows a configuration change within one period.
func (m *Manager) refreshSyncE(ctx context.Context, device *model.Device) error {
	name, ql, extended := selectSyncESource(device)

	var err error
	if device.SyncE.SelectedSource != name {
		err = errors.Join(err, m.stateWrite(ctx, "synce/selected-source", name))
	}
	if device.SyncE.SelectedQL != ql {
		err = errors.Join(err, m.stateWrite(ctx, "synce/selected-ql", string(ql)))
	}
	if device.SyncE.SelectedExtendedQL != extended {
		err = errors.Join(err, m.stateWrite(ctx, "synce/selected-extended-ql", extended))
	}
	return err
}

// selectSyncESource returns the best synchronization source of the device. A
// candidate must be a SyncE interface with SSM enabled; the best quality level
// wins, then the lowest configured priority, then the interface the PTP
// receiver prefers, and finally the interface name, so the choice is
// deterministic. QL-DNU is never selected, and no candidate yields an empty
// selection.
func selectSyncESource(device *model.Device) (string, model.QL, string) {
	if !device.SyncE.Enabled {
		return "", "", ""
	}

	var best *syncESource
	for _, iface := range device.SyncE.Interfaces {
		if !iface.SSMEnabled {
			continue
		}
		rank, ok := Rank(iface.QL)
		if !ok {
			continue
		}
		candidate := syncESource{
			name:     iface.Name,
			ql:       iface.QL,
			extended: iface.ExtendedQL,
			rank:     rank,
			priority: iface.Priority,
			prefers:  iface.PTPPreference,
		}
		if best == nil || betterSource(candidate, *best) {
			best = &candidate
		}
	}
	if best == nil {
		return "", "", ""
	}
	return best.name, best.ql, best.extended
}

// syncESource is one selectable SyncE interface and its selection key.
type syncESource struct {
	name     string
	ql       model.QL
	extended string
	rank     uint8
	priority uint8
	prefers  bool
}

// betterSource reports whether a beats b in the selection order.
func betterSource(a, b syncESource) bool {
	switch {
	case a.rank != b.rank:
		return a.rank < b.rank
	case a.priority != b.priority:
		return a.priority < b.priority
	case a.prefers != b.prefers:
		return a.prefers
	default:
		return a.name < b.name
	}
}
