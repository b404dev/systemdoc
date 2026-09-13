package remote

import (
	"debug/macho"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestUploadMachORequiresMatchingExecutable(t *testing.T) {
	for _, arch := range []struct {
		cpu      macho.Cpu
		platform string
	}{{macho.CpuArm64, "Darwin arm64"}, {macho.CpuAmd64, "Darwin x86_64"}} {
		path := filepath.Join(t.TempDir(), "binary")
		header := []uint32{macho.Magic64, uint32(arch.cpu), 0, uint32(macho.TypeExec), 0, 0, 0, 0}
		data := make([]byte, 32)
		for i, v := range header {
			binary.LittleEndian.PutUint32(data[i*4:], v)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if err := checkBinary(path, arch.platform); arch.cpu == macho.CpuArm64 && err != nil {
			t.Fatal(err)
		} else if arch.cpu == macho.CpuAmd64 && err == nil {
			t.Fatal("Intel Mac upload accepted")
		}
		other := "Darwin arm64"
		if arch.platform == other {
			other = "Darwin x86_64"
		}
		for _, platform := range []string{other, "Linux x86_64", "Darwin unknown"} {
			if err := checkBinary(path, platform); err == nil {
				t.Fatal("mismatched platform accepted", platform)
			}
		}
		binary.LittleEndian.PutUint32(data[12:], uint32(macho.TypeObj))
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if err := checkBinary(path, arch.platform); err == nil {
			t.Fatal("non-executable Mach-O accepted")
		}
	}
}
