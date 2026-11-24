package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	kolobraid "github.com/ciranda-dev/kolo-braid"
	kolocodec "github.com/ciranda-dev/kolo-codec"
	invite "github.com/ciranda-dev/kolo-invite"
	koloswarm "github.com/ciranda-dev/kolo-swarm"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoadConfig(t *testing.T) {
	c, err := LoadConfig([]string{"-data", "/x", "-bootstrap", "127.0.0.1:4000", "-first", "127.0.0.1:4000"}, envOf(map[string]string{"KOLO_BOARD_FIND_TIMEOUT": "9s"}), os.Stderr)
	if err != nil || c.DataDir != "/x" || c.First != "127.0.0.1:4000" || c.FindTimeout != 9*time.Second || c.bootstrapFrom != "flag" {
		t.Fatalf("%+v %v", c, err)
	}
	if c, _ = LoadConfig(nil, envOf(map[string]string{"KOLO_BOOTSTRAP": "127.0.0.1:1", "KOLO_BOARD_DATA": "/y"}), os.Stderr); c.bootstrapFrom != "env" || c.Bootstrap != "127.0.0.1:1" || c.DataDir != "/y" {
		t.Errorf("env: %+v", c)
	}
	if c, _ = LoadConfig([]string{"-bootstrap", "127.0.0.1:2"}, envOf(map[string]string{"KOLO_BOOTSTRAP": "127.0.0.1:1"}), os.Stderr); c.Bootstrap != "127.0.0.1:2" || c.bootstrapFrom != "flag" {
		t.Errorf("a flag wins over its variable: %+v", c)
	}
	if _, err := LoadConfig([]string{"-nope"}, envOf(nil), &bytes.Buffer{}); err == nil {
		t.Error("an unknown flag")
	}
	if _, err := LoadConfig([]string{"-h"}, envOf(nil), &bytes.Buffer{}); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("-h: %v", err)
	}
	if _, err := LoadConfig(nil, envOf(map[string]string{"KOLO_BOARD_FIND_TIMEOUT": "soon"}), os.Stderr); !errors.Is(err, ErrConfig) {
		t.Errorf("bad duration: %v", err)
	}
}

func TestResolve(t *testing.T) {
	bad := []Config{
		{FindTimeout: -time.Second}, {JoinTimeout: -time.Second}, {NotchLinger: -time.Second}, {LookupInterval: time.Millisecond}, {LookupInterval: 2 * time.Hour}, {First: "10.0.0.1:4000"},
		{First: "127.0.0.1:0"}, {First: "[::1]:4000"}, {Listen: "[::1]:0"}, {Listen: "[::ffff:127.0.0.1]:0"}, {Bootstrap: "not a node"}, {Bootstrap: "[::1]:4000"},
	}
	for _, c := range bad {
		c.DataDir = t.TempDir()
		if _, err := c.resolve(); !errors.Is(err, ErrConfig) {
			t.Errorf("%+v resolved: %v", c, err)
		}
	}
	s, err := Config{DataDir: t.TempDir(), Bootstrap: "127.0.0.1:4000"}.resolve()
	if err != nil || s.Listen != localListen || s.nodesFrom != "flag" || s.FindTimeout != 0 || s.JoinTimeout != 0 || s.NotchLinger != 0 || s.LookupInterval != defaultLookupInterval {
		t.Fatalf("%+v %v", s, err)
	}
	if s, _ = (Config{DataDir: t.TempDir(), Bootstrap: "example.org:4000"}).resolve(); s.Listen != publicListen {
		t.Errorf("listen %q", s.Listen)
	}
	if s, _ = (Config{DataDir: t.TempDir(), Bootstrap: "127.0.0.1:5", bootstrapFrom: "env"}).resolve(); s.nodesFrom != "env" || len(s.nodes) != 1 {
		t.Errorf("nodes from the environment: %+v", s)
	}
	if s, _ = (Config{DataDir: t.TempDir(), First: "127.0.0.1:4000"}).resolve(); len(s.nodes) != 1 || s.Listen != localListen || s.nodesFrom != "first" || s.mode != modeOwn {
		t.Errorf("first node: %+v", s)
	}
	var ce *ConfigError
	if _, err := (Config{DataDir: t.TempDir(), First: "x"}).resolve(); !errors.As(err, &ce) || ce.Setting != "first" || !strings.Contains(ce.Error(), "kolo-board: -first") {
		t.Errorf("%v", err)
	}
}

