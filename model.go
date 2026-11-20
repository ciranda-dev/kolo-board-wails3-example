package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	kolobraid "github.com/ciranda-dev/kolo-braid"
	kolocodec "github.com/ciranda-dev/kolo-codec"
)

// appTag is the application tag of the braid (GroupConfig.App, 1 to 64 bytes); opTag starts the body of every board
// entry, and a body of another tag is rejected.
const (
	appTag = "kolo-board/v1"
	opTag  = "kolo-board/op/v0"
)

// Sizes and bounds of the entries.
const (
	idSize      = 32                   // an entry ID (a column or a card) and a member key
	maxPosBytes = kolobraid.MaxViewKey // a position is bounded like a view key; one more byte per 255 cards added at the end of a column
	maxColor    = 6                    // the colors: 0 is none, then the six label colors
)

// colors are the label colors of a card, by number; 0 is no color.
var colors = [...]string{"", "red", "orange", "yellow", "green", "blue", "purple"}

// oid is the ID of a column or a card: the ID of the entry that created it. A member key has the same size.
type oid [idSize]byte

func (id oid) String() string { return hex.EncodeToString(id[:]) }

func parseOID(s string) (oid, bool) {
	var id oid
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != idSize {
		return id, false
	}
	copy(id[:], b)
	return id, true
}

// opKind says what a data entry of the board does.
type opKind uint64

// The operations. Their numbers are part of the format.
const (
	opProfile opKind = iota + 1
	opTitle
	opColAdd
	opColRename
	opColMove
	opColDel
	opCardAdd
	opCardEdit
	opCardMove
	opCardDel
	opLast = opCardDel
)

// Fields of opCardEdit, as a bit set.
const (
	fieldTitle = 1 << iota
	fieldDesc
	fieldAssignee
	fieldColor
	fieldsAll = fieldTitle | fieldDesc | fieldAssignee | fieldColor
)

// op is one decoded data entry. Which fields count depends on Kind; the others are zero.
type op struct {
	Kind        opKind
	Text        string // opProfile: the name; opTitle, opColAdd, opColRename, opCardAdd: the title; opCardEdit: the title if fieldTitle
	Desc        string // opCardAdd, opCardEdit (fieldDesc)
	ID          oid    // opColRename, opColMove, opColDel, opCardEdit, opCardMove, opCardDel
	Col         oid    // opCardAdd, opCardMove: the column
	Pos         []byte // opColAdd, opColMove, opCardAdd, opCardMove: the position among its siblings
	HasAssignee bool   // opCardAdd, opCardEdit (fieldAssignee)
	Assignee    oid
	Color       uint8  // opCardAdd, opCardEdit (fieldColor)
	Mask        uint64 // opCardEdit: the fields that change
}

var (
	errNotBoard  = errors.New("not a board entry")
	errMalformed = errors.New("malformed board entry")
)

// textBytes is the most bytes of a text of n characters.
func textBytes(n int) int { return n * utf8.UTFMax }

func checkText(s string, max int, required bool) error {
	if !utf8.ValidString(s) {
		return fmt.Errorf("%w: invalid UTF-8", errMalformed)
	}
	if utf8.RuneCountInString(s) > max {
		return fmt.Errorf("%w: text over %d characters", errMalformed, max)
	}
	if required && strings.TrimSpace(s) == "" {
		return fmt.Errorf("%w: empty text", errMalformed)
	}
	return nil
}

// checkPos accepts a position: 1 to maxPosBytes bytes that do not end in zero, so that each fraction has one spelling.
func checkPos(p []byte) error {
	if len(p) == 0 || len(p) > maxPosBytes || p[len(p)-1] == 0 {
		return fmt.Errorf("%w: position", errMalformed)
	}
	return nil
}

