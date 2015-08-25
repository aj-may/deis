package handler

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/deis/deis/logger/syslog"
)

func TestGetProjectName(t *testing.T) {
	pname, err := getProjectName("junk test[junk] junk junk junk")
	if err != nil {
		t.Errorf("Failed to get project name: %s", err)
	}

	if pname != "test" {
		t.Errorf("Expected project name 'test', got '%s'", pname)
	}

}

func TestRingBufferHandler(t *testing.T) {
	h := RingBufferHandler(5, 1031)

	count := 5
	msg := "junk test[%d] Test %d"
	x := make([]string, count)
	for i := 1; i <= count; i++ {
		m := fmt.Sprintf(msg, i, i)
		h.Handle(&syslog.Message{m})
		x[i-1] = m
	}

	time.Sleep(10 * time.Millisecond)

    expect := strings.Join(x, "\n")
	data := getFromStorage("test", count)
	if data != expect {
		t.Errorf("Expected '%s', Got '%s'", expect, data)
	}
}