// Every reason has a kind, an English sentence, and no leftover placeholder when filled in.
func TestReasons(t *testing.T) {
	kinds := map[string]bool{kindInvalidInvite: true, kindNotFound: true, kindRefused: true, kindExpired: true, kindBusy: true, kindNetwork: true,
		kindName: true, kindText: true, kindPermission: true, kindSync: true, kindLimit: true, kindConfig: true, kindEnded: true, kindCancelled: true, kindInternal: true}
	for id, r := range reasons {
		if !kinds[r.kind] || r.text == "" {
			t.Errorf("%s: %+v", id, r)
		}
		e := boardErr(id, errors.New("boom"), "D")
		if strings.Contains(e.Message, "{") || strings.Contains(e.Message, "kolo-board:") || strings.Contains(e.Message, "\u2014") || strings.Contains(e.Message, "  ") {
			t.Errorf("%s: %q", id, e.Message)
		}
		if strings.Contains(r.text, "{detail}") && e.Detail == "" || e.Reason != id || e.Kind != r.kind {
			t.Errorf("%s: %+v", id, e)
		}
		if !strings.HasSuffix(r.text, ".") {
			t.Errorf("%s: %q has no full stop", id, r.text)
		}
	}
}

func TestExplain(t *testing.T) {
	cases := []struct {
		err    error
		reason string
	}{
		{nil, ""},
		{ErrInvalidInvite, "invalid-invite"},
		{invite.ErrInvalidCode, "invalid-invite"},
		{invite.ErrDenied, "join-denied"},
		{invite.ErrExpired, "invite-expired"},
		{invite.ErrUsedUp, "invite-used-up"},
		{invite.ErrRevoked, "invite-revoked"},
		{invite.ErrInvalid, "invite-unknown"},
		{invite.ErrRateLimited, "join-rate-limited"},
		{invite.ErrBusy, "inviter-busy"},
		{invite.ErrProtocol, "bad-welcome"},
		{invite.ErrTooLarge, "bad-welcome"},
		{ErrBadWelcome, "bad-welcome"},
		{kolobraid.ErrMalformed, "bad-welcome"},
		{invite.ErrNotAnswered, "inviter-lost"},
		{invite.ErrUnreachable, "inviter-not-found"},
		{ErrNotFound, "inviter-not-found"},
		{context.Canceled, "cancelled"},
		{context.DeadlineExceeded, "inviter-not-found"},
		{ErrNotReady, "network-not-ready"},
		{ErrBusy, "board-exists"},
		{ErrNoBoard, "no-board"},
		{ErrClosed, "app-closed"},
		{koloswarm.ErrClosed, "app-closed"},
		{invite.ErrClosed, "app-closed"},
		{kolobraid.ErrClosed, "app-closed"},
		{ErrNoRoom, "reorder-no-room"},
		{ErrMainOnly, "main-window-only"},
		{ErrLastAdmin, "last-admin"},
		{kolobraid.ErrNotAdmin, "not-admin"},
		{kolobraid.ErrRemoved, "you-were-removed"},
		{kolobraid.ErrNotMember, "not-member-yet"},
		{kolobraid.ErrUnconfirmed, "strand-unconfirmed"},
		{errors.Join(kolobraid.ErrUnconfirmed, context.DeadlineExceeded), "strand-unconfirmed"},
		{kolobraid.ErrStrandLost, "strand-lost"},
		{kolobraid.ErrHalted, "board-halted"},
		{kolobraid.ErrDiverged, "board-diverged"},
		{kolobraid.ErrTooLarge, "entry-too-large"},
		{kolobraid.ErrActiveKey, "already-member"},
		{kolobraid.ErrKnownStrand, "already-member"},
		{errors.New("x"), "internal"},
	}
	for _, tc := range cases {
		got := explain(tc.err)
		switch {
		case tc.err == nil && got != nil:
			t.Errorf("nil: %+v", got)
		case tc.err != nil && (got == nil || got.Reason != tc.reason || !errors.Is(got, tc.err)):
			t.Errorf("%v: %+v", tc.err, got)
		}
	}
	e := boardErr("name-empty", ErrBadName, "")
	if explain(e) != e {
		t.Error("a BoardError is explained as itself")
	}
	if c := boardErr("data-dir", os.ErrPermission, ""); c.Cause != "permission denied" {
		t.Errorf("%+v", c)
	}
	if c := boardErr("data-dir", errors.New("kolobraid: view halted"), ""); c.Cause != "view halted" {
		t.Errorf("%+v", c)
	}
}

