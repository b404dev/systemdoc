package dashboard

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// CPU Cores is the per-CPU view the cards only hint at: every logical CPU on
// its own row with its busy share, how that time splits between user code,
// the kernel, waiting on I/O and a hypervisor's other guests, the clock it is
// running at, the core and socket it sits on, its own short trend and, when
// opened from Process Explorer, the busiest process last scheduled on it.
// It reads the samples the host metrics loop already takes every two seconds
// and adds no polling of its own.
type coresView struct {
	w         *workspace
	view      *tview.TextView
	ctx       context.Context
	cancel    context.CancelFunc
	processes func() []hostProcess
	sortBusy  bool
	focus     tview.Primitive
}

const coresPage = "cpu-cores"

// openCPUCores shows the view over the current page. processes may be nil:
// the dashboard has no process snapshot, so the per-CPU process column is
// left out there and the view says where to find it.
func (w *workspace) openCPUCores(processes func() []hostProcess) {
	if w.pages.HasPage(coresPage) {
		return
	}
	ctx, cancel := context.WithCancel(w.ctx)
	c := &coresView{w: w, ctx: ctx, cancel: cancel, processes: processes, sortBusy: true, focus: w.app.GetFocus()}
	p := w.palette()
	c.view = textView().SetDynamicColors(true).SetScrollable(true).SetWrap(false)
	c.view.SetBackgroundColor(tcell.GetColor(p.surface)).SetBorder(true).SetBorderColor(tcell.GetColor(panelHue(p, 3))).SetBorderAttributes(tcell.AttrBold)
	c.view.SetTitle(" " + w.iconLabel(iconResources, "CPU CORES · Esc returns ")).SetTitleColor(tcell.GetColor(panelHue(p, 3)))
	c.view.SetInputCapture(c.input)
	c.render()
	w.pages.AddPage(coresPage, centered(c.view, 150, 8+max(4, len(w.hostCoreHistory))+10), true, true)
	w.app.SetFocus(c.view)
	go c.loop()
}

func (c *coresView) render() {
	row, col := c.view.GetScrollOffset()
	var processes []hostProcess
	if c.processes != nil {
		processes = c.processes()
	}
	_, _, width, _ := c.view.GetInnerRect()
	c.view.SetText(renderCPUCores(c.w.palette(), c.w.settings.GraphMode, c.w.hostUsage, c.w.hostCPUHistory, c.w.hostCoreHistory, processes, c.processes != nil, c.sortBusy, width, time.Now()))
	c.view.ScrollTo(row, col)
}

// loop redraws on the host sampler's cadence. The sampler itself is quiet
// (it never forces a frame), so the view asks for one here while it is open.
func (c *coresView) loop() {
	ticker := time.NewTicker(hostSampleInterval())
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.w.queue(c.render)
		}
	}
}

func (c *coresView) close() {
	c.cancel()
	c.w.pages.RemovePage(coresPage)
	c.w.app.SetFocus(c.focus)
}

func (c *coresView) input(e *tcell.EventKey) *tcell.EventKey {
	switch {
	case e.Key() == tcell.KeyEscape, e.Rune() == 'c', e.Rune() == 'C':
		c.close()
	case e.Rune() == 'S':
		c.sortBusy = !c.sortBusy
		c.render()
	case e.Rune() == 'G':
		c.w.cycleGraphMode()
		c.render()
	case e.Rune() == 'j':
		return tcell.NewEventKey(tcell.KeyDown, 0, 0)
	case e.Rune() == 'k':
		return tcell.NewEventKey(tcell.KeyUp, 0, 0)
	default:
		return e
	}
	return nil
}

