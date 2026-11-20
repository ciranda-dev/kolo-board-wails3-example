/**
 * The newer of two network states by their seq. The answer of Start() is a snapshot that can arrive
 * after a newer board:network event (a loopback network is ready in milliseconds), and must not undo it.
 */
export function newerNetwork(prev, next) {
  if (!next) {
    return prev
  }
  return (Number(next.seq) || 0) >= (Number(prev?.seq) || 0) ? next : prev
}

/**
 * The guard of the board, which the window holds or drops (null): an event, the answer of Board(),
 * CreateBoard() or JoinBoard(), each with the seq of the moment Go read it. Only one that is not older
 * than the last one taken may change the window; null (no board) only before anything was taken.
 */
export function createSeqGuard() {
  let last = 0
  return {
    /** Whether next may be shown, and if so it becomes the last one taken. */
    take(next) {
      if (!next) {
        return last === 0
      }
      const seq = Number(next.seq) || 0
      if (seq < last) {
        return false
      }
      last = seq
      return true
    },
  }
}

/**
 * A guard for a state that has events but no seq (the invites, the notch). Every change of such a
 * state sends an event, so an answer of a getter is stale when an event arrived while it was asked
 * for: the event, or one after it, already says what is true now.
 */
export function createGate() {
  let events = 0
  return {
    /** An event arrived. */
    event() {
      events++
    },
    /** Call before asking; pass the result to fresh() with the answer. */
    mark() {
      return events
    },
    /** Whether an answer asked for at mark may be used. */
    fresh(mark) {
      return mark === events
    },
  }
}