func TestNames(t *testing.T) {
	ok := map[string]string{
		"Ana": "Ana", "  Ana   Maria\t": "Ana Maria", "José": "José", "李雷": "李雷", "a\u200Bb": "ab", "x\u0000y": "xy",
		"tab\there": "tab here", "line\nbreak": "line break", "\u202Eevil": "evil", "a\u2028b": "a b", "a\u3164b": "ab",
		strings.Repeat("é", maxNameRunes): strings.Repeat("é", maxNameRunes),
	}
	for in, want := range ok {
		if got, err := cleanName(in); err != nil || got != want {
			t.Errorf("cleanName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "   ", "\u200B\u0000", "\u3164", strings.Repeat("a", maxNameRunes+1), "\xff\xfe"} {
		var be *BoardError
		if _, err := cleanName(in); !errors.As(err, &be) || !errors.Is(err, ErrBadName) {
			t.Errorf("cleanName(%q): %v", in, err)
		}
	}
	key := bytes.Repeat([]byte{0xab}, 32)
	if got := peerName("", key); got != "Guest abab" {
		t.Errorf("peerName empty: %q", got)
	}
	if got := peerName("\u202E\u3164", key); got != "Guest abab" {
		t.Errorf("peerName of nothing visible: %q", got)
	}
	if got := peerName(strings.Repeat("x", 2*maxNameRunes), key); utf8.RuneCountInString(got) != maxNameRunes {
		t.Errorf("peerName long: %d", len(got))
	}
	if got := peerName("Ana", nil); got != "Ana" {
		t.Errorf("peerName without a key: %q", got)
	}
}

func TestCleanText(t *testing.T) {
	for in, want := range map[string]string{
		"":                          "",
		"one":                       "one",
		"a\r\nb\rc":                 "a\nb\nc",
		"a\n\n\n\nb":                "a\n\nb",
		"\n\n a  b \n\n":            "a b",
		"x\u202Ey\u200B\n\u2066z":   "xy\nz",
		"line\u2028two\u2029three":  "line\ntwo\nthree",
		"\xff\xfeok":                "ok",
		"tab\there\n\t\n   \nafter": "tab here\n\nafter",
	} {
		if got := cleanText(in); got != want {
			t.Errorf("cleanText(%q) = %q, want %q", in, got, want)
		}
	}
	if got := cleanLine("a\nb\u00A0c"); got != "a b c" {
		t.Errorf("cleanLine: %q", got)
	}
	if got := truncate("héllo", 2); got != "hé" || truncate("ab", 5) != "ab" {
		t.Errorf("truncate: %q", got)
	}
}

func FuzzCleanLine(f *testing.F) {
	for _, s := range []string{"Ana", "", "\u202E", strings.Repeat("é", 60), "a\x00b", "\xff", "a\nb", "\u3164"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		l := cleanLine(s)
		if !utf8.ValidString(l) || l != cleanLine(l) || strings.TrimSpace(l) != l || !shown(l) || strings.ContainsAny(l, "\n\r\t") {
			t.Fatalf("cleanLine(%q) = %q", s, l)
		}
		x := cleanText(s)
		if !utf8.ValidString(x) || x != cleanText(x) || !shown(x) || strings.Contains(x, "\n\n\n") || strings.HasPrefix(x, "\n") || strings.HasSuffix(x, "\n") {
			t.Fatalf("cleanText(%q) = %q", s, x)
		}
		if name, err := cleanName(s); err == nil && (name == "" || name != cleanLine(name) || utf8.RuneCountInString(name) > maxNameRunes) {
			t.Fatalf("cleanName(%q) = %q", s, name)
		}
		if got := peerName(s, make([]byte, 32)); got == "" || !shown(got) || utf8.RuneCountInString(got) > maxNameRunes+len("Guest 0000") {
			t.Fatalf("peerName(%q) = %q", s, got)
		}
	})
}

func TestJoinData(t *testing.T) {
	b := joinData("Ana")
	if name, err := parseJoinData(b); err != nil || name != "Ana" {
		t.Fatalf("%q %v", name, err)
	}
	long := joinData(strings.Repeat("n", 2*maxNameRunes))
	if name, err := parseJoinData(long); err != nil || utf8.RuneCountInString(name) != maxNameRunes {
		t.Errorf("a long name is cut to %d characters: %d %v", maxNameRunes, utf8.RuneCountInString(name), err)
	}
	for _, bad := range [][]byte{nil, {}, {0}, []byte("Ana"), append(bytes.Clone(b), 1), b[:len(b)-1], func() []byte {
		var e kolocodec.Encoder
		e.PutString("kolo-board/join/v1")
		e.PutString("Ana")
		return e.Bytes()
	}(), func() []byte {
		var e kolocodec.Encoder
		e.PutString(joinTag)
		e.PutBytes([]byte{0xff})
		return e.Bytes()
	}(), func() []byte {
		var e kolocodec.Encoder
		e.PutString(joinTag)
		e.PutBytes(bytes.Repeat([]byte("n"), textBytes(maxNameRunes)+1))
		return e.Bytes()
	}()} {
		if _, err := parseJoinData(bad); err == nil {
			t.Errorf("%x parsed", bad)
		}
	}
}

func FuzzParseJoinData(f *testing.F) {
	f.Add(joinData("Ana"))
	f.Add([]byte{})
	f.Add([]byte("kolo-board/join/v0"))
	f.Fuzz(func(t *testing.T, b []byte) {
		name, err := parseJoinData(b)
		if err == nil && (!utf8.ValidString(name) || utf8.RuneCountInString(name) > maxNameRunes) {
			t.Fatalf("%q", name)
		}
		if err == nil && !bytes.Equal(joinData(name), b) {
			t.Fatalf("%x parses and does not encode back", b)
		}
	})
}

// What decodes encodes back to the same bytes, so one operation has one spelling; nothing panics.
func FuzzDecodeOp(f *testing.F) {
	var id oid
	id[0] = 7
	for _, o := range []op{
		{Kind: opProfile, Text: "Ana"}, {Kind: opTitle, Text: "Launch"}, {Kind: opColAdd, Text: "To do", Pos: pos(0x80)},
		{Kind: opColRename, ID: id, Text: "x"}, {Kind: opColMove, ID: id, Pos: pos(1)}, {Kind: opColDel, ID: id},
		{Kind: opCardAdd, Col: id, Text: "t", Desc: "d", HasAssignee: true, Assignee: id, Color: 3, Pos: pos(5)},
		{Kind: opCardEdit, ID: id, Mask: fieldsAll, Text: "t", Desc: "d", HasAssignee: true, Assignee: id, Color: 6},
		{Kind: opCardEdit, ID: id, Mask: fieldColor, Color: 1}, {Kind: opCardMove, ID: id, Col: id, Pos: pos(9)}, {Kind: opCardDel, ID: id},
	} {
		body, err := o.encode()
		if err != nil {
			f.Fatal(err)
		}
		f.Add(body)
	}
	f.Add([]byte{})
	f.Add([]byte(opTag))
	f.Fuzz(func(t *testing.T, body []byte) {
		o, err := decodeOp(body)
		if err != nil {
			return
		}
		again, err := o.encode()
		if err != nil || !bytes.Equal(again, body) {
			t.Fatalf("%x decodes to %+v and encodes to %x, %v", body, o, again, err)
		}
		if o.validate() != nil {
			t.Fatalf("%+v decodes and does not validate", o)
		}
	})
}

// The records of the view and the activity list decode without panicking, and what decodes encodes back to the same bytes.
func FuzzDecodeRecords(f *testing.F) {
	f.Add(column{Title: "To do", Pos: pos(1)}.encode())
	f.Add(card{Title: "t", Desc: "d", Pos: pos(1), LastOp: opCardAdd}.encode())
	f.Add(activity{Kind: actCardAdd, Text: "t", Text2: "c"}.encode())
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		if c, err := decodeColumn(b); err == nil && !bytes.Equal(c.encode(), b) {
			t.Fatalf("column %x", b)
		}
		if c, err := decodeCard(b); err == nil && !bytes.Equal(c.encode(), b) {
			t.Fatalf("card %x", b)
		}
		if a, err := decodeActivity(b); err == nil && !bytes.Equal(a.encode(), b) {
			t.Fatalf("activity %x", b)
		}
	})
}

