//go:build !linux

package dashboard

// processCPURates is a no-op without /proc: macOS keeps ps pcpu, and
// hostProcess.RateCPU stays false.
func processCPURates([]hostProcess) {}
