package dashboard

import (
	"context"
	"fmt"
	"time"
)

type inventoryJob struct {
	cancel context.CancelFunc
	user   bool
}

// Explicit refreshes (including completed operations) replace obsolete work.
func (w *workspace) load() {
	if job := w.inventoryJobs[w.mode]; job != nil {
		job.cancel()
		w.inventoryJobs[w.mode] = nil
	}
	for key := range w.detailCache {
		if key.mode == w.mode {
			delete(w.detailCache, key)
		}
	}
	if w.mode == 1 {
		retryKubeProbe()
	}
	w.startInventory(w.mode)
}

// One worker per backend publishes a fast list, then an enriched snapshot.
// Published slices are copies; all workspace mutation stays on tview's UI loop.
func (w *workspace) startInventory(mode int) {
	if w.ctx.Err() != nil {
		return
	}
	if job := w.inventoryJobs[mode]; job != nil {
		if mode == 1 || job.user == w.user {
			return
		}
		job.cancel()
	}
	ctx, cancel := context.WithCancel(w.ctx)
	job := &inventoryJob{cancel: cancel, user: w.user}
	w.inventoryJobs[mode] = job
	if mode == w.mode {
		w.loading = true
		w.updateDashboard()
	}
	go func() {
		defer cancel()
		items, err := listWorkloads(ctx, mode, job.user)
		if ctx.Err() != nil {
			return
		}
		fast := append([]workload(nil), items...)
		w.queue(func() {
			if !w.currentInventoryJob(mode, job) {
				return
			}
			if err != nil {
				w.inventoryJobs[mode] = nil
				w.backendError[mode] = true
				if mode == w.mode {
					w.loading = false
					w.dismissSplash()
					w.updateDashboard()
					w.detail.SetText(richOutput(err.Error(), 0, w.palette()))
				}
				return
			}
			w.backendError[mode] = false
			w.lastRefresh[mode] = time.Now()
			w.publishInventory(mode, carryAccounting(w.items[mode], fast), false)
		})
		if err != nil || ctx.Err() != nil {
			return
		}
		items = enrichInventory(ctx, mode, job.user, items)
		if ctx.Err() != nil {
			return
		}
		w.queue(func() {
			if !w.currentInventoryJob(mode, job) {
				return
			}
			w.inventoryJobs[mode] = nil
			computeResourceRates(w.items[mode], items)
			w.publishInventory(mode, items, true)
		})
	}()
}

func (w *workspace) currentInventoryJob(mode int, job *inventoryJob) bool {
	return w.inventoryJobs[mode] == job && (mode == 1 || job.user == w.user)
}

// Keep the last accounting values during the short enrichment phase. Newly
// observed workloads remain unknown until their first resource sample arrives.
func carryAccounting(previous, items []workload) []workload {
	old := make(map[string]workload, len(previous))
	for _, item := range previous {
		old[item.ID] = item
	}
	for i, item := range items {
		if prior, ok := old[item.ID]; ok {
			items[i].Enablement = prior.Enablement
			items[i].Aliases = prior.Aliases
			if item.State == prior.State && item.PID == prior.PID {
				items[i].CPU, items[i].Memory = prior.CPU, prior.Memory
				items[i].CPUCounter, items[i].HasCPU, items[i].SampleAt = prior.CPUCounter, prior.HasCPU, prior.SampleAt
			}
		}
		delete(old, item.ID)
	}
	// Avoid briefly removing installed-only services on every refresh.
	for _, item := range previous {
		if _, ok := old[item.ID]; ok && item.UnitFileOnly {
			items = append(items, item)
		}
	}
	return items
}

func (w *workspace) publishInventory(mode int, items []workload, complete bool) {
	previous := w.selected[mode]
	previousPID := 0
	for _, item := range w.items[mode] {
		if item.ID == previous {
			previousPID = item.PID
			break
		}
	}
	for _, item := range items {
		if matchesUnitName(item, previous) {
			w.selected[mode] = item.ID
			break
		}
	}
	w.recordChanges(mode, w.items[mode], items)
	w.pruneHistories(mode, w.items[mode], items)
	w.items[mode] = items
	if complete {
		w.sampleWorkloads(mode, items)
	}
	if mode != w.mode {
		return
	}
	w.loading = false
	w.dismissSplash()
	if complete {
		w.sampleFleet()
	}
	w.redrawRows()
	if previous != w.selected[mode] || (mode == 0 && usesLaunchd() && w.tab == 1 && previousPID != w.current().PID) {
		w.showDetail()
	} else if !complete {
		w.refreshDetail = true
		w.showDetail()
		w.refreshDetail = false
	}
}

// pruneHistories drops the trend and metric histories of workloads that have
// left the inventory. Container IDs and pod names churn on every restart, so
// without this a long session keeps one 60-sample trail for every ID it has
// ever seen.
func (w *workspace) pruneHistories(mode int, previous, current []workload) {
	present := make(map[string]bool, len(current))
	for _, item := range current {
		present[item.ID] = true
	}
	for _, item := range previous {
		if present[item.ID] {
			continue
		}
		delete(w.workloadHistory, fmt.Sprintf("%d/%t/%s", mode, w.user, item.ID))
		delete(w.metrics, fmt.Sprintf("%t/%s", w.user, item.ID))
	}
}

func (w *workspace) dismissSplash() {
	// tview reassigns focus even when RemovePage cannot find the page. Once
	// startup is over, refreshing inventory must leave the reader's focus alone.
	if w.pages.HasPage("splash") {
		w.pages.RemovePage("splash")
	}
	w.splashView = nil
}