// between returns a position strictly between its bounds whatever they are, or nil when there is none.
func FuzzBetween(f *testing.F) {
	f.Add([]byte{1}, []byte{2}, true, true)
	f.Add([]byte(nil), []byte{1}, false, true)
	f.Add([]byte{255, 255}, []byte(nil), true, false)
	f.Fuzz(func(t *testing.T, a, b []byte, hasA, hasB bool) {
		if !hasA {
			a = nil
		} else if checkPos(a) != nil {
			return
		}
		if !hasB {
			b = nil
		} else if checkPos(b) != nil {
			return
		}
		got := between(a, b)
		switch {
		case got == nil:
		case checkPos(got) != nil || bytes.Compare(got, a) <= 0 || b != nil && bytes.Compare(got, b) >= 0:
			t.Fatalf("between(%x, %x) = %x", a, b, got)
		}
		if b != nil && bytes.Compare(a, b) < 0 && got == nil && len(a) < maxPosBytes-1 && len(b) < maxPosBytes-1 {
			t.Fatalf("no position between %x and %x", a, b)
		}
	})
}

func TestSettingsAndFiles(t *testing.T) {
	dir := t.TempDir()
	if s, err := loadSaved(dir); err != nil || s != (savedSettings{}) || !s.showInNotch() || !s.notchPreview() {
		t.Fatalf("no file: %+v %v", s, err)
	}
	off := false
	want := savedSettings{ShowInNotch: &off, DisplayName: "Ana"}
	if err := saveSettings(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadSaved(dir)
	if err != nil || got.showInNotch() || !got.notchPreview() || got.DisplayName != "Ana" {
		t.Fatalf("%+v %v", got, err)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(filepath.Join(dir, settingsFile)); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("settings file mode %v %v", info.Mode(), err)
		}
	}
	// a name that is not clean is cleaned again, and one with nothing visible is dropped
	os.WriteFile(filepath.Join(dir, settingsFile), []byte(`{"displayName":"\u202EAna\u200B"}`), 0o600)
	if s, _ := loadSaved(dir); s.DisplayName != "Ana" {
		t.Errorf("%q", s.DisplayName)
	}
	os.WriteFile(filepath.Join(dir, settingsFile), []byte(`{"displayName":"\u200B"}`), 0o600)
	if s, _ := loadSaved(dir); s.DisplayName != "" {
		t.Errorf("%q", s.DisplayName)
	}
	// a file that is not ours is the zero value: not JSON, too large, not a regular file
	os.WriteFile(filepath.Join(dir, settingsFile), []byte("{"), 0o600)
	if s, err := loadSaved(dir); err != nil || s != (savedSettings{}) {
		t.Errorf("garbage: %+v %v", s, err)
	}
	os.WriteFile(filepath.Join(dir, settingsFile), bytes.Repeat([]byte(" "), maxSmallFile+1), 0o600)
	if s, err := loadSaved(dir); err != nil || s != (savedSettings{}) {
		t.Errorf("too large: %+v %v", s, err)
	}
	os.Remove(filepath.Join(dir, settingsFile))
	os.Symlink(filepath.Join(dir, "elsewhere"), filepath.Join(dir, settingsFile))
	if s, err := loadSaved(dir); err != nil || s != (savedSettings{}) {
		t.Errorf("a symlink: %+v %v", s, err)
	}

	// the marker
	if _, ok, err := readMarker(dir); ok || err != nil {
		t.Fatalf("no marker: %v %v", ok, err)
	}
	if err := writeMarker(dir, marker{Version: 1, Group: "ab", Member: true}); err != nil {
		t.Fatal(err)
	}
	if m, ok, err := readMarker(dir); !ok || err != nil || m.Group != "ab" || !m.Member {
		t.Errorf("marker %+v %v %v", m, ok, err)
	}
	os.WriteFile(filepath.Join(dir, markerFile), []byte(`{"v":2}`), 0o600)
	if _, ok, _ := readMarker(dir); ok {
		t.Error("a marker of another version")
	}
	// atomic writes leave nothing behind
	if es, _ := os.ReadDir(dir); func() bool {
		for _, e := range es {
			if strings.HasPrefix(e.Name(), ".tmp-") {
				return true
			}
		}
		return false
	}() {
		t.Error("a temporary file was left")
	}
}

