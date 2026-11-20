package main

import (
	"errors"
	"slices"
	"strconv"
	"unicode/utf8"

	kolobraid "github.com/ciranda-dev/kolo-braid"
)

// mutate reads the board as this member sees it, lets plan turn that into operations, and writes them to the own strand
// in order. It returns the ID of the entry of the last operation, which is the ID of what an add creates.
func (s *BoardService) mutate(plan func(b *board, m *model) ([]op, error)) (oid, error) {
	b, err := s.activeBoard()
	if err != nil {
		return oid{}, err
	}
	v := b.braid.Load().View()
	m := readModel(v)
	v.Close()
	ops, err := plan(b, m)
	if err != nil {
		return oid{}, explain(err)
	}
	var last oid
	for _, o := range ops {
		ref, err := b.write(o)
		if err != nil {
			return oid{}, explain(err)
		}
		last = oid(ref.ID)
	}
	b.kick()
	return last, nil
}

// withFix puts the operation that moves a sibling out of the way before the one that needs the room.
func withFix(sl slot, fix func(sibling) op, o op) []op {
	if sl.Fix == nil {
		return []op{o}
	}
	return []op{fix(*sl.Fix), o}
}

func noRoom(err error) error {
	if errors.Is(err, ErrNoRoom) {
		return boardErr("reorder-no-room", err, "")
	}
	return err
}

func findColumn(m *model, id string) (*modelColumn, error) {
	cid, ok := parseOID(id)
	if ok {
		if c := m.column(cid); c != nil {
			return c, nil
		}
	}
	return nil, boardErr("column-not-found", ErrNoSuchItem, "")
}

func findCard(m *model, id string) (*modelColumn, *modelCard, error) {
	cid, ok := parseOID(id)
	if ok {
		if col, c := m.find(cid); c != nil {
			return col, c, nil
		}
	}
	return nil, nil, boardErr("card-not-found", ErrNoSuchItem, "")
}

// colorNumber maps a color name to its number; "" is no color.
func colorNumber(name string) (uint8, error) {
	if i := slices.Index(colors[:], name); i >= 0 {
		return uint8(i), nil
	}
	return 0, boardErr("bad-color", ErrBadText, "")
}

func cleanDescription(s string) (string, error) {
	d := cleanText(s)
	if utf8.RuneCountInString(d) > maxCardDescRunes {
		return "", boardErr("card-text-too-long", ErrBadText, strconv.Itoa(maxCardDescRunes))
	}
	return d, nil
}

// RenameBoard changes the title of the board; only an admin can.
func (s *BoardService) RenameBoard(title string) error {
	title, err := checkLine(title, maxBoardTitleRunes, "board-title", ErrBadText)
	if err != nil {
		return err
	}
	_, err = s.mutate(func(b *board, _ *model) ([]op, error) {
		if !b.isAdmin() {
			return nil, boardErr("not-admin", kolobraid.ErrNotAdmin, "")
		}
		return []op{{Kind: opTitle, Text: title}}, nil
	})
	return err
}

// AddColumn adds a column at index (0 is the first; past the end is the end) and returns its ID.
func (s *BoardService) AddColumn(title string, index int) (string, error) {
	title, err := checkLine(title, maxColumnTitleRunes, "column-title", ErrBadText)
	if err != nil {
		return "", err
	}
	id, err := s.mutate(func(_ *board, m *model) ([]op, error) {
		sl, err := plan(columnSiblings(m, oid{}), index)
		if err != nil {
			return nil, noRoom(err)
		}
		return withFix(sl, func(f sibling) op { return op{Kind: opColMove, ID: f.ID, Pos: f.Pos} }, op{Kind: opColAdd, Text: title, Pos: sl.Pos}), nil
	})
	return id.String(), err
}

// RenameColumn changes the title of a column.
func (s *BoardService) RenameColumn(id, title string) error {
	title, err := checkLine(title, maxColumnTitleRunes, "column-title", ErrBadText)
	if err != nil {
		return err
	}
	_, err = s.mutate(func(_ *board, m *model) ([]op, error) {
		c, err := findColumn(m, id)
		if err != nil {
			return nil, err
		}
		return []op{{Kind: opColRename, ID: c.ID, Text: title}}, nil
	})
	return err
}

// MoveColumn moves a column to index among the other columns.
func (s *BoardService) MoveColumn(id string, index int) error {
	_, err := s.mutate(func(_ *board, m *model) ([]op, error) {
		c, err := findColumn(m, id)
		if err != nil {
			return nil, err
		}
		sl, err := plan(columnSiblings(m, c.ID), index)
		if err != nil {
			return nil, noRoom(err)
		}
		return withFix(sl, func(f sibling) op { return op{Kind: opColMove, ID: f.ID, Pos: f.Pos} }, op{Kind: opColMove, ID: c.ID, Pos: sl.Pos}), nil
	})
	return err
}

