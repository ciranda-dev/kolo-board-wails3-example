package main

import (
	"context"
	"errors"
	"io/fs"
	"strings"

	kolobraid "github.com/ciranda-dev/kolo-braid"
	invite "github.com/ciranda-dev/kolo-invite"
	koloswarm "github.com/ciranda-dev/kolo-swarm"
)

// Sentinels of the app, for errors.Is. Every error a window sees is a *BoardError that wraps one of them or an error of
// a Kolo library.
var (
	ErrInvalidInvite = errors.New("kolo-board: not a board invite")
	ErrBusy          = errors.New("kolo-board: busy")
	ErrNotFound      = errors.New("kolo-board: inviter not found")
	ErrBadWelcome    = errors.New("kolo-board: unusable answer from the inviter")
	ErrNotReady      = errors.New("kolo-board: network not ready")
	ErrNoBoard       = errors.New("kolo-board: no board")
	ErrBadName       = errors.New("kolo-board: bad display name")
	ErrBadText       = errors.New("kolo-board: bad text")
	ErrNoSuchItem    = errors.New("kolo-board: no such column, card, member, invite or request")
	ErrNoRoom        = errors.New("kolo-board: no room to put it there")
	ErrMainOnly      = errors.New("kolo-board: only the main window may do this")
	ErrLastAdmin     = errors.New("kolo-board: the last admin stays")
	ErrLastSettler   = errors.New("kolo-board: the last settler stays")
	ErrNotSent       = errors.New("kolo-board: the removal did not reach another member")
	ErrLimit         = errors.New("kolo-board: limit reached")
	ErrClosed        = errors.New("kolo-board: closed")
)

// Kinds of BoardError: broad groups, as the window sees them.
const (
	kindInvalidInvite = "invalid-invite"
	kindNotFound      = "not-found"
	kindRefused       = "refused"
	kindExpired       = "expired"
	kindBusy          = "busy"
	kindNetwork       = "network"
	kindName          = "name"
	kindText          = "text"
	kindPermission    = "permission"
	kindSync          = "sync"
	kindLimit         = "limit"
	kindConfig        = "config"
	kindEnded         = "ended"
	kindCancelled     = "cancelled"
	kindInternal      = "internal"
)

