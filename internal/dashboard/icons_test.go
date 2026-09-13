package dashboard

import (
	"strings"
	"testing"

	"github.com/rivo/tview"
)

func TestIconVocabularyHasReadableNerdAndFallbackGlyphs(t *testing.T) {
	for role := iconEye; role <= iconConstellation; role++ {
		pair, ok := iconVocabulary[role]
		if !ok || strings.TrimSpace(pair.nerd) == "" || strings.TrimSpace(pair.fallback) == "" {
			t.Fatalf("icon role %d is incomplete: %+v", role, pair)
		}
		if pair.nerd == pair.fallback {
			t.Fatalf("icon role %d does not have a real fallback", role)
		}
	}
}

func TestSuiteLabelsKeepKeysAndNamesInBothIconModes(t *testing.T) {
	for _, nerd := range []bool{false, true} {
		buttons := make([]*tview.Button, 5)
		for i := range buttons {
			buttons[i] = tview.NewButton("")
		}
		setSuiteLabels(buttons, 120, nerd)
		for i, button := range buttons {
			label := button.GetLabel()
			if !strings.Contains(label, string(rune('1'+i))) || !strings.Contains(label, hostTabNames[i]) {
				t.Fatalf("suite label lost readable identity: %q", label)
			}
		}
	}
}
