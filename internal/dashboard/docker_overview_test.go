package dashboard

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rivo/tview"

	"systemdoc/internal/dockerinspect"
)

func TestDockerInspectionRoutesOverviewAndRawConfig(t *testing.T) {
	dir := t.TempDir()
	fixture, err := filepath.Abs("../dockerinspect/testdata/docker-inspect.json")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKER_FIXTURE", fixture)
	script := `#!/bin/sh
[ "$1" = inspect ] && [ "$2" = --type ] && [ "$3" = container ] && [ "$4" = -- ] && [ "$5" = abc ] || exit 90
/bin/cat "$DOCKER_FIXTURE"
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	for _, tab := range []int{0, 2, 4} {
		out, err := inspect(context.Background(), 1, false, workload{ID: "abc"}, tab)
		if err != nil {
			t.Fatal(err)
		}
		if tab == 2 && !strings.Contains(out, "DEMO_SECRET=not-for-overview") {
			t.Fatal("raw inspection was lost")
		}
		if tab != 2 && !strings.Contains(out, "VOLUMES & MOUNTS") {
			t.Fatal("structured view was not used")
		}
	}
	raw := `[{"Id":"abc","Name":"/[red]name","Config":{"Labels":{"label":"[blue]value"}}}]`
	out, err := dockerinspect.Overview(raw, false)
	if err != nil {
		t.Fatal(err)
	}
	plain := tview.NewTextView().SetDynamicColors(true).SetText(richOutput(out, 0, themes[0])).GetText(true)
	if !strings.Contains(plain, "[red]name") || !strings.Contains(plain, "[blue]value") {
		t.Fatal("untrusted Docker markup was interpreted")
	}
}