// reasons gives every error sentence a stable ID, its kind and its English text. {detail} is a name, a number or a
// setting, and {cause} the plain words of the underlying error. A window in another language translates by reason and
// fills in the same two values; the English Message is the fallback.
var reasons = map[string]struct{ kind, text string }{
	"cancelled":             {kindCancelled, "Cancelled."},
	"invalid-invite":        {kindInvalidInvite, "This is not a board invite. Copy the whole invite, it starts with koloinv0."},
	"inviter-not-found":     {kindNotFound, "Could not find the person who invited you. They must be online with Kolo Board open and use the same entry nodes."},
	"inviter-lost":          {kindNotFound, "The connection to the person who invited you ended before they answered. Try again: asking twice is safe."},
	"join-denied":           {kindRefused, "The invite was not accepted: the person who made it said no, or it is not theirs to give. Ask for a new invite."},
	"invite-expired":        {kindExpired, "This invite has expired. Ask for a new one."},
	"invite-used-up":        {kindExpired, "This invite has no uses left. Ask for a new one."},
	"invite-revoked":        {kindExpired, "This invite was revoked. Ask for a new one."},
	"invite-unknown":        {kindInvalidInvite, "The person who made this invite does not recognize it. Check that you pasted the whole invite."},
	"join-rate-limited":     {kindBusy, "Too many attempts. Wait a minute and try again."},
	"inviter-busy":          {kindBusy, "The person who invited you is busy. Try again in a moment."},
	"bad-welcome":           {kindRefused, "The answer to your request is not one this version of Kolo Board can use. Ask for a new invite."},
	"join-sync-timeout":     {kindSync, "You were let in, but the board did not arrive in time. Ask for a new invite and try again."},
	"already-member":        {kindRefused, "This person is already a member of the board."},
	"board-exists":          {kindBusy, "You have a board already. Leave it first to create or join another one."},
	"busy-joining":          {kindBusy, "A join is in progress. Wait for it or cancel it first."},
	"busy-restarting":       {kindBusy, "The network is restarting. Try again in a moment."},
	"app-closed":            {kindBusy, "Kolo Board is closing. Start it again to go on."},
	"no-board":              {kindBusy, "You have no board. Create one or join one with an invite."},
	"board-not-active":      {kindBusy, "The board is not ready yet. Wait until it has loaded."},
	"network-not-ready":     {kindNetwork, "The network is not ready yet. Wait a moment, or press Try again if it failed."},
	"first-node-failed":     {kindNetwork, "Could not start the first entry node on {detail}: {cause}."},
	"public-unreachable":    {kindNetwork, "Could not reach the public network: {cause}. Check your internet connection and press Try again. Or choose another network: start Kolo Board with -bootstrap and the entry nodes of your own network, or with -local to use this computer only."},
	"bootstrap-failed":      {kindNetwork, "Could not reach the entry nodes ({detail}): {cause}. Check that they run and that -bootstrap names them. Or start Kolo Board without -bootstrap for the public network, or with -local to use this computer only."},
	"local-port-taken":      {kindNetwork, "Another program uses {detail} on this computer, so the local network cannot start. Close that program and press Try again, or start Kolo Board without -local for the public network, or with -bootstrap and the entry nodes of your own network."},
	"lookup-failed":         {kindNetwork, "Could not look for the person who invited you: {cause}."},
	"name-empty":            {kindName, "Write a name other members will see."},
	"name-too-long":         {kindName, "The name is too long. Use at most {detail} characters."},
	"board-title-empty":     {kindText, "Write a title for the board."},
	"board-title-too-long":  {kindText, "The board title is too long. Use at most {detail} characters."},
	"column-title-empty":    {kindText, "Write a title for the column."},
	"column-title-too-long": {kindText, "The column title is too long. Use at most {detail} characters."},
	"card-title-empty":      {kindText, "Write a title for the card."},
	"card-title-too-long":   {kindText, "The card title is too long. Use at most {detail} characters."},
	"card-text-too-long":    {kindText, "The description is too long. Use at most {detail} characters."},
	"bad-color":             {kindText, "That is not one of the card colors."},
	"column-not-found":      {kindNotFound, "That column is not on the board any more. Someone may have deleted it."},
	"card-not-found":        {kindNotFound, "That card is not on the board any more. Someone may have deleted it."},
	"member-not-found":      {kindNotFound, "That person is not a member of the board."},
	"invite-not-found":      {kindNotFound, "That invite does not exist."},
	"request-not-found":     {kindNotFound, "That request is gone: the person left or the invite ended."},
	"reorder-no-room":       {kindLimit, "There is no room to put it there. Move it to the start or the end instead."},
	"entry-too-large":       {kindLimit, "That is too large to send: {cause}."},
	"invite-ttl-range":      {kindLimit, "Choose how long the invite lasts: at least 1 second."},
	"activity-limit":        {kindLimit, "Ask for at least 1 item."},
	"invite-uses-range":     {kindLimit, "An invite takes 1 to {detail} people."},
	"not-admin":             {kindPermission, "Only an admin can do this."},
	"last-admin":            {kindPermission, "The last admin cannot be removed. Make someone else an admin first."},
	"last-settler":          {kindPermission, "The only computer that settles the board cannot be removed: nothing would settle any more. Settling is shared once the board has three members."},
	"leave-not-sent":        {kindSync, "You left on this computer, but the others still list you: your removal did not reach them. Stay online with the board open to send it, or leave again to delete this copy; an admin can remove you."},
	"main-window-only":      {kindPermission, "Only the main window can do this."},
	"not-member-yet":        {kindSync, "You are not a member yet: the board is still arriving. Try again in a moment."},
	"you-were-removed":      {kindEnded, "You were removed from this board. You keep what you had received; nothing new reaches you."},
	"strand-unconfirmed":    {kindSync, "Waiting for another member to be online before you can edit. Your last changes are kept."},
	"strand-lost":           {kindSync, "Another copy of your data folder wrote to this board. This copy can no longer edit it. Never copy or run a copy of the data folder."},
	"board-halted":          {kindSync, "The board stopped on this computer: {cause}. Restart Kolo Board to try again."},
	"board-diverged":        {kindSync, "This computer's view of the board differs from the one other members agreed on. It stopped to avoid showing wrong data."},
	"settlement-halted":     {kindSync, "A member signed two histories of its changes. Changes still reach everyone, but they cannot be settled any more."},
	"board-open":            {kindSync, "Could not open the board: {cause}."},
	"config-setting":        {kindConfig, "The setting -{detail} {cause}."},
	"data-dir":              {kindConfig, "Could not open the data directory: {cause}."},
	"data-dir-open":         {kindConfig, "The data directory {detail} is open to other users. Make it private (chmod 700) or choose another one."},
	"internal":              {kindInternal, "Something went wrong: {cause}."},

	// Connection relays (-relay): why one holds no place for the app, and a join that found nobody through them either.
	"relay-wrong-key":          {kindNetwork, "The connection relay {detail} answered with another key. Check the key given with -relay: it is the kolo-id the relay prints when it starts."},
	"relay-bad-key":            {kindNetwork, "The key of the connection relay {detail} cannot be used. Check the key given with -relay."},
	"relay-not-allowed":        {kindNetwork, "The connection relay {detail} has an allow list that does not name this app. Ask its operator to add your kolo-id, which Kolo Board prints in the terminal when it connects."},
	"relay-full":               {kindNetwork, "The connection relay {detail} is full. Try again later, or list another relay."},
	"relay-unreachable":        {kindNetwork, "Could not reach the connection relay {detail}: {cause}."},
	"inviter-not-found-relays": {kindNotFound, "Could not find the person who invited you, directly or through the connection relays. They must be online with Kolo Board open and use the same entry nodes, and both of you must list the same connection relay with the key it prints."},
}

