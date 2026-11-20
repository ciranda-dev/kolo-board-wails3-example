# Security

What the example protects and what it does not, in plain words. It claims no more than the code and the libraries give.

## What is protected

- **Every change is signed by the member who made it.** Each member writes into their own log, kolo-log signs each entry, and the others check it. A change cannot be forged as another member's.
- **Every member sees the same board.** kolo-braid merges the logs into one deterministic order, and every computer applies the same rules to it. A computer whose result differs from the one a quorum of settlers agreed on stops, and the window says so.
- **Connections between members are encrypted and authenticated** by kolo-secure.
- **A member cannot break the board with a bad change.** Whatever a member writes, the rules either apply it or reject it, the same on every computer. A change over a size or format bound is rejected; it never stops the board.
- **Text from other members is cleaned** of control, format and bidirectional characters before a window shows it, and is shown as text, never as HTML.
- **The invite stays in the main window.** It never goes to the notch page, to an event, to the tray or to a log. A request to join shows the person's name and key, not the code.

## What is not protected

- **Every member can read the whole board and its history.** There are no private cards. A removed member keeps everything they had received.
- **A change is never erased.** Deleting a card takes it off the board; the change that made it, with its text, stays in every member's history.
- **The order among changes made at the same time is not fair.** A settler can try to choose it, so a rule such as "the first change wins" is not offered.
- **Names are not verified.** A member chooses their own name; two members can choose the same one. The key identifies a member.
- **An invite code is a secret.** Whoever has it can ask to join, and without approval they join. The first members added become admins, who can add and remove anyone.
- **Members see each other's network addresses**, because they connect directly. Through a connection relay they do not, but the relay sees every member's kolo-id and network address, who connects to whom, when, and how much. It passes on bytes encrypted end to end between the members and cannot read the board, the invite or the names. Its key check at start shows it a throwaway key and your address, and holds no place.
- **A settler that signs two histories stops settling for good**, and with three or five settlers one that does so during a split can make settled prefixes differ. The window shows the state.

## Strangers and memory

The board's topic is announced on the DHT, the public one by default, so anyone who finds it can open a connection to a member's copy before they are a member; an invite is needed to join, not to connect. What a stranger can hold is bounded by kolo-transport and kolo-swarm:

- One source address may have 4 connections to a member's listener (kolo-transport's default; the example does not raise it), and each connection may open 16 streams. kolo-transport measures up to about 856 MiB of retained memory for one connection at 16 streams and about 3.3 GiB for one source address at 4 connections, when a peer sends one-byte frames on every stream. That is the worst case, not what a normal peer costs; the numbers are in the kolo-transport limits.
- The example sets a soft memory limit of 2 GiB with `debug.SetMemoryLimit` when `GOMEMLIMIT` is not set; a `GOMEMLIMIT` in the environment is used instead, and the start prints one log line with the limit. A soft limit only makes the garbage collector work harder as the heap nears it. It does not stop an allocation and does not free memory a peer keeps live, so the worst case above can still end the process: the limit is not a defense against a hostile address.
- Several members behind one address (an office) share the 4 connections; with five or more of them behind one address, some connections are refused until others close.
- At most 64 requests to join wait for an admin at once, all invites together, and 16 from one address; each is a name an admin reads.

## The log

The log (the terminal, or the system's log for the example) holds the example's own `kolo-id` key line at start, the soft memory limit, short events (a connection relay that gave no place, a peer's 8-character key prefix and why a connection ended, an invite that could not be announced, an error a window reported, cut to 8,192 characters) and file errors. It holds no board text, card, name, invite code or seed. People paste it into issues, so read it first.

## Data on disk

The data directory is created with mode 0700 and refused when open to others. The identity is a file with mode 0600. The small state files (`board.json`, `invites.json`, the settings) are replaced by writing a temporary file, flushing it, renaming it over the old one and flushing the directory, so a power loss leaves the old content or the new, never a part. The example adds no encryption of its own to the files in it. Do not copy a data directory or run a copy of it.

## Reporting

See [SECURITY.md](../SECURITY.md).