func TestInviteMeta(t *testing.T) {
	dir := t.TempDir()
	m, err := loadInviteMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	var a, b invite.ID
	a[0], b[0] = 1, 2
	if !m.get(a) {
		t.Error("an invite the file does not know must need approval")
	}
	if err := m.set(a, false); err != nil {
		t.Fatal(err)
	}
	if err := m.set(b, true); err != nil {
		t.Fatal(err)
	}
	m2, _ := loadInviteMeta(dir)
	if m2.get(a) || !m2.get(b) {
		t.Errorf("not kept: %v %v", m2.get(a), m2.get(b))
	}
	m2.keep([]invite.Info{{ID: b}})
	m3, _ := loadInviteMeta(dir)
	if !m3.get(a) || !m3.get(b) || len(m3.approval) != 1 {
		t.Errorf("an invite that is gone is forgotten, so it needs approval again: %+v", m3.approval)
	}
	os.WriteFile(filepath.Join(dir, invitesMeta), []byte("{"), 0o600)
	if m4, err := loadInviteMeta(dir); err != nil || len(m4.approval) != 0 {
		t.Errorf("garbage: %v %v", m4, err)
	}
}

func TestDataDirIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("modes do not apply")
	}
	dir := filepath.Join(t.TempDir(), "data")
	os.MkdirAll(dir, 0o755)
	os.Chmod(dir, 0o755)
	s := NewBoardService(Config{DataDir: dir, Bootstrap: "127.0.0.1:9", Listen: "127.0.0.1:0"}, func(string, any) {})
	_, err := s.Start()
	var be *BoardError
	if !errors.As(err, &be) || be.Reason != "data-dir-open" {
		t.Fatalf("a data directory open to others: %v", err)
	}
	if err := prepareDataDir(filepath.Join(t.TempDir(), "new")); err != nil {
		t.Fatal(err)
	}
	// a bad setting is a config error, with the setting's name
	s = NewBoardService(Config{DataDir: t.TempDir(), Bootstrap: "nonsense"}, func(string, any) {})
	if _, err := s.Start(); !errors.As(err, &be) || be.Reason != "config-setting" || be.Detail != "bootstrap" {
		t.Errorf("a bad setting: %v", err)
	}
}

