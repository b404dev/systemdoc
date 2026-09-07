package dashboard

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func filterLogLines(text, query string) string {
	if query == "" {
		return text
	}
	var matches []string
	query = strings.ToLower(query)
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(strings.ToLower(line), query) {
			matches = append(matches, line)
		}
	}
	return strings.Join(matches, "\n")
}

func (w *workspace) exportView() {
	content := w.detail.GetText(true)
	if w.logDrawer.HasFocus() {
		content = w.drawerLog.text + w.drawerLog.ending
	} else if w.tab == 1 {
		content = filterLogLines(w.inspectorLog.text, w.logQuery) + w.inspectorLog.ending
	}
	field := tview.NewInputField().SetLabel(" Export file ")
	field.SetBorder(true).SetTitle(" Export current inspector · review contents first · never overwrites ")
	field.SetDoneFunc(func(key tcell.Key) {
		w.pages.RemovePage("export")
		w.app.SetFocus(w.table)
		if key == tcell.KeyEscape {
			return
		}
		path := field.GetText()
		if path == "" {
			return
		}
		w.confirm("Export inspector", path+"\n\nThe current visible inspector content will be saved with owner-only permissions. It may contain sensitive configuration or logs.", func() {
			err := writeNewFile(path, content, 0600)
			if err != nil {
				w.message("Export failed", err.Error())
			} else {
				w.message("Export saved", path)
			}
		})
	})
	w.pages.AddPage("export", centered(field, 100, 3), true, true)
}
