package dashboard

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rivo/tview"

	"systemdoc/internal/promapi"
)

var pinUnits = []string{promapi.UnitNone, promapi.UnitPercent, promapi.UnitBytes, promapi.UnitBytesPerSecond, promapi.UnitSeconds, promapi.UnitCount, promapi.UnitPerSecond, promapi.UnitLoad, promapi.UnitCelsius}
var pinUnitLabels = []string{"plain number", "percent", "bytes", "bytes per second", "seconds", "count", "per second", "load", "°C"}

// addMetric fetches the server's metric names and offers them in a
// searchable list; the chosen one goes to pinDialog.
func (m *metricsPage) addMetric() {
	if m.endpoint.URL == "" {
		m.w.message("No Prometheus", "Configure or detect a server first; s opens setup.")
		return
	}
	endpoint, ctx := m.endpoint, m.ctx
	go func() {
		client, _, problem := currentPrometheus(ctx, endpoint)
		var names []string
		var err error
		if client != nil {
			names, err = client.MetricNames(ctx, nil, time.Now().Add(-15*time.Minute))
		}
		m.w.queue(func() {
			switch {
			case ctx.Err() != nil:
			case client == nil:
				m.w.message("Prometheus unreachable", problem)
			case err != nil:
				m.w.message("Cannot list metrics", err.Error())
			default:
				m.pickMetric(names)
			}
		})
	}()
}

func (m *metricsPage) pickMetric(names []string) {
	choices := make([]choice, 0, len(names))
	for _, name := range names {
		name := name
		choices = append(choices, choice{name, "Pin this metric as a panel", func() { m.pinDialog(name) }})
	}
	m.w.choose("metrics-add", m.w.iconLabel(iconMetrics, "ADD METRIC · type to search"), choices)
}

// pinDialog asks how to show one metric and saves the pin.
func (m *metricsPage) pinDialog(name string) {
	w := m.w
	form := tview.NewForm()
	form.AddInputField("Title", name, 40, nil, nil).
		AddInputField("Label filter (job=\"node\",mode!=\"idle\")", "", 40, nil, nil).
		AddDropDown("Unit", pinUnitLabels, 0, nil).
		AddCheckbox("Show the rate of change", false, nil).
		AddInputField("Ceiling (blank scales to the window)", "", 12, nil, nil)
	form.SetBorder(true).SetTitle(" PIN · " + tview.Escape(name) + " ")
	close := func() { w.pages.RemovePage("metrics-pin"); m.focusPrimary() }
	form.AddButton("Save", func() {
		pin := pinnedMetric{Title: strings.TrimSpace(form.GetFormItem(0).(*tview.InputField).GetText()), Name: name}
		if labels := strings.TrimSpace(form.GetFormItem(1).(*tview.InputField).GetText()); labels != "" {
			pin.Selector = "{" + strings.Trim(labels, "{}") + "}"
		}
		unit, _ := form.GetFormItem(2).(*tview.DropDown).GetCurrentOption()
		pin.Unit = pinUnits[unit]
		pin.Rate = form.GetFormItem(3).(*tview.Checkbox).IsChecked()
		if text := strings.TrimSpace(form.GetFormItem(4).(*tview.InputField).GetText()); text != "" {
			ceiling, err := strconv.ParseFloat(text, 64)
			if err != nil {
				w.message("Invalid ceiling", "Enter a number or leave it blank.")
				return
			}
			pin.Ceiling = ceiling
		}
		if err := m.savePin(pin); err != nil {
			w.message("Cannot pin metric", err.Error())
			return
		}
		close()
	}).AddButton("Cancel", close)
	w.pages.AddPage("metrics-pin", centered(form, 80, 15), true, true)
	w.app.SetFocus(form)
}

// savePin validates a pin, stores it in settings and shows it as the last panel.
func (m *metricsPage) savePin(pin pinnedMetric) error {
	if err := pin.validate(); err != nil {
		return err
	}
	if len(m.w.settings.PrometheusMetrics) >= maxPinnedMetrics {
		return fmt.Errorf("at most %d pinned metrics; unpin one first", maxPinnedMetrics)
	}
	for _, other := range m.w.settings.PrometheusMetrics {
		if other.Title == pin.Title {
			return fmt.Errorf("the title %q is already used", pin.Title)
		}
	}
	next := m.w.settings
	next.PrometheusMetrics = append(append([]pinnedMetric(nil), m.w.settings.PrometheusMetrics...), pin)
	if !m.w.persistViewSettings(next) {
		return fmt.Errorf("settings were not saved")
	}
	m.rebuildPanels()
	m.selectPanel(len(m.panels) - 1)
	return nil
}

// removePin drops one pin from settings and the page.
func (m *metricsPage) removePin(pin pinnedMetric) {
	next := m.w.settings
	next.PrometheusMetrics = nil
	for _, other := range m.w.settings.PrometheusMetrics {
		if other != pin {
			next.PrometheusMetrics = append(next.PrometheusMetrics, other)
		}
	}
	if m.w.persistViewSettings(next) {
		m.rebuildPanels()
		m.selectPanel(m.selected)
	}
}

// unpinSelected removes the selected pinned panel after review.
func (m *metricsPage) unpinSelected() {
	if m.selected >= len(m.panels) {
		return
	}
	panel := m.panels[m.selected]
	if !panel.pinned {
		m.w.message("Built-in panel", "Only pinned metrics can be removed; built-in panels follow the detected exporters.")
		return
	}
	w := m.w
	w.confirm("unpin · "+panel.spec.Title, "Removes the pinned metric "+panel.pin.Name+panel.pin.Selector+" from settings.json. The server is not changed.", func() { m.removePin(panel.pin) })
}

// rebuildPanels re-derives the panel list after pins change and polls again.
func (m *metricsPage) rebuildPanels() {
	m.panels = panelsFor(m.families, m.names, m.w.settings.PrometheusMetrics)
	m.generation++
	m.busy = false
	m.lastPoll = time.Time{}
	m.render()
}
