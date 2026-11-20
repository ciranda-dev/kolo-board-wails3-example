package main

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"testing"
)

// The window reads people, columns, requests and cards as lists: a null in any payload a joiner gets (also in the moment
// its braid is attached and the roster has not arrived) makes the page throw and go blank.
func TestJoinerPayloadsHaveNoNullLists(t *testing.T) {
	ps := network(t, "Ana", "Bia")
	a, b := ps[0], ps[1]
	if _, err := a.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	invited(t, a, b, InviteOptions{TTLSeconds: 3600})
	now, _ := json.Marshal(b.Board())
	for _, e := range append(b.rec.events(), "Board() "+string(now)) {
		if !strings.HasPrefix(e, "board:state ") && !strings.HasPrefix(e, "Board() ") {
			continue
		}
		for _, field := range []string{`"people":null`, `"columns":null`, `"requests":null`, `"cards":null`} {
			if strings.Contains(e, field) {
				t.Errorf("%s in %.200s", field, e)
			}
		}
	}
}

// A window's error reaches the log as one bounded entry, cleaned of control characters.
func TestReportError(t *testing.T) {
	var buf strings.Builder
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	s := NewBoardService(Config{}, func(string, any) {})
	s.ReportError("TypeError: x is null\n    at App\x1b[31m")
	if got := buf.String(); !strings.Contains(got, "frontend error: TypeError: x is null\nat App") || strings.Contains(got, "\x1b") {
		t.Errorf("%q", got)
	}
	buf.Reset()
	s.ReportError(strings.Repeat("e", maxCardTitleRunes+100))
	if n := strings.Count(buf.String(), "e") - strings.Count("frontend error", "e"); n != maxCardTitleRunes {
		t.Errorf("%d characters of the report were logged", n)
	}
}
