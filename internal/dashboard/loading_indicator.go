package dashboard

import "time"

// Reserve two footer cells so starting or finishing a refresh never moves the
// keyboard hints or resizes the workspace. Activity remains available with v.
func (w *workspace) updateLoadingIndicator() {
	busy := w.inventoryJobs[w.mode] != nil
	w.loadingAnimation.Store(busy)
	glyph := ""
	if busy {
		frames := []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")
		glyph = string(frames[(time.Now().UnixMilli()/120)%int64(len(frames))])
	}
	w.loadingIndicator.SetText(glyph)
}
