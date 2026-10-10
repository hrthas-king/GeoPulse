package ipinfo

import (
	"strings"
	"testing"
)

func TestTraceValidation(t *testing.T) {
	for _, body := range []string{"ip=1.1.1.1\nloc=CN\n", "fl=abc\r\nip=2606:4700:4700::1111\r\nloc=US\r\n"} {
		if _, err := ParseTrace([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{"ip=hello\nloc=CN", "ip=10.0.0.1\nloc=CN", "ip=127.0.0.1\nloc=CN", "ip=1.1.1.1\nloc=XX", "ip=1.1.1.1\nloc=cn", "ip=1.1.1.1\nloc=USA", "ip=1.1.1.1\nloc=CN\nloc=US", strings.Repeat("a", MaxBody+1)} {
		if _, err := ParseTrace([]byte(body)); err == nil {
			t.Fatalf("accepted invalid body: %.80s", body)
		}
	}
}