// validate checks the fields Kind uses; encode and decode both run it, so what is written is what is accepted.
func (o op) validate() error {
	switch o.Kind {
	case opProfile:
		return checkText(o.Text, maxNameRunes, true)
	case opTitle:
		return checkText(o.Text, maxBoardTitleRunes, true)
	case opColAdd:
		if err := checkText(o.Text, maxColumnTitleRunes, true); err != nil {
			return err
		}
		return checkPos(o.Pos)
	case opColRename:
		return checkText(o.Text, maxColumnTitleRunes, true)
	case opColMove:
		return checkPos(o.Pos)
	case opColDel, opCardDel:
		return nil
	case opCardAdd:
		if err := checkText(o.Text, maxCardTitleRunes, true); err != nil {
			return err
		}
		if err := checkText(o.Desc, maxCardDescRunes, false); err != nil {
			return err
		}
		if o.Color > maxColor {
			return fmt.Errorf("%w: color", errMalformed)
		}
		return checkPos(o.Pos)
	case opCardEdit:
		if o.Mask == 0 || o.Mask&^fieldsAll != 0 {
			return fmt.Errorf("%w: fields", errMalformed)
		}
		if o.Mask&fieldTitle != 0 {
			if err := checkText(o.Text, maxCardTitleRunes, true); err != nil {
				return err
			}
		}
		if o.Mask&fieldDesc != 0 {
			if err := checkText(o.Desc, maxCardDescRunes, false); err != nil {
				return err
			}
		}
		if o.Mask&fieldColor != 0 && o.Color > maxColor {
			return fmt.Errorf("%w: color", errMalformed)
		}
		return nil
	case opCardMove:
		return checkPos(o.Pos)
	}
	return fmt.Errorf("%w: unknown operation %d", errMalformed, o.Kind)
}

// encode returns the body of the entry: the tag, the operation and its fields, in kolo-codec.
func (o op) encode() ([]byte, error) {
	if err := o.validate(); err != nil {
		return nil, err
	}
	var e kolocodec.Encoder
	e.PutString(opTag)
	e.PutUvarint(uint64(o.Kind))
	switch o.Kind {
	case opProfile, opTitle:
		e.PutString(o.Text)
	case opColAdd:
		e.PutString(o.Text)
		e.PutBytes(o.Pos)
	case opColRename:
		e.PutFixed(o.ID[:])
		e.PutString(o.Text)
	case opColMove:
		e.PutFixed(o.ID[:])
		e.PutBytes(o.Pos)
	case opColDel, opCardDel:
		e.PutFixed(o.ID[:])
	case opCardAdd:
		e.PutFixed(o.Col[:])
		e.PutString(o.Text)
		e.PutString(o.Desc)
		putAssignee(&e, o.HasAssignee, o.Assignee)
		e.PutUvarint(uint64(o.Color))
		e.PutBytes(o.Pos)
	case opCardEdit:
		e.PutFixed(o.ID[:])
		e.PutUvarint(o.Mask)
		if o.Mask&fieldTitle != 0 {
			e.PutString(o.Text)
		}
		if o.Mask&fieldDesc != 0 {
			e.PutString(o.Desc)
		}
		if o.Mask&fieldAssignee != 0 {
			putAssignee(&e, o.HasAssignee, o.Assignee)
		}
		if o.Mask&fieldColor != 0 {
			e.PutUvarint(uint64(o.Color))
		}
	case opCardMove:
		e.PutFixed(o.ID[:])
		e.PutFixed(o.Col[:])
		e.PutBytes(o.Pos)
	}
	return e.Bytes(), nil
}

func putAssignee(e *kolocodec.Encoder, has bool, key oid) {
	e.PutBool(has)
	if has {
		e.PutFixed(key[:])
	}
}

// reader collects the first error of a sequence of reads, so a decoder reads like a list of fields.
type reader struct {
	d   *kolocodec.Decoder
	err error
}

func (r *reader) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

func (r *reader) str(max int) string {
	if r.err != nil {
		return ""
	}
	s, err := r.d.String(textBytes(max))
	r.fail(err)
	return s
}

func (r *reader) bytes(max int) []byte {
	if r.err != nil {
		return nil
	}
	b, err := r.d.Bytes(max)
	r.fail(err)
	return b
}

func (r *reader) id() (id oid) {
	if r.err != nil {
		return id
	}
	b, err := r.d.Fixed(idSize)
	r.fail(err)
	copy(id[:], b)
	return id
}

func (r *reader) uvarint(max uint64) uint64 {
	if r.err != nil {
		return 0
	}
	v, err := r.d.UvarintMax(max)
	r.fail(err)
	return v
}

func (r *reader) boolean() bool {
	if r.err != nil {
		return false
	}
	v, err := r.d.Bool()
	r.fail(err)
	return v
}

func (r *reader) assignee() (bool, oid) {
	if !r.boolean() {
		return false, oid{}
	}
	return true, r.id()
}