// renderCPUCores is the pure renderer behind the view. width is the inner
// width available; the process column and the trend give way first when it
// is short.
func renderCPUCores(p palette, glyphs string, usage hostUtilisation, hostHistory []float64, coreHistory [][]float64, processes []hostProcess, withProcesses, sortBusy bool, width int, now time.Time) string {
	var out strings.Builder
	hue := panelHue(p, 3)
	muted := func(text string) string { return fmt.Sprintf("[%s]%s[-]", p.muted, tview.Escape(clean(text))) }
	title := "CPU CORES"
	if usage.cpus.model != "" {
		title += " · " + usage.cpus.model
	}
	fmt.Fprintf(&out, "[%s::b]%s[-::-]\n", hue, tview.Escape(clean(title)))
	detail := cpuInventoryDetail(usage)
	if usage.cpus.model != "" {
		detail = strings.Replace(detail, " · "+usage.cpus.model, "", 1)
	}
	if detail == "" {
		detail = "CPU inventory not read yet"
	}
	fmt.Fprintf(&out, "%s\n", muted(detail+" · every "+hostSampleInterval().String()))

	// The whole-machine line the cards show, so the per-CPU rows beneath
	// have their sum in view.
	host := "—"
	hostHue := p.muted
	if usage.cpuOK {
		host, hostHue = fmt.Sprintf("%.0f%%", usage.cpuPercent), pressureHue(p, int(usage.cpuPercent))
	}
	line := fmt.Sprintf("[%s]HOST[-] [%s::b]%s[-::-]", p.muted, hostHue, host)
	if usage.loadOK {
		line += muted(fmt.Sprintf(" · load %.2f %.2f %.2f", usage.load1, usage.load5, usage.load15))
	}
	if stall := pressureSignal(p, usage.pressure); stall != "" {
		line += fmt.Sprintf(" [%s]·[-] %s", p.muted, stall)
	}
	if len(hostHistory) >= 2 {
		line += fmt.Sprintf("   [%s]%s[-]  %s", hue, signalChart(trimHistory(hostHistory, 24), glyphs), muted(signalDelta(hostHistory)))
	}
	out.WriteString(line + "\n\n")

	order := "busiest first"
	if !sortBusy {
		order = "by number"
	}
	fmt.Fprintf(&out, "[%s::b]── PER CPU · %s · S sorts[-::-]\n", hue, order)
	count := max(len(usage.coreBusy), len(coreHistory), len(usage.clocks.mhz))
	if usage.cpus.logical > 0 && len(usage.coreBusy) == 0 && len(coreHistory) == 0 {
		count = usage.cpus.logical
	}
	if count == 0 {
		fmt.Fprintf(&out, "  %s\n", muted("no per-CPU reading · the kernel exposes none here"))
		return out.String() + footerCores(p)
	}
	if len(usage.coreBusy) == 0 {
		note := "per-CPU shares need two samples · collecting"
		if servicePlatform == "darwin" {
			note = "per-CPU utilisation is not read on macOS · counts and clocks only"
		}
		fmt.Fprintf(&out, "  %s\n", muted(note))
	}
	// Column budget: identity 18, busy meter 26, split 30, clock 9, trend 18,
	// process the rest. Trend then process are dropped on a narrow terminal.
	trendWidth, processWidth := 16, 0
	fixed := 18 + 26 + 30 + 9
	if width > 0 && width-fixed < trendWidth+4 {
		trendWidth = 0
	}
	if withProcesses {
		processWidth = max(0, width-fixed-trendWidth-4)
		if width == 0 {
			processWidth = 40
		}
	}
	header := fmt.Sprintf("  %-4s %-5s %-5s  %-16s %5s  %5s %5s %6s %5s  %8s", "CPU", "CORE", "SKT", "BUSY", "", "USER", "SYS", "IOWAIT", "STEAL", "CLOCK")
	if trendWidth > 0 {
		header += fmt.Sprintf("  %-*s", trendWidth, "TREND")
	}
	if processWidth > 8 {
		header += "  BUSIEST PROCESS"
	}
	fmt.Fprintf(&out, "[%s::b]%s[-::-]\n", p.accent, header)

	// The busiest process on a CPU is the one with the highest interval
	// share among those last scheduled there. A CPU whose processes all read
	// 0% has no busiest process; it is reported as idle with how many were
	// last placed on it, rather than promoting an arbitrary sleeper.
	busiest, placed := map[int]hostProcess{}, map[int]int{}
	for _, process := range processes {
		if process.LastCPU < 0 {
			continue
		}
		placed[process.LastCPU]++
		if process.CPU <= 0 {
			continue
		}
		if current, ok := busiest[process.LastCPU]; !ok || process.CPU > current.CPU {
			busiest[process.LastCPU] = process
		}
	}
	ids := make([]int, count)
	for i := range ids {
		ids[i] = i
	}
	share := func(cpu int) float64 {
		if cpu < len(usage.coreBusy) {
			return usage.coreBusy[cpu]
		}
		return -1
	}
	if sortBusy {
		sort.SliceStable(ids, func(a, b int) bool {
			if share(ids[a]) != share(ids[b]) {
				return share(ids[a]) > share(ids[b])
			}
			return ids[a] < ids[b]
		})
	}
	hot := 0
	for _, cpu := range ids {
		core, socket := usage.cpus.coreLabel(cpu)
		if core == "" {
			core, socket = "—", "—"
		}
		busy := share(cpu)
		meter, figure, figureHue := strings.Repeat("·", 16), "    —", p.muted
		if busy >= 0 {
			meter, figure, figureHue = gaugeMeter(busy, 100, 16, glyphs), fmt.Sprintf("%4.0f%%", busy), pressureHue(p, int(busy))
			if busy >= 70 {
				hot++
			}
		}
		split := fmt.Sprintf("%5s %5s %6s %5s", "—", "—", "—", "—")
		if cpu < len(usage.coreSplit) && usage.coreSplit[cpu].ok {
			s := usage.coreSplit[cpu]
			iowait, steal := fmt.Sprintf("%5.0f%%", s.iowait), fmt.Sprintf("%4.0f%%", s.steal)
			if s.iowait >= 10 {
				iowait = fmt.Sprintf("[%s]%5.0f%%[-]", p.warning, s.iowait)
			}
			if s.steal >= 5 {
				steal = fmt.Sprintf("[%s]%4.0f%%[-]", p.warning, s.steal)
			}
			split = fmt.Sprintf("%4.0f%% %4.0f%% %s %s", s.user, s.system, iowait, steal)
		}
		clock := "       —"
		if cpu < len(usage.clocks.mhz) && usage.clocks.mhz[cpu] > 0 {
			clock = fmt.Sprintf("%8s", formatClock(usage.clocks.mhz[cpu]))
		}
		fmt.Fprintf(&out, "  [%s::b]%-4d[-::-] [%s]%-5s %-5s[-]  [%s]%s[-] [%s::b]%s[-::-]  %s  [%s]%s[-]", hue, cpu, p.muted, core, socket, figureHue, meter, figureHue, figure, split, p.muted, clock)
		if trendWidth > 0 {
			trend := strings.Repeat(" ", trendWidth)
			if cpu < len(coreHistory) && len(coreHistory[cpu]) >= 2 {
				trend = fmt.Sprintf("%-*s", trendWidth, signalChart(trimHistory(coreHistory[cpu], trendWidth), glyphs))
			}
			fmt.Fprintf(&out, "  [%s]%s[-]", hue, trend)
		}
		if processWidth > 8 {
			if process, ok := busiest[cpu]; ok {
				label := fmt.Sprintf("%s · PID %d · %.1f%%", shortCommand(process.Command), process.PID, process.CPU)
				fmt.Fprintf(&out, "  %s", tview.Escape(clean(ellipsize(label, processWidth))))
			} else if n := placed[cpu]; n > 0 {
				fmt.Fprintf(&out, "  %s", muted(fmt.Sprintf("idle · %d processes last here", n)))
			} else {
				fmt.Fprintf(&out, "  %s", muted("—"))
			}
		}
		out.WriteByte('\n')
	}
	if len(usage.coreBusy) > 0 {
		summary := fmt.Sprintf("%d of %d CPUs above 70%% at this sample", hot, count)
		if clock := clockText(usage.clocks); clock != "" {
			summary += " · clocks " + clock
		}
		fmt.Fprintf(&out, "  %s\n", muted(summary))
	}
	out.WriteByte('\n')
	notes := []string{"BUSY is everything but idle and iowait", "IOWAIT is idle time spent waiting for a device", "STEAL is time the hypervisor ran another guest", "a process's ON CPU is where it was last scheduled, not an affinity"}
	if !withProcesses {
		notes = append(notes, "open Processes (4) then c to see the busiest process on each CPU")
	}
	fmt.Fprintf(&out, "%s\n", muted(strings.Join(notes, " · ")))
	return out.String() + footerCores(p)
}

func footerCores(p palette) string {
	return fmt.Sprintf("[%s]S sort · G graphics · Esc returns[-]\n", p.muted)
}
