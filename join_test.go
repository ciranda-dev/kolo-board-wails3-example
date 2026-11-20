package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	koloid "github.com/ciranda-dev/kolo-id"
	invite "github.com/ciranda-dev/kolo-invite"
	kolostore "github.com/ciranda-dev/kolo-store"
)

func TestParseInviteTextBoundsThePastedText(t *testing.T) {
	if _, err := parseInviteText(strings.Repeat("a", maxCodeText+1)); !errors.Is(err, invite.ErrInvalidCode) {
		t.Errorf("text over %d bytes: %v", maxCodeText, err)
	}
	if _, err := parseInviteText(strings.Repeat(" \n", 1<<20)); !errors.Is(err, invite.ErrInvalidCode) {
		t.Errorf("a megabyte of spaces: %v", err)
	}
	s := NewBoardService(Config{}, nil)
	_, err := s.JoinBoard(mainCtx, strings.Repeat("a", 1<<20))
	if explain(err).Reason != "invalid-invite" {
		t.Errorf("JoinBoard of a huge text: %v", err)
	}
}

func TestParseInviteTextIgnoresSpacesAndLineBreaks(t *testing.T) {
	id, err := koloid.Generate()
	if err != nil {
		t.Fatal(err)
	}
	db := kolostore.NewMemory()
	defer db.Close()
	inv, err := invite.Open(db, id, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer inv.Close()
	text, _, err := inv.Create(&invite.CreateOptions{TTL: time.Hour, MaxUses: 1, Manual: true})
	if err != nil {
		t.Fatal(err)
	}
	want, err := parseInviteText(text)
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseInviteText(" " + text[:10] + " \n\t" + text[10:20] + "\n" + text[20:] + "\n")
	if err != nil || got.ID() != want.ID() {
		t.Errorf("a code with spaces and line breaks: %v, %v", got, err)
	}
}

func TestInviteOptionsBoundThePendingRequests(t *testing.T) {
	o := inviteOptions()
	if o.MaxPending != maxPendingRequests || maxPendingRequests != 64 {
		t.Errorf("MaxPending %d, want %d", o.MaxPending, maxPendingRequests)
	}
	id, err := koloid.Generate()
	if err != nil {
		t.Fatal(err)
	}
	db := kolostore.NewMemory()
	defer db.Close()
	inv, err := invite.Open(db, id, o)
	if err != nil {
		t.Fatalf("kolo-invite refuses the options: %v", err)
	}
	inv.Close()
}

func TestActivityPageIsBounded(t *testing.T) {
	p := startPeer(t, "Ana", Config{}, nil)
	if _, err := p.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	todo := p.board(t).column("To do").ID
	for i := range maxActivityPage + 20 {
		if _, err := p.AddCard(todo, CardInput{Title: fmt.Sprint("card ", i)}, 0); err != nil {
			t.Fatal(err)
		}
	}
	act, err := p.Activity(0, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	if len(act.Items) != maxActivityPage || !act.More {
		t.Errorf("%d items, more %v; want %d and more", len(act.Items), act.More, maxActivityPage)
	}
	if _, err := p.Activity(0, 0); explain(err).Reason != "activity-limit" {
		t.Errorf("limit 0: %v", err)
	}
}
