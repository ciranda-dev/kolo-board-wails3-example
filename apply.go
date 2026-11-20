package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"slices"

	kolobraid "github.com/ciranda-dev/kolo-braid"
)

// apply turns the braid's order into the board. It is deterministic and total: every data entry either takes effect or is
// rejected by Reject, the same on every peer, and nothing else it does can fail. The rules are in docs/how-it-works.md,
// "Rules of the board".
func apply(tx *kolobraid.Tx, e *kolobraid.Entry) error {
	switch e.Kind {
	case kolobraid.KindGenesis:
		if err := put(tx, []byte(keyMembers), memberKeys(tx)); err != nil {
			return err
		}
		return record(tx, e, actBoard, oid{}, "", "", 0)
	case kolobraid.KindAddMember, kolobraid.KindRemoveMember:
		return applyMembers(tx, e)
	case kolobraid.KindData:
		o, err := decodeOp(e.Body)
		if err != nil {
			return kolobraid.Reject(err.Error())
		}
		return applyOp(tx, e, o)
	}
	return nil
}

func reject(why string) error { return kolobraid.Reject(why) }

// put writes a pair; a write over the view's bounds rejects the entry, as the library does whatever apply returns.
func put(tx *kolobraid.Tx, key, value []byte) error {
	err := tx.Put(key, value)
	if errors.Is(err, kolobraid.ErrViewLimit) {
		return reject("over the view bounds")
	}
	return err
}

// get reads a key: found is false for a key that is not there. Any other error is a storage error and halts the view.
func get(tx *kolobraid.Tx, key []byte) (v []byte, found bool, err error) {
	v, err = tx.Get(key)
	if errors.Is(err, kolobraid.ErrNotFound) {
		return nil, false, nil
	}
	return v, err == nil, err
}

func getColumn(tx *kolobraid.Tx, id oid) (column, bool, error) {
	b, found, err := get(tx, keyOf(prefixCol, id[:]))
	if err != nil || !found {
		return column{}, false, err
	}
	c, err := decodeColumn(b)
	if err != nil {
		return column{}, false, reject("corrupt column") // our own records: unreachable, and the same everywhere if it is not
	}
	return c, true, nil
}

func getCard(tx *kolobraid.Tx, id oid) (card, bool, error) {
	b, found, err := get(tx, keyOf(prefixCard, id[:]))
	if err != nil || !found {
		return card{}, false, err
	}
	c, err := decodeCard(b)
	if err != nil {
		return card{}, false, reject("corrupt card")
	}
	return c, true, nil
}

// writerOID is the writer's key as an ID; a key of another size is not one the library produces.
func writerOID(e *kolobraid.Entry) (oid, bool) {
	var id oid
	if len(e.Writer) != idSize {
		return id, false
	}
	copy(id[:], e.Writer)
	return id, true
}

// record appends one activity record.
func record(tx *kolobraid.Tx, e *kolobraid.Entry, kind actKind, subject oid, text, text2 string, detail uint64) error {
	w, ok := writerOID(e)
	if !ok {
		return reject("writer key")
	}
	var seq uint64
	if b, found, err := get(tx, []byte(keySeq)); err != nil {
		return err
	} else if found && len(b) == 8 {
		seq = binary.BigEndian.Uint64(b)
	}
	a := activity{Entry: oid(e.ID), Writer: w, Kind: kind, Subject: subject, Text: text, Text2: text2, Detail: detail, Position: e.Position}
	if err := put(tx, binary.BigEndian.AppendUint64([]byte(prefixAct), seq), a.encode()); err != nil {
		return err
	}
	return put(tx, []byte(keySeq), binary.BigEndian.AppendUint64(nil, seq+1))
}

// memberKeys is the sorted keys of the roster at the entry's position, 32 bytes each.
func memberKeys(tx *kolobraid.Tx) []byte {
	var keys [][]byte
	for _, m := range tx.Members() {
		if len(m.Key) == idSize {
			keys = append(keys, m.Key)
		}
	}
	slices.SortFunc(keys, bytes.Compare)
	return bytes.Join(keys, nil)
}

