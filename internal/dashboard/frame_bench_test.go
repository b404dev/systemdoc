package dashboard

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

// Frame benchmarks guard the draw loop: a full 160x44 frame is what the
// one-second dashboard tick pays, so a regression here is idle CPU for every
// user. Run with:  go test ./internal/dashboard -run '^$' -bench 'Blend|Frame'

// lerpBlend is the channel-space floor blend is compared against. Lab blending
// costs roughly 100x this uncached; the per-pair cache in blend() brings the
// steady-state cost back to within a few times this figure.
func lerpBlend(a, b tcell.Color, amount float64) tcell.Color {
	ar, ag, ab := a.RGB()
	br, bg, bb := b.RGB()
	return tcell.NewRGBColor(int32(float64(ar)+float64(br-ar)*amount), int32(float64(ag)+float64(bg-ag)*amount), int32(float64(ab)+float64(bb-ab)*amount))
}

func BenchmarkBlendLab(b *testing.B) {
	x, y := tcell.GetColor("#070a10"), tcell.GetColor("#64ddea")
	for i := 0; i < b.N; i++ {
		blend(x, y, float64(i%256)/255)
	}
}

func BenchmarkBlendLerp(b *testing.B) {
	x, y := tcell.GetColor("#070a10"), tcell.GetColor("#64ddea")
	for i := 0; i < b.N; i++ {
		lerpBlend(x, y, float64(i%256)/255)
	}
}

func benchFrame(b *testing.B, w *workspace) {
	screen := tcell.NewSimulationScreen("UTF-8")
	w.app.SetScreen(screen)
	defer screen.Fini()
	screen.SetSize(160, 44)
	w.app.ForceDraw()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.app.ForceDraw()
	}
}

func BenchmarkFrameMain(b *testing.B) { benchFrame(b, testWorkspace()) }

func BenchmarkFrameStorage(b *testing.B) {
	w := testWorkspace()
	w.hostPage(4)
	_, page := w.pages.GetFrontPage()
	h := page.(*hostPage)
	h.mounts, _ = parseMounts(macDiskFixture, "darwin", false)
	h.render()
	benchFrame(b, w)
}
