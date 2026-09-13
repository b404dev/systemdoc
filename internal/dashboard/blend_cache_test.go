package dashboard

import (
	"sync"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// The memoised blend must return exactly what a fresh Lab computation returns
// for the same quantised step, from any goroutine, for any colour pair.
func TestBlendCacheMatchesFreshComputation(t *testing.T) {
	fresh := func(a, b tcell.Color, step int) tcell.Color {
		mixed := colorfulOf(a).BlendLab(colorfulOf(b), float64(step)/255).Clamped()
		r, g, bl := mixed.RGB255()
		return tcell.NewRGBColor(int32(r), int32(g), int32(bl))
	}
	pairs := [][2]tcell.Color{}
	for _, theme := range themes {
		pairs = append(pairs,
			[2]tcell.Color{tcell.GetColor(theme.accent), tcell.GetColor(theme.glow)},
			[2]tcell.Color{tcell.GetColor(theme.background), tcell.GetColor(theme.accent)},
			[2]tcell.Color{tcell.GetColor(theme.surface), tcell.GetColor(theme.error)})
	}
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for _, pair := range pairs {
				for step := offset; step <= 255; step += 8 {
					amount := float64(step) / 255
					if got, want := blend(pair[0], pair[1], amount), fresh(pair[0], pair[1], step); got.Hex() != want.Hex() {
						t.Errorf("blend(%06x, %06x, %d/255) = %06x, want %06x", pair[0].Hex(), pair[1].Hex(), step, got.Hex(), want.Hex())
						return
					}
				}
			}
		}(worker)
	}
	wg.Wait()
	// Quantisation rounds to the nearest step, so near-neighbours agree and the
	// endpoints are exact.
	a, b := pairs[0][0], pairs[0][1]
	if blend(a, b, 0.5).Hex() != blend(a, b, 0.5+1.0/1024).Hex() {
		t.Error("amounts within one quantisation step must resolve to the same colour")
	}
	if blend(a, b, 0).Hex() != a.Hex() || blend(a, b, 1).Hex() != b.Hex() {
		t.Error("endpoints must be exact")
	}
}
