package main

// contentKinds are the activity that counts as a change by another member: what happens to the board's columns and cards
// and its title. A member joining or renaming themselves is not a change of the board.
var contentKinds = map[actKind]bool{
	actTitle: true, actColAdd: true, actColRename: true, actColMove: true, actColDel: true,
	actCardAdd: true, actCardEdit: true, actCardMove: true, actCardDel: true,
}

// collectChanges finds the activity records that no earlier look counted, in the view from the last settled one on, and returns the
// ones another member wrote, oldest first. It adds them to the unseen count and remembers the newest as the last change.
func (b *board) collectChanges() []Change {
	br := b.braid.Load()
	if br == nil {
		return nil
	}
	v, st := br.View(), br.Settled()
	defer v.Close()
	defer st.Close()
	settled := activityTotal(st)
	self := b.selfKey()
	var out []Change
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.seen == nil {
		b.seen = map[oid]bool{}
	}
	// A record before b.watch is settled and was looked at; the others are the settled records since and the tentative ones.
	keep := map[oid]bool{}
	for _, it := range readActivityFrom(v, b.watch) {
		keep[it.Entry] = true
		if b.seen[it.Entry] {
			continue
		}
		b.seen[it.Entry] = true
		if string(it.Writer[:]) == string(self) || !contentKinds[it.Kind] {
			continue
		}
		who := ""
		if n, err := v.Get(keyOf(prefixName, it.Writer[:])); err == nil {
			who = string(n)
		}
		ch := Change{Kind: activityKinds[it.Kind], Who: peerName(who, it.Writer[:]), WhoKey: it.Writer.String()}
		switch it.Kind {
		case actCardAdd, actCardEdit, actCardMove, actCardDel:
			ch.Card, ch.Column = truncate(cleanLine(it.Text), maxCardTitleRunes), cleanLine(it.Text2)
		case actColAdd, actColRename, actColMove, actColDel, actTitle:
			ch.Column = truncate(cleanLine(it.Text), maxColumnTitleRunes)
		}
		out = append(out, ch)
	}
	b.seen = keep
	b.watch = max(b.watch, settled)
	b.unseen += len(out)
	if n := len(out); n > 0 {
		last := out[n-1]
		b.last = &last
	}
	return out
}
