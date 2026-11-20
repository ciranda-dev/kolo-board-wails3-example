package main

// Info is what Start returns.
type Info struct {
	DataDir string  `json:"dataDir"`
	Version string  `json:"version"`
	Network Network `json:"network"`
	// KeyID is the public key other members know this one by, in hex. It is not a secret.
	KeyID string `json:"keyId"`
}

// Network is the payload of the "board:network" event.
type Network struct {
	// Mode is the network: "public" (the default), "own" (-bootstrap or -first) or "local" (-local, this computer only).
	Mode       string      `json:"mode"`
	State      string      `json:"state"`
	Message    string      `json:"message"`
	Error      *BoardError `json:"error"` // the reason of Message, to translate it; nil when Message is ""
	EntryNodes int         `json:"entryNodes"`
	// FirstNode is the entry node this app runs: the one of -first, or in -local the one it took when it was the first window.
	FirstNode string `json:"firstNode"`
	// LocalNode is the address of -local, "" in the other networks.
	LocalNode string `json:"localNode"`
	// Relays are the connection relays of the settings and their states; never null.
	Relays []RelayStatus `json:"relays"`
	// Seq grows with every state, so the window keeps the newer of an event and the snapshot of Start.
	Seq uint64 `json:"seq"`
}

// Network states.
const (
	netStarting = "starting"
	netReady    = "ready"
	netFailed   = "failed"
)

// Board states, and the phases of a join.
const (
	stateCreating = "creating"
	stateJoining  = "joining"
	stateActive   = "active"
	stateRemoved  = "removed"
	stateEnded    = "ended"

	phaseFinding = "finding" // looking for the person who made the invite
	phaseWaiting = "waiting" // the request is sent, the answer has not come
	phaseSyncing = "syncing" // let in: the board is arriving
)

// Board is the board as a window shows it: the payload of the "board:state" event.
type Board struct {
	// ID is the group ID of the braid in hex: the same on every member's computer. Empty until the board exists.
	ID    string `json:"id"`
	State string `json:"state"`
	Phase string `json:"phase"`
	Title string `json:"title"`
	// Self is the key of this person, to find them in People.
	Self    string   `json:"self"`
	Columns []Column `json:"columns"`
	// People are the members, and the former members that wrote or are named on a card.
	People []Person `json:"people"`
	// Requests are the people who asked to join with an invite that needs approval; only an admin has any.
	Requests []JoinRequest `json:"requests"`
	Sync     SyncInfo      `json:"sync"`
	// CanEdit says that a change would be written now. When it is false, Blocked says why.
	CanEdit bool        `json:"canEdit"`
	Blocked *BoardError `json:"blocked"`
	// Unseen counts the changes by other members since the window called MarkSeen.
	Unseen int `json:"unseen"`
	// Error is why the board ended, or the problem of a board that is running.
	Error *BoardError `json:"error"`
	// Seq grows with every board read, for every board of the app, so the window keeps the newest of an event and an answer.
	Seq uint64 `json:"seq"`
	// Leave says what LeaveBoard does now: "everyone" (removes this member for all members, then deletes this computer's
	// copy), "alone" (deletes the board), "not-admin" or "local" (deletes only this computer's copy: the others still list
	// this member), "removed", "cancel" (a join), or "last-admin" and "last-settler" (refused).
	Leave string `json:"leave"`
}

// Column is a column and its cards, in order.
type Column struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Syncing bool   `json:"syncing"`
	Cards   []Card `json:"cards"`
}

// Card is a card. Title and Description are text written by members: shown as text, never as HTML.
type Card struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Assignee    string `json:"assignee"` // a key in People, or ""
	Color       string `json:"color"`    // "", red, orange, yellow, green, blue or purple
	CreatedBy   string `json:"createdBy"`
	LastBy      string `json:"lastBy"`
	// Syncing is true while the card, as it is shown, differs from what is settled: a change of it is not settled yet.
	Syncing bool `json:"syncing"`
}

// Person is a member of the board, or a former member that a card still names.
type Person struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Tag    string `json:"tag"` // four hex digits of the key, to tell two people of one name apart
	You    bool   `json:"you"`
	Member bool   `json:"member"` // false for a former member
	Admin  bool   `json:"admin"`
	Seat   bool   `json:"seat"` // a settler: takes part in settling changes
	// Online is true while this computer has a direct connection to the member; yourself is always online.
	Online  bool `json:"online"`
	Syncing bool `json:"syncing"` // added, but not settled as a member yet
}