// DeleteColumn deletes a column and the cards in it. A card another member adds to it meanwhile is lost with it.
func (s *BoardService) DeleteColumn(id string) error {
	_, err := s.mutate(func(_ *board, m *model) ([]op, error) {
		c, err := findColumn(m, id)
		if err != nil {
			return nil, err
		}
		return []op{{Kind: opColDel, ID: c.ID}}, nil
	})
	return err
}

// AddCard adds a card to a column at index (0 is the top; past the end is the bottom) and returns its ID.
func (s *BoardService) AddCard(columnID string, in CardInput, index int) (string, error) {
	title, err := checkLine(in.Title, maxCardTitleRunes, "card-title", ErrBadText)
	if err != nil {
		return "", err
	}
	desc, err := cleanDescription(in.Description)
	if err != nil {
		return "", err
	}
	color, err := colorNumber(in.Color)
	if err != nil {
		return "", err
	}
	id, err := s.mutate(func(b *board, m *model) ([]op, error) {
		col, err := findColumn(m, columnID)
		if err != nil {
			return nil, err
		}
		o := op{Kind: opCardAdd, Col: col.ID, Text: title, Desc: desc, Color: color}
		if in.Assignee != "" {
			k, err := b.memberKey(in.Assignee)
			if err != nil {
				return nil, err
			}
			o.HasAssignee, o.Assignee = true, oid(k)
		}
		sl, err := plan(cardSiblings(col, oid{}), index)
		if err != nil {
			return nil, noRoom(err)
		}
		o.Pos = sl.Pos
		return withFix(sl, func(f sibling) op { return op{Kind: opCardMove, ID: f.ID, Col: col.ID, Pos: f.Pos} }, o), nil
	})
	return id.String(), err
}

// EditCard changes the fields of a card that are not null in edit; the others stay as they are, even when another member
// changes them at the same time. When two members change one field at once, the one later in the board's order wins.
func (s *BoardService) EditCard(id string, edit CardEdit) error {
	o := op{Kind: opCardEdit}
	if edit.Title != nil {
		t, err := checkLine(*edit.Title, maxCardTitleRunes, "card-title", ErrBadText)
		if err != nil {
			return err
		}
		o.Mask |= fieldTitle
		o.Text = t
	}
	if edit.Description != nil {
		d, err := cleanDescription(*edit.Description)
		if err != nil {
			return err
		}
		o.Mask |= fieldDesc
		o.Desc = d
	}
	if edit.Color != nil {
		c, err := colorNumber(*edit.Color)
		if err != nil {
			return err
		}
		o.Mask |= fieldColor
		o.Color = c
	}
	if edit.Assignee != nil {
		o.Mask |= fieldAssignee
	}
	if o.Mask == 0 {
		return nil
	}
	_, err := s.mutate(func(b *board, m *model) ([]op, error) {
		_, c, err := findCard(m, id)
		if err != nil {
			return nil, err
		}
		o.ID = c.ID
		if edit.Assignee != nil && *edit.Assignee != "" {
			k, err := b.memberKey(*edit.Assignee)
			if err != nil {
				return nil, err
			}
			o.HasAssignee, o.Assignee = true, oid(k)
		}
		return []op{o}, nil
	})
	return err
}

// MoveCard moves a card to a column at index (0 is the top; past the end is the bottom), in the same column or another.
func (s *BoardService) MoveCard(id, columnID string, index int) error {
	_, err := s.mutate(func(_ *board, m *model) ([]op, error) {
		_, c, err := findCard(m, id)
		if err != nil {
			return nil, err
		}
		col, err := findColumn(m, columnID)
		if err != nil {
			return nil, err
		}
		sl, err := plan(cardSiblings(col, c.ID), index)
		if err != nil {
			return nil, noRoom(err)
		}
		return withFix(sl, func(f sibling) op { return op{Kind: opCardMove, ID: f.ID, Col: col.ID, Pos: f.Pos} },
			op{Kind: opCardMove, ID: c.ID, Col: col.ID, Pos: sl.Pos}), nil
	})
	return err
}

// DeleteCard deletes a card. A change another member makes to it afterwards, in the board's order, is lost.
func (s *BoardService) DeleteCard(id string) error {
	_, err := s.mutate(func(_ *board, m *model) ([]op, error) {
		_, c, err := findCard(m, id)
		if err != nil {
			return nil, err
		}
		return []op{{Kind: opCardDel, ID: c.ID}}, nil
	})
	return err
}
