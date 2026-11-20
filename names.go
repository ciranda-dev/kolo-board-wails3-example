package main

import (
	"encoding/binary"
	"encoding/hex"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	kolobraid "github.com/ciranda-dev/kolo-braid"
)

// Text bounds. Every one is an expression over a kolo-braid constant, so none is the app's own number (limits_test.go
// recomputes each from the constant). A name or a title is bounded like a view key: kolo-braid's MaxViewKey bytes, which is
// MaxViewKey / UTFMax characters. A card is one view value, at most MaxViewValue bytes: the description gets what is left
// after the title, the position and the fixed fields. The window clips long text; Go enforces these.
const (
	maxShortBytes       = kolobraid.MaxViewKey
	maxNameRunes        = maxShortBytes / utf8.UTFMax
	maxBoardTitleRunes  = maxShortBytes / utf8.UTFMax
	maxColumnTitleRunes = maxShortBytes / utf8.UTFMax
	maxCardTitleRunes   = maxShortBytes / utf8.UTFMax
	// cardFixedBytes covers the card record's fields besides title, description and position: four IDs, the assignee flag, the
	// color, the last operation, and six varints (the touched position and the lengths).
	cardFixedBytes   = 4*idSize + 2 + 6*binary.MaxVarintLen64
	maxCardDescRunes = (kolobraid.MaxViewValue - maxShortBytes - maxPosBytes - cardFixedBytes) / utf8.UTFMax
)

// blankLike are letters that draw nothing: a name made of them would look empty.
func blankLike(r rune) bool {
	switch r {
	case 0x115F, 0x1160, 0x3164, 0xFFA0, 0x2800:
		return true
	}
	return false
}

// hidden reports a character that is not text to show: control and format characters (bidi marks, zero-width joiners),
// private use, and letters that draw nothing.
func hidden(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Co, r) || r == utf8.RuneError || blankLike(r)
}

// cleanLine makes one line of display text of what another person (or this one) wrote: invalid UTF-8 and hidden
// characters are dropped, any run of blanks, line breaks included, becomes one space, the ends are trimmed.
func cleanLine(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	var b strings.Builder
	space := false
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			space = true
		case hidden(r):
		default:
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		}
	}
	return b.String()
}

// cleanText makes display text of several lines: like cleanLine for each line, line breaks kept, and at most one blank
// line in a row.
func cleanText(s string) string {
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\u2028", "\n", "\u2029", "\n", "\u0085", "\n").Replace(s)
	var out []string
	blank := false
	for _, line := range strings.Split(s, "\n") {
		line = cleanLine(line)
		if line == "" {
			blank = true
			continue
		}
		if blank && len(out) > 0 {
			out = append(out, "")
		}
		blank = false
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// truncate cuts s to at most n characters.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// cleanName checks a display name that a person typed: cleaned, then 1 to maxNameRunes characters.
func cleanName(s string) (string, error) {
	return checkLine(s, maxNameRunes, "name", ErrBadName)
}

// checkLine cleans a line a person typed and checks it is 1 to max characters; reason names its field: reason+"-empty"
// and reason+"-too-long" are its error reasons.
func checkLine(s string, max int, reason string, err error) (string, error) {
	clean := cleanLine(s)
	switch {
	case clean == "":
		return "", boardErr(reason+"-empty", err, "")
	case utf8.RuneCountInString(clean) > max:
		return "", boardErr(reason+"-too-long", err, strconv.Itoa(max))
	}
	return clean, nil
}

// peerName makes a display name of what another member wrote: the same cleaning, cut at maxNameRunes, and a short form
// of its key when nothing is left. A member's name is never trusted to be well formed.
func peerName(s string, key []byte) string {
	name := truncate(cleanLine(s), maxNameRunes)
	if name == "" {
		return "Guest " + keyTag(key)
	}
	return name
}

// keyTag is the first two bytes of a key in hex: enough to tell two members of one name apart.
func keyTag(key []byte) string { return hex.EncodeToString(key[:min(len(key), 2)]) }