// decodeOp decodes the body of a data entry in its one canonical form. It never panics: a body that is not ours, is cut
// short, has bytes left, or breaks a bound returns an error.
func decodeOp(body []byte) (op, error) {
	r := &reader{d: kolocodec.NewDecoder(body)}
	if t, err := r.d.String(len(opTag)); err != nil || t != opTag {
		return op{}, errNotBoard
	}
	o := op{Kind: opKind(r.uvarint(uint64(opLast)))}
	if r.err == nil && o.Kind == 0 {
		r.fail(errMalformed)
	}
	switch o.Kind {
	case opProfile:
		o.Text = r.str(maxNameRunes)
	case opTitle:
		o.Text = r.str(maxBoardTitleRunes)
	case opColAdd:
		o.Text = r.str(maxColumnTitleRunes)
		o.Pos = r.bytes(maxPosBytes)
	case opColRename:
		o.ID = r.id()
		o.Text = r.str(maxColumnTitleRunes)
	case opColMove:
		o.ID = r.id()
		o.Pos = r.bytes(maxPosBytes)
	case opColDel, opCardDel:
		o.ID = r.id()
	case opCardAdd:
		o.Col = r.id()
		o.Text = r.str(maxCardTitleRunes)
		o.Desc = r.str(maxCardDescRunes)
		o.HasAssignee, o.Assignee = r.assignee()
		o.Color = uint8(r.uvarint(maxColor))
		o.Pos = r.bytes(maxPosBytes)
	case opCardEdit:
		o.ID = r.id()
		o.Mask = r.uvarint(fieldsAll)
		if o.Mask&fieldTitle != 0 {
			o.Text = r.str(maxCardTitleRunes)
		}
		if o.Mask&fieldDesc != 0 {
			o.Desc = r.str(maxCardDescRunes)
		}
		if o.Mask&fieldAssignee != 0 {
			o.HasAssignee, o.Assignee = r.assignee()
		}
		if o.Mask&fieldColor != 0 {
			o.Color = uint8(r.uvarint(maxColor))
		}
	case opCardMove:
		o.ID = r.id()
		o.Col = r.id()
		o.Pos = r.bytes(maxPosBytes)
	}
	if r.err == nil {
		r.fail(r.d.Done())
	}
	if r.err != nil {
		return op{}, fmt.Errorf("%w: %w", errMalformed, r.err)
	}
	if err := o.validate(); err != nil {
		return op{}, err
	}
	return o, nil
}

// The keys of the view. A prefix ends in "/" and the range of a prefix is [prefix, prefix with "0"), since "0" follows "/".
const (
	keyTitle   = "t"  // the board's title
	keySeq     = "s"  // the number of activity records, 8 bytes big endian
	keyMembers = "m"  // the keys of the members at the last membership entry, sorted, 32 bytes each
	prefixCol  = "c/" // c/<column ID>: a column
	prefixCard = "k/" // k/<card ID>: a card
	prefixName = "n/" // n/<member key>: the name that member wrote for itself
	prefixAct  = "a/" // a/<sequence number, 8 bytes big endian>: one activity record
)

func keyOf(prefix string, id []byte) []byte { return append([]byte(prefix), id...) }

// prefixEnd is the exclusive end of the range of a prefix that ends in "/".
func prefixEnd(prefix string) []byte { return []byte(prefix[:len(prefix)-1] + "0") }

// column is the stored record of a column.
type column struct {
	Title string
	Pos   []byte
}

func (c column) encode() []byte {
	var e kolocodec.Encoder
	e.PutString(c.Title)
	e.PutBytes(c.Pos)
	return e.Bytes()
}

func decodeColumn(b []byte) (column, error) {
	r := &reader{d: kolocodec.NewDecoder(b)}
	c := column{Title: r.str(maxColumnTitleRunes), Pos: r.bytes(maxPosBytes)}
	if r.err == nil {
		r.fail(r.d.Done())
	}
	return c, r.err
}

// card is the stored record of a card. Touched is the position of the entry that last changed it, so that a card whose
// settled and tentative records differ is a card with a change that is not settled yet.
type card struct {
	Col         oid
	Title, Desc string
	HasAssignee bool
	Assignee    oid
	Color       uint8
	Pos         []byte
	CreatedBy   oid
	LastBy      oid
	LastOp      opKind
	Touched     uint64
}

func (c card) encode() []byte {
	var e kolocodec.Encoder
	e.PutFixed(c.Col[:])
	e.PutString(c.Title)
	e.PutString(c.Desc)
	putAssignee(&e, c.HasAssignee, c.Assignee)
	e.PutUvarint(uint64(c.Color))
	e.PutBytes(c.Pos)
	e.PutFixed(c.CreatedBy[:])
	e.PutFixed(c.LastBy[:])
	e.PutUvarint(uint64(c.LastOp))
	e.PutUvarint(c.Touched)
	return e.Bytes()
}

