//go:build linux

package dashboard

import "time"

// processCPURates replaces ps's lifetime-average pcpu with the share of one
// logical CPU each process used since the previous poll, where /proc offers a
// comparable earlier sample. See processCPUSampler.
func processCPURates(rows []hostProcess) {
	processCPU.apply(rows, time.Now())
}