// BoardError is what the window shows. Reason is the stable ID of the sentence and Kind its group; Detail and Cause fill
// the sentence; Message is the sentence in English. Error returns Message, so a rejected binding carries it; Unwrap gives
// the cause for errors.Is.
type BoardError struct {
	Kind    string `json:"kind"`
	Reason  string `json:"reason"`
	Detail  string `json:"detail"`
	Cause   string `json:"cause"`
	Message string `json:"message"`
	Err     error  `json:"-"`
}

func (e *BoardError) Error() string { return e.Message }
func (e *BoardError) Unwrap() error { return e.Err }

// boardErr makes the BoardError of reason. cause is the plain text of err when err is not one of the app's sentinels,
// so a sentence never shows "kolo-board: ...".
func boardErr(reason string, err error, detail string) *BoardError {
	cause := ""
	if err != nil {
		cause = plainCause(err)
	}
	return &BoardError{Kind: reasons[reason].kind, Reason: reason, Detail: detail, Cause: cause, Message: sentence(reason, detail, cause), Err: err}
}

// sentence fills the English text of reason.
func sentence(reason, detail, cause string) string {
	return strings.NewReplacer("{detail}", detail, "{cause}", cause).Replace(reasons[reason].text)
}

// plainCause is the text of err for a sentence: the words of the OS for the common cases, without the library prefixes.
func plainCause(err error) string {
	switch {
	case errors.Is(err, fs.ErrPermission):
		return "permission denied"
	case errors.Is(err, fs.ErrNotExist):
		return "it does not exist"
	}
	s := err.Error()
	for _, p := range []string{"kolo-board: ", "kolobraid: ", "koloswarm: ", "invite: ", "kolodht: ", "kolostore: ", "kolorelay: ", "kolosecure: "} {
		s = strings.TrimPrefix(s, p)
	}
	return s
}