func decodeCard(b []byte) (card, error) {
	r := &reader{d: kolocodec.NewDecoder(b)}
	var c card
	c.Col = r.id()
	c.Title = r.str(maxCardTitleRunes)
	c.Desc = r.str(maxCardDescRunes)
	c.HasAssignee, c.Assignee = r.assignee()
	c.Color = uint8(r.uvarint(maxColor))
	c.Pos = r.bytes(maxPosBytes)
	c.CreatedBy = r.id()
	c.LastBy = r.id()
	c.LastOp = opKind(r.uvarint(uint64(opLast)))
	c.Touched = r.uvarint(^uint64(0))
	if r.err == nil {
		r.fail(r.d.Done())
	}
	return c, r.err
}

// Activity kinds: what a record of the activity list says. They are not the operations: a member joining or leaving has
// no operation, and a rename of the board is one record.
type actKind uint64

const (
	actBoard actKind = iota + 1 // the board was created
	actTitle
	actName
	actJoined
	actRemoved
	actColAdd
	actColRename
	actColMove
	actColDel
	actCardAdd
	actCardEdit
	actCardMove
	actCardDel
	actLast = actCardDel
)

// activity is one record of the activity list: Writer did Kind to Subject (a card, a column or a member key); Text is
// the title at the time (or the new name), Text2 the column a card moved to, Detail the fields an edit changed.
type activity struct {
	Entry    oid
	Writer   oid
	Kind     actKind
	Subject  oid
	Text     string
	Text2    string
	Detail   uint64
	Position uint64
}

func (a activity) encode() []byte {
	var e kolocodec.Encoder
	e.PutFixed(a.Entry[:])
	e.PutFixed(a.Writer[:])
	e.PutUvarint(uint64(a.Kind))
	e.PutFixed(a.Subject[:])
	e.PutString(a.Text)
	e.PutString(a.Text2)
	e.PutUvarint(a.Detail)
	e.PutUvarint(a.Position)
	return e.Bytes()
}

func decodeActivity(b []byte) (activity, error) {
	r := &reader{d: kolocodec.NewDecoder(b)}
	var a activity
	a.Entry = r.id()
	a.Writer = r.id()
	a.Kind = actKind(r.uvarint(uint64(actLast)))
	a.Subject = r.id()
	a.Text = r.str(maxCardTitleRunes)
	a.Text2 = r.str(maxColumnTitleRunes)
	a.Detail = r.uvarint(^uint64(0))
	a.Position = r.uvarint(^uint64(0))
	if r.err == nil {
		r.fail(r.d.Done())
	}
	return a, r.err
}

// between returns a position strictly between a and b, which are positions or nil for no bound. A position is a fraction
// in base 256 with no trailing zero digit, and bytes.Compare orders positions as the fractions. A bound open at the low
// end steps down from b, one open at the high end steps up from a, so adding at the start or the end of a column grows a
// position by one byte per about 255 additions; a gap between two neighbors is halved. It returns nil when a >= b.
func between(a, b []byte) []byte { return betweenLimit(a, b, maxPosBytes) }

// betweenLimit is between with positions of at most limit bytes.
func betweenLimit(a, b []byte, limit int) []byte {
	if b != nil && bytes.Compare(a, b) >= 0 {
		return nil
	}
	var out []byte
	aNil, bNil := len(a) == 0, b == nil
	for i := 0; ; i++ {
		da, db := 0, 256 // a digit past the end of a is 0; b nil has no end
		if i < len(a) {
			da = int(a[i])
		}
		if b != nil {
			db = 0
			if i < len(b) {
				db = int(b[i])
			}
		}
		switch {
		case db-da > 1:
			switch {
			case i >= len(a) && b != nil:
				out = append(out, byte(db-1))
			case b == nil && i < len(a):
				out = append(out, byte(da+1))
			case b == nil && bNil && !aNil: // adding at the end: the smallest step, so a byte lasts 255 additions
				out = append(out, 1)
			case b == nil && aNil && !bNil: // adding at the start: the largest digit, for the same reason
				out = append(out, 255)
			case b == nil:
				out = append(out, 128)
			default:
				out = append(out, byte((da+db)/2))
			}
			if len(out) > limit {
				return nil
			}
			return out
		case db == da:
			out = append(out, byte(da))
		default: // db == da+1: take da, and the rest has no upper bound
			out = append(out, byte(da))
			b = nil
		}
		if len(out) >= limit {
			return nil
		}
	}
}
