package dashboard

import "time"

type detailKey struct {
	mode int
	user bool
	id   string
	tab  int
}

type detailEntry struct {
	text string
	at   time.Time
}

func (w *workspace) cacheDetail(key detailKey, text string) {
	// Logs are live streams, and resource histories already have their own cache.
	if key.tab == 1 || key.tab == 3 || len(text) > 128*1024 {
		return
	}
	if w.detailCache == nil {
		w.detailCache = map[detailKey]detailEntry{}
	}
	if len(w.detailCache) >= 64 {
		var oldest detailKey
		var at time.Time
		for k, entry := range w.detailCache {
			if at.IsZero() || entry.at.Before(at) {
				oldest, at = k, entry.at
			}
		}
		delete(w.detailCache, oldest)
	}
	w.detailCache[key] = detailEntry{text: text, at: time.Now()}
}

func (w *workspace) filterChanged(value string) {
	w.filters[w.mode] = value
	w.refilterRows()
}

func (w *workspace) setQuickFilter(filter int) {
	w.quickFilter = filter
	w.savedQuickFilters[w.mode] = filter
	if w.mode == 0 {
		w.settings.SystemdActiveOnly = filter == 1
		w.savePreferences()
	}
	w.styleNavigation()
	w.refilterRows()
}

func (w *workspace) toggleActiveOnly() {
	filter := 1
	if w.quickFilter == 1 {
		filter = 0
	}
	w.setQuickFilter(filter)
}

func (w *workspace) refilterRows() {
	selected := w.current().ID
	w.redrawRows()
	if selected != w.current().ID {
		w.showDetail()
	}
}
