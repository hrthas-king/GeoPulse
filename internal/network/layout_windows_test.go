//go:build windows && (amd64 || arm64)

package network

import (
	"testing"
	"unsafe"
)

func TestMIBLayout(t *testing.T) {
	var row ifRow2
	if unsafe.Sizeof(row) != 1352 || unsafe.Offsetof(row.InOctets) != 1208 ||
		unsafe.Offsetof(row.OutOctets) != 1280 || unsafe.Offsetof(row.OperStatus) != 1156 {
		t.Fatalf("MIB_IF_ROW2 ABI 错误: size=%d in=%d out=%d oper=%d", unsafe.Sizeof(row),
			unsafe.Offsetof(row.InOctets), unsafe.Offsetof(row.OutOctets), unsafe.Offsetof(row.OperStatus))
	}
}
