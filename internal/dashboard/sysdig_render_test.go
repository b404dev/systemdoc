package dashboard

import (
	"strings"
	"testing"

	"github.com/rivo/tview"
)

const sysdigLine = "1234 12:34:56.123456789 0 nginx (4242) > clock_gettime"

func TestCollapseSysdigStreamFoldsIdenticalRuns(t *testing.T) {
	var in strings.Builder
	for i := 0; i < 5; i++ {
		in.WriteString("100" + string(rune('0'+i)) + " 12:00:00.00000000" + string(rune('0'+i)) + " 1 nginx (4242) > clock_gettime\n")
	}
	got := collapseSysdigStream(in.String())
	if got != "1000 12:00:00.000000000 1 nginx (4242) > clock_gettime ×5\n" {
		t.Fatalf("run of 5 was not collapsed to one line:\n%q", got)
	}
	two := "1 12:00:00.000000000 1 nginx (4242) > clock_gettime\n2 12:00:00.000000001 1 nginx (4242) < clock_gettime\n"
	if collapseSysdigStream(two) != two {
		t.Fatalf("different events must not merge:\n%q", collapseSysdigStream(two))
	}
	mixed := "Starting capture\n" + sysdigLine + "\n" + sysdigLine + "\n[red] note\n" + sysdigLine
	want := "Starting capture\n" + sysdigLine + " ×2\n[red] note\n" + sysdigLine
	if got := collapseSysdigStream(mixed); got != want {
		t.Fatalf("non-sysdig lines must pass through and break runs:\n%q", got)
	}
	if collapseSysdigStream("") != "" {
		t.Fatal("empty input must stay empty")
	}
}

func TestSysdigRichPaintsAndEscapes(t *testing.T) {
	p := themes[0]
	got := sysdigRich(sysdigLine+" fd=3 [red] ×3", p)
	for _, want := range []string{
		"[" + p.muted + "]1234 12:34:56.123456789 [-]",
		"[" + p.accent + "]>[-]",
		"[" + p.text + "::b]clock_gettime[-::-]",
		tview.Escape("[red]"),
		"[" + p.warning + "::b] ×3[-::-]",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("sysdigRich missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "[red]") {
		t.Fatalf("external text must be escaped:\n%s", got)
	}
	exit := sysdigRich("1 12:00:00.000000000 0 curl (7) < connect res=-ECONNREFUSED fd=5", p)
	if !strings.Contains(exit, "["+p.glow+"]<[-]") || !strings.Contains(exit, "["+p.error+"] res=-ECONNREFUSED fd=5[-]") {
		t.Fatalf("exit marker or error result not painted:\n%s", exit)
	}
	plain := sysdigRich("sysdig failed: permission denied", p)
	if plain != richOutput("sysdig failed: permission denied", 0, p) {
		t.Fatalf("non-sysdig lines must use richOutput's default treatment:\n%s", plain)
	}
}

func TestSysdigSnapshotKeepsCollapsedTextForExport(t *testing.T) {
	text := sysdigLine + "\n" + sysdigLine + "\n"
	snapshot := sysdigSnapshot(text, "\n[sysdig ended]", logStyle{palette: themes[0]})
	if snapshot.text != sysdigLine+" ×2\n" || !strings.Contains(snapshot.formatted, "×2") || !strings.Contains(snapshot.formatted, "sysdig ended") {
		t.Fatalf("snapshot did not keep the collapsed text and ending:\n%q\n%s", snapshot.text, snapshot.formatted)
	}
	if snapshot.display(logStyle{palette: themes[0]}) != snapshot.formatted {
		t.Fatal("display must return the preformatted sysdig text for the same style")
	}
}
