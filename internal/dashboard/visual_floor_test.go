package dashboard

import (
	"strings"
	"testing"
)

// A host running at a few percent must still show a trail: on the fixed
// 0–100 scale the old renderer rounded it to nothing.
func TestSignalAreaFloorsLiveReadings(t *testing.T) {
	area := signalArea([]float64{3, 5, 0, 7}, 100, "blocks")
	rows := strings.Split(area, "\n")
	if len(rows) != 2 {
		t.Fatalf("expected two rows, got %q", area)
	}
	bottom := []rune(rows[1])
	if bottom[0] != '▁' || bottom[1] != '▁' || bottom[3] != '▁' {
		t.Fatalf("non-zero samples must draw at least one level: %q", rows[1])
	}
	if bottom[2] != ' ' {
		t.Fatalf("a genuine zero stays empty: %q", rows[1])
	}
	if ascii := signalArea([]float64{2}, 100, "ascii"); !strings.HasSuffix(ascii, "#") {
		t.Fatalf("ascii mode must apply the same floor: %q", ascii)
	}
}

func TestEllipsizeKeepsWidth(t *testing.T) {
	if got := ellipsize("short", 10); got != "short" {
		t.Fatalf("text that fits is unchanged: %q", got)
	}
	got := ellipsize("a much longer piece of text", 12)
	if !strings.HasSuffix(got, "…") || displayWidth(got) > 12 {
		t.Fatalf("truncated text must end in an ellipsis within the width: %q (%d)", got, displayWidth(got))
	}
	if got := ellipsize("anything", 0); got != "" {
		t.Fatalf("zero width yields nothing: %q", got)
	}
}