// applyMembers records who joined and who was removed: the roster at this position against the roster at the last
// membership entry. The library has already applied the entry's effect to the roster.
func applyMembers(tx *kolobraid.Tx, e *kolobraid.Entry) error {
	prev, _, err := get(tx, []byte(keyMembers))
	if err != nil {
		return err
	}
	cur := memberKeys(tx)
	has := func(list, key []byte) bool {
		for i := 0; i+idSize <= len(list); i += idSize {
			if bytes.Equal(list[i:i+idSize], key) {
				return true
			}
		}
		return false
	}
	for i := 0; i+idSize <= len(cur); i += idSize {
		if key := cur[i : i+idSize]; !has(prev, key) {
			if err := record(tx, e, actJoined, oid(key), "", "", 0); err != nil {
				return err
			}
		}
	}
	for i := 0; i+idSize <= len(prev); i += idSize {
		if key := prev[i : i+idSize]; !has(cur, key) {
			if err := record(tx, e, actRemoved, oid(key), "", "", 0); err != nil {
				return err
			}
		}
	}
	return put(tx, []byte(keyMembers), cur)
}

// isAdmin reports whether the key is an active admin at the entry's position.
func isAdmin(tx *kolobraid.Tx, key []byte) bool {
	m, ok := tx.Member(key)
	return ok && m.Roles&kolobraid.RoleAdmin != 0
}

// assigneeOK: a card is assigned to an active member, as of the position of the entry.
func assigneeOK(tx *kolobraid.Tx, has bool, key oid) bool {
	if !has {
		return true
	}
	_, ok := tx.Member(key[:])
	return ok
}

func applyOp(tx *kolobraid.Tx, e *kolobraid.Entry, o op) error {
	w, ok := writerOID(e)
	if !ok {
		return reject("writer key")
	}
	switch o.Kind {
	case opProfile:
		return applyProfile(tx, e, o, w)
	case opTitle:
		return applyTitle(tx, e, o)
	case opColAdd:
		return applyColAdd(tx, e, o)
	case opColRename:
		return applyColRename(tx, e, o)
	case opColMove:
		return applyColMove(tx, e, o)
	case opColDel:
		return deleteColumn(tx, e, o.ID)
	case opCardAdd:
		return applyCardAdd(tx, e, o, w)
	case opCardEdit:
		return applyCardEdit(tx, e, o, w)
	case opCardMove:
		return applyCardMove(tx, e, o, w)
	case opCardDel:
		return applyCardDel(tx, e, o)
	}
	return reject("unknown operation")
}

func applyProfile(tx *kolobraid.Tx, e *kolobraid.Entry, o op, w oid) error {
	key := keyOf(prefixName, w[:])
	prev, found, err := get(tx, key)
	if err != nil {
		return err
	}
	if err := put(tx, key, []byte(o.Text)); err != nil {
		return err
	}
	if found && string(prev) != o.Text {
		return record(tx, e, actName, w, o.Text, "", 0)
	}
	return nil
}

func applyTitle(tx *kolobraid.Tx, e *kolobraid.Entry, o op) error {
	if !isAdmin(tx, e.Writer) {
		return reject("only an admin renames the board")
	}
	if err := put(tx, []byte(keyTitle), []byte(o.Text)); err != nil {
		return err
	}
	return record(tx, e, actTitle, oid{}, o.Text, "", 0)
}

func applyColAdd(tx *kolobraid.Tx, e *kolobraid.Entry, o op) error {
	id := oid(e.ID)
	if err := put(tx, keyOf(prefixCol, id[:]), column{Title: o.Text, Pos: o.Pos}.encode()); err != nil {
		return err
	}
	return record(tx, e, actColAdd, id, o.Text, "", 0)
}

func applyColRename(tx *kolobraid.Tx, e *kolobraid.Entry, o op) error {
	c, found, err := getColumn(tx, o.ID)
	if err != nil {
		return err
	}
	if !found {
		return reject("no such column")
	}
	c.Title = o.Text
	if err := put(tx, keyOf(prefixCol, o.ID[:]), c.encode()); err != nil {
		return err
	}
	return record(tx, e, actColRename, o.ID, o.Text, "", 0)
}

func applyColMove(tx *kolobraid.Tx, e *kolobraid.Entry, o op) error {
	c, found, err := getColumn(tx, o.ID)
	if err != nil {
		return err
	}
	if !found {
		return reject("no such column")
	}
	c.Pos = o.Pos
	if err := put(tx, keyOf(prefixCol, o.ID[:]), c.encode()); err != nil {
		return err
	}
	return record(tx, e, actColMove, o.ID, c.Title, "", 0)
}