func TestIdentityIsKept(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := prepareDataDir(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "id")
	a, err := loadIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := loadIdentity(path)
	if err != nil || a.PublicKeyString() != b.PublicKeyString() {
		t.Fatalf("a second load makes another identity: %v", err)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm()&0o077 != 0 {
			t.Errorf("identity file mode %v %v", info.Mode(), err)
		}
	}
}

func TestTrayUsable(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "bus")
	up := func(p string) error {
		if p == sock || p == "@abstract" {
			return nil
		}
		return os.ErrNotExist
	}
	for _, c := range []struct {
		goos string
		env  map[string]string
		want bool
	}{
		{"darwin", nil, true},
		{"windows", nil, true},
		{"linux", nil, false},
		{"linux", map[string]string{"DBUS_SESSION_BUS_ADDRESS": "unix:path=/nowhere"}, false},
		{"linux", map[string]string{"DBUS_SESSION_BUS_ADDRESS": "unix:path=" + sock + ",guid=1"}, true},
		{"linux", map[string]string{"DBUS_SESSION_BUS_ADDRESS": "tcp:host=x;unix:abstract=abstract"}, true},
		{"linux", map[string]string{"XDG_RUNTIME_DIR": filepath.Dir(sock)}, true},
	} {
		if got := trayUsable(c.goos, envOf(c.env), up); got != c.want {
			t.Errorf("%s %v: %v, want %v", c.goos, c.env, got, c.want)
		}
	}
}

