package main

import (
	"bytes"
	"encoding/binary"
	"slices"

	kolobraid "github.com/ciranda-dev/kolo-braid"
)

// model is the board as a snapshot of the view holds it: columns in order, each with its cards in order.
type model struct {
	Title    string
	Names    map[oid]string // the name each member wrote for itself, as written
	Columns  []*modelColumn
	Sequence uint64 // the number of activity records
}

type modelColumn struct {
	ID oid
	column
	Cards []*modelCard
}

type modelCard struct {
	ID oid
	card
}

// byPosition orders siblings by position, then by ID: concurrent inserts into one gap get the same position, and the ID
// decides the same way on every peer.
func byPosition(posA, posB []byte, a, b oid) int {
	if c := bytes.Compare(posA, posB); c != 0 {
		return c
	}
	return bytes.Compare(a[:], b[:])
}

// readModel reads the whole board from a snapshot. A record that does not decode is skipped: apply wrote every record, so
// that does not happen, and a view must never stop a window from showing the rest.
func readModel(s *kolobraid.Snapshot) *model {
	m := &model{Names: map[oid]string{}}
	if v, err := s.Get([]byte(keyTitle)); err == nil {
		m.Title = string(v)
	}
	if v, err := s.Get([]byte(keySeq)); err == nil && len(v) == 8 {
		m.Sequence = binary.BigEndian.Uint64(v)
	}
	for k, v := range s.Scan([]byte(prefixName), prefixEnd(prefixName)) {
		if len(k) == len(prefixName)+idSize {
			m.Names[oid(k[len(prefixName):])] = string(v)
		}
	}
	cols := map[oid]*modelColumn{}
	for k, v := range s.Scan([]byte(prefixCol), prefixEnd(prefixCol)) {
		c, err := decodeColumn(v)
		if err != nil || len(k) != len(prefixCol)+idSize {
			continue
		}
		mc := &modelColumn{ID: oid(k[len(prefixCol):]), column: c}
		cols[mc.ID] = mc
		m.Columns = append(m.Columns, mc)
	}
	for k, v := range s.Scan([]byte(prefixCard), prefixEnd(prefixCard)) {
		c, err := decodeCard(v)
		if err != nil || len(k) != len(prefixCard)+idSize {
			continue
		}
		if col := cols[c.Col]; col != nil {
			col.Cards = append(col.Cards, &modelCard{ID: oid(k[len(prefixCard):]), card: c})
		}
	}
	slices.SortFunc(m.Columns, func(a, b *modelColumn) int { return byPosition(a.Pos, b.Pos, a.ID, b.ID) })
	for _, col := range m.Columns {
		slices.SortFunc(col.Cards, func(a, b *modelCard) int { return byPosition(a.Pos, b.Pos, a.ID, b.ID) })
	}
	return m
}

func (m *model) column(id oid) *modelColumn {
	for _, c := range m.Columns {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// find returns the card and its column.
func (m *model) find(id oid) (*modelColumn, *modelCard) {
	for _, col := range m.Columns {
		for _, c := range col.Cards {
			if c.ID == id {
				return col, c
			}
		}
	}
	return nil, nil
}

// sibling is an item of a list ordered by position: a column among the columns, a card among the cards of a column.
type sibling struct {
	ID  oid
	Pos []byte
}

func columnSiblings(m *model, except oid) []sibling {
	var out []sibling
	for _, c := range m.Columns {
		if c.ID != except {
			out = append(out, sibling{c.ID, c.Pos})
		}
	}
	return out
}

func cardSiblings(col *modelColumn, except oid) []sibling {
	var out []sibling
	if col == nil {
		return out
	}
	for _, c := range col.Cards {
		if c.ID != except {
			out = append(out, sibling{c.ID, c.Pos})
		}
	}
	return out
}

// slot is where an item goes: Pos is its position, and Fix, when set, is a sibling that must get a new position first
// because it shares its position with its neighbor before the slot, which no position fits between.
type slot struct {
	Pos []byte
	Fix *sibling
}

// plan finds the slot at index among siblings, already in order and without the item itself; an index past the ends is
// clamped. It returns ErrNoRoom when a position would pass maxPosBytes.
func plan(siblings []sibling, index int) (slot, error) {
	index = max(0, min(index, len(siblings)))
	var prev, next []byte
	if index > 0 {
		prev = siblings[index-1].Pos
	}
	if index < len(siblings) {
		next = siblings[index].Pos
	}
	if prev != nil && next != nil && bytes.Equal(prev, next) {
		// Two siblings with one position: the one after the slot moves up, to just before the first position above.
		var above []byte
		for _, s := range siblings[index:] {
			if !bytes.Equal(s.Pos, next) {
				above = s.Pos
				break
			}
		}
		moved := between(next, above)
		if moved == nil {
			return slot{}, ErrNoRoom
		}
		pos := between(prev, moved)
		if pos == nil {
			return slot{}, ErrNoRoom
		}
		return slot{Pos: pos, Fix: &sibling{siblings[index].ID, moved}}, nil
	}
	pos := between(prev, next)
	if pos == nil {
		return slot{}, ErrNoRoom
	}
	return slot{Pos: pos}, nil
}

// activityItem is a record of the activity list with its place in it.
type activityItem struct {
	Seq uint64
	activity
}

// readActivity reads up to limit records older than before (0: the newest), newest first, and whether older ones exist.
func readActivity(s *kolobraid.Snapshot, before uint64, limit int) (items []activityItem, more bool) {
	var total uint64
	if v, err := s.Get([]byte(keySeq)); err == nil && len(v) == 8 {
		total = binary.BigEndian.Uint64(v)
	}
	end := total
	if before != 0 && before < total {
		end = before
	}
	start := uint64(0)
	if end > uint64(limit) {
		start = end - uint64(limit)
	}
	from := binary.BigEndian.AppendUint64([]byte(prefixAct), start)
	to := binary.BigEndian.AppendUint64([]byte(prefixAct), end)
	for k, v := range s.Scan(from, to) {
		a, err := decodeActivity(v)
		if err != nil || len(k) != len(prefixAct)+8 {
			continue
		}
		items = append(items, activityItem{Seq: binary.BigEndian.Uint64(k[len(prefixAct):]), activity: a})
	}
	slices.Reverse(items)
	return items, start > 0
}

// activityTotal is the number of activity records in a snapshot.
func activityTotal(s *kolobraid.Snapshot) uint64 {
	if v, err := s.Get([]byte(keySeq)); err == nil && len(v) == 8 {
		return binary.BigEndian.Uint64(v)
	}
	return 0
}

// readActivityFrom reads the activity records from sequence number from on, oldest first.
func readActivityFrom(s *kolobraid.Snapshot, from uint64) (items []activityItem) {
	for k, v := range s.Scan(binary.BigEndian.AppendUint64([]byte(prefixAct), from), prefixEnd(prefixAct)) {
		a, err := decodeActivity(v)
		if err != nil || len(k) != len(prefixAct)+8 {
			continue
		}
		items = append(items, activityItem{Seq: binary.BigEndian.Uint64(k[len(prefixAct):]), activity: a})
	}
	return items
}