// SyncInfo says how much of the board is settled.
type SyncInfo struct {
	// Pending is the number of changes (entries of members) that are not settled yet, CardsSyncing and ColumnsSyncing the
	// cards and columns that differ from what is settled.
	Pending        int `json:"pending"`
	CardsSyncing   int `json:"cardsSyncing"`
	CardsSettled   int `json:"cardsSettled"`
	ColumnsSyncing int `json:"columnsSyncing"`
	// Seats is the number of settlers and Quorum how many must be online for changes to settle; SeatsOnline is how many
	// this computer is connected to, itself included.
	Seats       int `json:"seats"`
	Quorum      int `json:"quorum"`
	SeatsOnline int `json:"seatsOnline"`
	// Settling is true while enough settlers are online for changes to settle. Halted says why settling stopped for good.
	// SingleSettler is true while one member's computer settles a board of several members: when it is off, nothing settles.
	// PendingSeats is the number of settlers being added: the change takes effect after they are ready and at the next
	// change anyone makes (kolo-braid starts no settling of its own on an idle board), until then Seats and Quorum are the
	// old ones.
	Settling      bool        `json:"settling"`
	PendingSeats  int         `json:"pendingSeats"`
	SingleSettler bool        `json:"singleSettler"`
	Halted        *BoardError `json:"halted"`
	// Stuck is true when this member's own changes stayed unsettled for a minute while enough settlers were connected:
	// members that removed it stop serving it without telling it, so the window says it may have been removed.
	Stuck bool `json:"stuck"`
}

// JoinRequest is a person who asked to join with an invite that needs approval.
type JoinRequest struct {
	Request string `json:"request"` // decimal, for ApproveRequest and DenyRequest
	Invite  string `json:"invite"`  // the invite's ID, not its code
	Joiner  string `json:"joiner"`  // the key of the person
	Name    string `json:"name"`
	Tag     string `json:"tag"`
}

// ActivityItem is one entry of the activity list: who did what.
type ActivityItem struct {
	Seq     uint64   `json:"seq"`
	Kind    string   `json:"kind"`    // board-created, board-renamed, name-changed, joined, removed, column-added, column-renamed, column-moved, column-deleted, card-added, card-edited, card-moved, card-deleted
	Who     string   `json:"who"`     // the key of the member who did it
	Subject string   `json:"subject"` // the card or column ID, or the member key for joined and removed
	Text    string   `json:"text"`    // the card or column title at the time, the new board title, or the new name
	Column  string   `json:"column"`  // card-moved and card-added: the column
	Fields  []string `json:"fields"`  // card-edited: title, description, assignee, color
	Cards   int      `json:"cards"`   // column-deleted: how many cards went with it
	Settled bool     `json:"settled"`
}

// Activity is a page of the activity list, newest first.
type Activity struct {
	Items []ActivityItem `json:"items"`
	// People names everyone in Items.
	People []Person `json:"people"`
	// More says that older items exist; ask again with Before set to the Seq of the last item.
	More bool `json:"more"`
}

// InviteOptions is what a member chooses for a new invite.
type InviteOptions struct {
	// TTLSeconds is how long it can be used, at least 1 second.
	TTLSeconds int64 `json:"ttlSeconds"`
	// Uses is how many people it lets in; 0 is one.
	Uses int `json:"uses"`
	// RequireApproval asks an admin to approve each person; without it, the first people to use it are let in at once.
	RequireApproval bool `json:"requireApproval"`
}

// InviteInfo describes an invite. Code is the invite's secret: only the answer of CreateInvite has it.
type InviteInfo struct {
	ID              string `json:"id"`
	Code            string `json:"code,omitempty"`
	Created         string `json:"created"` // RFC 3339
	Expires         string `json:"expires"` // RFC 3339
	Uses            int    `json:"uses"`
	Used            int    `json:"used"`
	RequireApproval bool   `json:"requireApproval"`
	State           string `json:"state"` // active, expired, used up or revoked
}

// Change is a change by another member, for a toast: the payload of the "board:change" event.
type Change struct {
	Kind   string `json:"kind"` // as ActivityItem.Kind
	Who    string `json:"who"`  // the name
	WhoKey string `json:"whoKey"`
	Card   string `json:"card"`
	Column string `json:"column"`
}

// CardInput is what a new card has. Assignee is the key of a member, or "".
type CardInput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Assignee    string `json:"assignee"`
	Color       string `json:"color"`
}

// CardEdit says which fields of a card change: a field that is null stays as it is.
type CardEdit struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Assignee    *string `json:"assignee"` // "" clears it
	Color       *string `json:"color"`    // "" clears it
}
