package commandcapture

import (
	"strings"
	"testing"
)

func TestNetworkOutputOverflowCannotLoseHeaders(t *testing.T) {
	var b prefixBuffer
	b.Write([]byte("p42\x00"))
	b.Write([]byte(strings.Repeat("x", 4*1024*1024)))
	if !b.overflow || b.Len() != 4*1024*1024 || !strings.HasPrefix(b.String(), "p42\x00") {
		t.Fatal("bounded output lost record origin")
	}
}