func TestParseOID(t *testing.T) {
	var id oid
	id[0], id[31] = 0xab, 0xcd
	if got, ok := parseOID(id.String()); !ok || got != id {
		t.Errorf("round trip: %v %v", got, ok)
	}
	for _, bad := range []string{"", "ab", strings.Repeat("zz", 32), strings.Repeat("ab", 33), strings.Repeat("a", 63)} {
		if _, ok := parseOID(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
	if _, err := hex.DecodeString(id.String()); err != nil || len(id.String()) != 64 {
		t.Error("an ID is 64 hex digits")
	}
}

func TestColors(t *testing.T) {
	for i, name := range colors {
		if n, err := colorNumber(name); err != nil || int(n) != i {
			t.Errorf("%q: %d %v", name, n, err)
		}
	}
	if len(colors) != maxColor+1 {
		t.Errorf("%d colors, maxColor %d", len(colors), maxColor)
	}
	if _, err := colorNumber("pink"); explain(err).Reason != "bad-color" {
		t.Errorf("%v", err)
	}
}

func TestSwarmConfig(t *testing.T) {
	set, err := Config{DataDir: t.TempDir(), Bootstrap: "127.0.0.1:4000", LookupInterval: 7 * time.Second}.resolve()
	if err != nil {
		t.Fatal(err)
	}
	c := swarmConfig(set)
	if c.LookupInterval != 7*time.Second || len(c.BootstrapNodes) != 1 || c.MaxTopics != 0 || c.RetryMax != 7*time.Second {
		t.Errorf("%+v", c)
	}
	if c.DHT == nil || c.DHT.Secure == nil || c.DHT.Secure.Transport == nil || c.DHT.Secure.Transport.MaxConnsPerIP != testMaxConnsPerIP {
		t.Errorf("the transport limit is not the test seam's: %+v", c.DHT)
	}
	// Without the seam the app keeps kolo-transport's default of 4 connections per source address, which bounds what a
	// stranger's address can hold before it is a member.
	testMaxConnsPerIP = 0
	t.Cleanup(func() { testMaxConnsPerIP = testMaxConns })
	if n := swarmConfig(set).DHT.Secure.Transport.MaxConnsPerIP; n != 0 {
		t.Errorf("MaxConnsPerIP %d, want 0 (the library default)", n)
	}
}
