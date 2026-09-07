package dashboard

import (
	"context"
	"fmt"
	"time"

	"github.com/rivo/tview"
)

func (w *workspace) toggleLogDrawer() {
	w.drawerOpen = !w.drawerOpen
	w.lastWidth = 0
	if !w.drawerOpen && w.zoom == 3 {
		w.zoom = 0
	}
	if !w.drawerOpen && w.logDrawer.HasFocus() {
		w.app.SetFocus(w.table)
	}
	w.syncLogDrawer()
}

func (w *workspace) syncLogDrawer() {
	item := w.current()
	key := ""
	if w.drawerOpen && item.ID != "" && !(w.mode == 0 && isTemplate(item)) {
		key = fmt.Sprintf("%d/%t/%s", w.mode, w.user, item.ID)
	}
	if key == w.drawerKey {
		if key == "" && w.drawerOpen && w.mode == 0 && isTemplate(item) {
			w.logDrawer.SetText(" Templates have no runtime logs. Select a named instance to follow its journal.")
		}
		return
	}
	w.drawerLog = logSnapshot{}
	w.drawerKey = key
	w.drawerGeneration++
	if w.drawerCancel != nil {
		w.drawerCancel()
		w.drawerCancel = nil
	}
	if key == "" {
		if w.mode == 0 && isTemplate(item) {
			w.logDrawer.SetText(" Templates have no runtime logs. Select a named instance to follow its journal.")
		} else {
			w.logDrawer.SetText(" Select a workload to follow its logs.")
		}
		return
	}
	ctx, cancel := context.WithCancel(w.ctx)
	w.drawerCancel = cancel
	generation, mode, user := w.drawerGeneration, w.mode, w.user
	w.drawerPaused = false
	w.logDrawer.SetTitle(" LOGS · " + tview.Escape(clean(item.Name)) + " · L closes ")
	w.logDrawer.SetText(" Connecting to log stream…")
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(120 * time.Millisecond):
		}
		followLogs(ctx, mode, user, item, func(text, final string) {
			snapshot := prepareLogSnapshot(text, final, *w.drawerLogStyle.Load())
			if ctx.Err() != nil {
				return
			}
			w.queue(func() {
				if generation != w.drawerGeneration || ctx.Err() != nil {
					return
				}
				w.drawerLog = snapshot
				w.renderDrawerLogs()
			})
		})
	}()
}