// explain turns any error of a call into a BoardError.
func explain(err error) *BoardError {
	var be *BoardError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &be):
		return be
	case errors.Is(err, context.Canceled):
		return boardErr("cancelled", err, "")
	case errors.Is(err, ErrInvalidInvite), errors.Is(err, invite.ErrInvalidCode):
		return boardErr("invalid-invite", err, "")
	case errors.Is(err, invite.ErrDenied):
		return boardErr("join-denied", err, "")
	case errors.Is(err, invite.ErrExpired):
		return boardErr("invite-expired", err, "")
	case errors.Is(err, invite.ErrUsedUp):
		return boardErr("invite-used-up", err, "")
	case errors.Is(err, invite.ErrRevoked):
		return boardErr("invite-revoked", err, "")
	case errors.Is(err, invite.ErrInvalid):
		return boardErr("invite-unknown", err, "")
	case errors.Is(err, invite.ErrRateLimited):
		return boardErr("join-rate-limited", err, "")
	case errors.Is(err, invite.ErrBusy):
		return boardErr("inviter-busy", err, "")
	case errors.Is(err, invite.ErrTooLarge), errors.Is(err, invite.ErrProtocol), errors.Is(err, ErrBadWelcome), errors.Is(err, kolobraid.ErrMalformed):
		return boardErr("bad-welcome", err, "")
	case errors.Is(err, invite.ErrNotAnswered):
		return boardErr("inviter-lost", err, "")
	case errors.Is(err, ErrNotFound), errors.Is(err, invite.ErrUnreachable):
		return boardErr("inviter-not-found", err, "")
	case errors.Is(err, ErrNotReady):
		return boardErr("network-not-ready", err, "")
	case errors.Is(err, ErrBusy):
		return boardErr("board-exists", err, "")
	case errors.Is(err, ErrNoBoard):
		return boardErr("no-board", err, "")
	case errors.Is(err, ErrClosed), errors.Is(err, koloswarm.ErrClosed), errors.Is(err, invite.ErrClosed), errors.Is(err, kolobraid.ErrClosed):
		return boardErr("app-closed", err, "")
	case errors.Is(err, ErrNoRoom):
		return boardErr("reorder-no-room", err, "")
	case errors.Is(err, ErrMainOnly):
		return boardErr("main-window-only", err, "")
	case errors.Is(err, ErrLastAdmin):
		return boardErr("last-admin", err, "")
	case errors.Is(err, ErrLastSettler):
		return boardErr("last-settler", err, "")
	case errors.Is(err, ErrNotSent):
		return boardErr("leave-not-sent", err, "")
	case errors.Is(err, kolobraid.ErrNotAdmin):
		return boardErr("not-admin", err, "")
	case errors.Is(err, kolobraid.ErrRemoved):
		return boardErr("you-were-removed", err, "")
	case errors.Is(err, kolobraid.ErrNotMember):
		return boardErr("not-member-yet", err, "")
	case errors.Is(err, kolobraid.ErrUnconfirmed):
		return boardErr("strand-unconfirmed", err, "")
	case errors.Is(err, kolobraid.ErrStrandLost):
		return boardErr("strand-lost", err, "")
	case errors.Is(err, kolobraid.ErrHalted):
		return boardErr("board-halted", err, "")
	case errors.Is(err, kolobraid.ErrDiverged):
		return boardErr("board-diverged", err, "")
	case errors.Is(err, kolobraid.ErrTooLarge):
		return boardErr("entry-too-large", err, "")
	case errors.Is(err, kolobraid.ErrActiveKey), errors.Is(err, kolobraid.ErrKnownStrand):
		return boardErr("already-member", err, "")
	case errors.Is(err, context.DeadlineExceeded): // after the braid's errors: they wrap the deadline of a wait
		return boardErr("inviter-not-found", err, "")
	}
	return boardErr("internal", err, "")
}
