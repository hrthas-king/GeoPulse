//go:build windows && (amd64 || arm64)

package network

import (
	"encoding/binary"
	"testing"
	"unsafe"
)

func TestProcessEntryLayout(t *testing.T) {
	var p processEntry
	if unsafe.Sizeof(p) != 568 || unsafe.Offsetof(p.Name) != 44 {
		t.Fatal("PROCESSENTRY32W ABI changed")
	}
}

func TestProxyEndpointSwitchFingerprint(t *testing.T) {
	makeTable := func(remote byte, localPort uint32) []byte {
		data := make([]byte, 28)
		binary.LittleEndian.PutUint32(data, 1)
		row := data[4:]
		binary.LittleEndian.PutUint32(row, 5)
		binary.LittleEndian.PutUint32(row[8:], localPort)
		row[12] = remote
		binary.LittleEndian.PutUint32(row[16:], 443)
		binary.LittleEndian.PutUint32(row[20:], 42)
		return data
	}
	pids := map[uint32]bool{42: true}
	first := socketKeys(makeTable(1, 50000), false, false, pids)
	more := socketKeys(makeTable(1, 50001), false, false, pids)
	if egressHash("tun", first) != egressHash("tun", append(first, more...)) {
		t.Fatal("same-node extra connection must not trigger lookup")
	}
	switched := socketKeys(makeTable(8, 50002), false, false, pids)
	if egressHash("tun", first) == egressHash("tun", switched) {
		t.Fatal("node endpoint switch was not detected")
	}
	if len(socketKeys(makeTable(1, 50000), false, false, map[uint32]bool{7: true})) != 0 {
		t.Fatal("non-proxy process must be ignored")
	}
}
