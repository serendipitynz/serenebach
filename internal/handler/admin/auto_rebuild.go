package admin

import (
	"context"
	"errors"
	"log"
)

// maybeAutoRebuild runs the static rebuild when the weblog has
// AutoRebuildOnPublish on. Triggered after entry / category mutations
// so the static snapshot tracks the live DB without a manual click.
//
// Errors are logged but never propagated: the calling handler has
// already committed the save by this point, and a failed rebuild
// must not undo it. A persistent failure is visible from the
// /admin/rebuild page (last-built timestamp, manual retry).
//
// When another rebuild is already running we skip rather than queue.
// The rebuild is fast enough that the operator can click the manual
// button if they want to be sure the latest save landed.
func (h *Handler) maybeAutoRebuild(ctx context.Context) {
	if h.Rebuilder == nil {
		return
	}
	weblog, err := h.Store.WeblogByID(ctx, h.wid())
	if err != nil {
		log.Printf("admin.maybeAutoRebuild: load weblog: %v", err)
		return
	}
	if !weblog.AutoRebuildOnPublish {
		return
	}
	if _, err := h.Rebuilder.Run(ctx, h.Store, h.wid()); err != nil {
		if errors.Is(err, ErrRebuildBusy) {
			log.Printf("admin.maybeAutoRebuild: skipped (another rebuild in flight)")
			return
		}
		log.Printf("admin.maybeAutoRebuild: %v", err)
	}
}
