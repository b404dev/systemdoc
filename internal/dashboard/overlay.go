package dashboard

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// overlay keeps the live workspace visible around a bounded dialog and shrinks
// to fit smaller terminals. Its content owns keyboard, paste and mouse handling.
type overlay struct {
	*tview.Box
	content       tview.Primitive
	width, height int
}

func centered(content tview.Primitive, width, height int) *overlay {
	return &overlay{Box: tview.NewBox(), content: content, width: width, height: height}
}

func (o *overlay) Draw(screen tcell.Screen) {
	x, y, width, height := o.GetRect()
	w, h := min(width, o.width), min(height, o.height)
	o.content.SetRect(x+(width-w)/2, y+(height-h)/2, w, h)
	o.content.Draw(screen)
}
func (o *overlay) Focus(delegate func(tview.Primitive)) { delegate(o.content) }
func (o *overlay) HasFocus() bool                       { return o.content.HasFocus() }
func (o *overlay) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return o.content.InputHandler()
}
func (o *overlay) PasteHandler() func(string, func(tview.Primitive)) { return o.content.PasteHandler() }
func (o *overlay) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	// Consume clicks outside the dialog so they cannot act on obscured workloads.
	return func(action tview.MouseAction, event *tcell.EventMouse, focus func(tview.Primitive)) (bool, tview.Primitive) {
		if handler := o.content.MouseHandler(); handler != nil {
			_, capture := handler(action, event, focus)
			return true, capture
		}
		return true, nil
	}
}
