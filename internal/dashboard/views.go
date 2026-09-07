package dashboard

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type savedView struct {
	Name        string `json:"name"`
	Mode        int    `json:"mode"`
	User        bool   `json:"user"`
	Filter      string `json:"filter"`
	QuickFilter int    `json:"quick_filter"`
	Sort        int    `json:"sort"`
	Favorites   bool   `json:"favorites_only"`
	Layout      string `json:"layout"`
	Tab         int    `json:"tab"`
	Drawer      bool   `json:"log_drawer"`
	Endpoint    string `json:"endpoint,omitempty"`
}

func (v savedView) validate() error {
	if strings.TrimSpace(v.Name) == "" || v.Mode < 0 || v.Mode > 1 || v.QuickFilter < 0 || v.QuickFilter >= len(quickFilterNames) || v.Sort < 0 || v.Sort >= len(sortNames) || v.Tab < 0 || v.Tab >= len(tabNames) {
		return fmt.Errorf("invalid saved view")
	}
	if v.Layout != "" && v.Layout != "auto" && v.Layout != "stacked" && v.Layout != "side-by-side" {
		return fmt.Errorf("invalid saved layout")
	}
	return nil
}
func (w *workspace) captureView(name string) savedView {
	v := savedView{Name: strings.TrimSpace(name), Mode: w.mode, User: w.user, Filter: w.filters[w.mode], QuickFilter: w.quickFilter, Sort: w.sortMode, Favorites: w.favoriteOnly, Layout: w.settings.Layout, Tab: w.tab, Drawer: w.drawerOpen}
	if w.mode == 1 {
		v.Endpoint = dockerEndpoint()
	}
	return v
}
func (w *workspace) applySavedView(v savedView) error {
	if err := v.validate(); err != nil {
		return err
	}
	if v.Mode == 1 && v.Endpoint != dockerEndpoint() {
		return fmt.Errorf("this view belongs to %s; current endpoint is %s", v.Endpoint, dockerEndpoint())
	}
	w.savedQuickFilters[w.mode] = w.quickFilter
	if w.user != v.User && v.Mode == 0 {
		w.user = v.User
		if job := w.inventoryJobs[0]; job != nil {
			job.cancel()
			w.inventoryJobs[0] = nil
		}
		w.items[0] = nil
		w.fleetHistory[0] = nil
		w.lastRefresh[0] = time.Time{}
		w.selected[0] = ""
	}
	w.mode = v.Mode
	w.quickFilter = v.QuickFilter
	w.savedQuickFilters[v.Mode] = v.QuickFilter
	w.sortMode = v.Sort
	w.favoriteOnly = v.Favorites
	w.settings.Layout = v.Layout
	w.tab = v.Tab
	w.drawerOpen = v.Drawer
	w.filters[v.Mode] = v.Filter
	w.search.SetChangedFunc(nil).SetText(v.Filter).SetChangedFunc(w.filterChanged)
	w.zoom = 0
	w.lastWidth = 0
	w.renderTable()
	w.chrome()
	w.load()
	w.app.SetFocus(w.table)
	return nil
}
func (w *workspace) savedViews() {
	choices := []choice{{"Save current view", "Remember mode, scope, filters, sort, layout, tab and log drawer", w.saveViewDialog}}
	for _, v := range w.settings.Views {
		v := v
		scope := "system services"
		if v.User {
			scope = "user services"
		}
		if v.Mode == 1 {
			scope = "containers · " + v.Endpoint
		}
		choices = append(choices, choice{v.Name, scope + " · " + v.Filter, func() {
			w.choose("view-actions", v.Name, []choice{
				{"Open view", "Restore this workspace", func() {
					if err := w.applySavedView(v); err != nil {
						w.message("Cannot open view", err.Error())
					}
				}},
				{"Replace with current view", "Update the saved filters and layout", func() { w.storeView(w.captureView(v.Name)) }},
				{"Delete view", "Remove this saved view", func() {
					next := w.settings
					next.Views = nil
					for _, other := range w.settings.Views {
						if other.Name != v.Name {
							next.Views = append(next.Views, other)
						}
					}
					if w.persistViewSettings(next) {
						w.savedViews()
					}
				}},
			})
		}})
	}
	w.choose("saved-views", "Saved views", choices)
}
func (w *workspace) persistViewSettings(next settings) bool {
	if w.configError != nil {
		w.message("Settings not saved", w.configError.Error())
		return false
	}
	if err := writeSettings(next); err != nil {
		w.message("Settings not saved", err.Error())
		return false
	}
	w.settings = next
	return true
}
func (w *workspace) storeView(v savedView) {
	if err := v.validate(); err != nil {
		w.message("Cannot save view", err.Error())
		return
	}
	next := w.settings
	next.Views = append([]savedView(nil), next.Views...)
	for i, old := range next.Views {
		if old.Name == v.Name {
			next.Views[i] = v
			if w.persistViewSettings(next) {
				w.savedViews()
			}
			return
		}
	}
	next.Views = append(next.Views, v)
	if w.persistViewSettings(next) {
		w.savedViews()
	}
}
func (w *workspace) saveViewDialog() {
	field := tview.NewInputField().SetLabel(" View name ")
	field.SetBorder(true).SetTitle(" Save view · Enter saves · Escape cancels ")
	field.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEscape {
			w.pages.RemovePage("save-view")
			w.app.SetFocus(w.table)
			return
		}
		v := w.captureView(field.GetText())
		if err := v.validate(); err != nil {
			w.message("View name required", err.Error())
			return
		}
		for _, old := range w.settings.Views {
			if old.Name == v.Name {
				w.message("Name already exists", "Choose a different name, or replace the existing view from Saved views.")
				return
			}
		}
		w.pages.RemovePage("save-view")
		w.storeView(v)
	})
	w.pages.AddPage("save-view", centered(field, 80, 3), true, true)
}