func applyCardAdd(tx *kolobraid.Tx, e *kolobraid.Entry, o op, w oid) error {
	col, found, err := getColumn(tx, o.Col)
	if err != nil {
		return err
	}
	if !found {
		return reject("no such column")
	}
	if !assigneeOK(tx, o.HasAssignee, o.Assignee) {
		return reject("the assignee is not a member")
	}
	id := oid(e.ID)
	c := card{Col: o.Col, Title: o.Text, Desc: o.Desc, HasAssignee: o.HasAssignee, Assignee: o.Assignee, Color: o.Color, Pos: o.Pos,
		CreatedBy: w, LastBy: w, LastOp: opCardAdd, Touched: e.Position}
	if err := put(tx, keyOf(prefixCard, id[:]), c.encode()); err != nil {
		return err
	}
	return record(tx, e, actCardAdd, id, o.Text, col.Title, 0)
}

func applyCardEdit(tx *kolobraid.Tx, e *kolobraid.Entry, o op, w oid) error {
	c, found, err := getCard(tx, o.ID)
	if err != nil {
		return err
	}
	if !found {
		return reject("no such card")
	}
	if o.Mask&fieldAssignee != 0 && !assigneeOK(tx, o.HasAssignee, o.Assignee) {
		return reject("the assignee is not a member")
	}
	if o.Mask&fieldTitle != 0 {
		c.Title = o.Text
	}
	if o.Mask&fieldDesc != 0 {
		c.Desc = o.Desc
	}
	if o.Mask&fieldAssignee != 0 {
		c.HasAssignee, c.Assignee = o.HasAssignee, o.Assignee
	}
	if o.Mask&fieldColor != 0 {
		c.Color = o.Color
	}
	c.LastBy, c.LastOp, c.Touched = w, opCardEdit, e.Position
	if err := put(tx, keyOf(prefixCard, o.ID[:]), c.encode()); err != nil {
		return err
	}
	return record(tx, e, actCardEdit, o.ID, c.Title, "", o.Mask)
}

func applyCardMove(tx *kolobraid.Tx, e *kolobraid.Entry, o op, w oid) error {
	c, found, err := getCard(tx, o.ID)
	if err != nil {
		return err
	}
	if !found {
		return reject("no such card")
	}
	col, found, err := getColumn(tx, o.Col)
	if err != nil {
		return err
	}
	if !found {
		return reject("no such column")
	}
	c.Col, c.Pos, c.LastBy, c.LastOp, c.Touched = o.Col, o.Pos, w, opCardMove, e.Position
	if err := put(tx, keyOf(prefixCard, o.ID[:]), c.encode()); err != nil {
		return err
	}
	return record(tx, e, actCardMove, o.ID, c.Title, col.Title, 0)
}

func applyCardDel(tx *kolobraid.Tx, e *kolobraid.Entry, o op) error {
	c, found, err := getCard(tx, o.ID)
	if err != nil {
		return err
	}
	if !found {
		return reject("no such card")
	}
	if err := tx.Delete(keyOf(prefixCard, o.ID[:])); err != nil {
		return deleteErr(err)
	}
	return record(tx, e, actCardDel, o.ID, c.Title, "", 0)
}

func deleteErr(err error) error {
	if errors.Is(err, kolobraid.ErrViewLimit) {
		return reject("over the view bounds")
	}
	return err
}

// deleteColumn removes the column and the cards in it as of this position. A card added or moved into it later is
// rejected, because the column is gone.
func deleteColumn(tx *kolobraid.Tx, e *kolobraid.Entry, id oid) error {
	col, found, err := getColumn(tx, id)
	if err != nil {
		return err
	}
	if !found {
		return reject("no such column")
	}
	var doomed [][]byte // collected first: a Scan does not see the writes made while it runs
	for k, v := range tx.Scan([]byte(prefixCard), prefixEnd(prefixCard)) {
		c, err := decodeCard(v)
		if err != nil {
			return reject("corrupt card")
		}
		if c.Col == id {
			doomed = append(doomed, k)
		}
	}
	for _, k := range doomed {
		if err := tx.Delete(k); err != nil {
			return deleteErr(err)
		}
	}
	if err := tx.Delete(keyOf(prefixCol, id[:])); err != nil {
		return deleteErr(err)
	}
	return record(tx, e, actColDel, id, col.Title, "", uint64(len(doomed)))
}
