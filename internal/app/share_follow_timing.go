package app

import (
	"math/rand/v2"
	"time"
)

// followTiming is a follower's reconnect policy: the backoff ladder, the
// jitter on each wait, and how long a silent session must stay up before it
// counts as accepted (see followSession). Production always runs
// defaultFollowTiming; tests shrink it with setFollowTimingForTest so the
// ladder can be walked in milliseconds, and turn jitter off so the number of
// redials in a window is exact rather than a distribution.
type followTiming struct {
	floor, cap  time.Duration
	acceptGrace time.Duration
	noJitter    bool
}

func defaultFollowTiming() followTiming {
	return followTiming{
		floor:       ReconnectFloor,
		cap:         ReconnectCap,
		acceptGrace: FollowAcceptGrace,
	}
}

// followTiming returns the effective reconnect policy.
func (m *ShareManager) followTiming() followTiming {
	if m.followTimingOverride != nil {
		return *m.followTimingOverride
	}
	return defaultFollowTiming()
}

// setFollowTimingForTest replaces the reconnect policy. Call it before any
// follow exists — runFollowLoop reads it once, without a lock, exactly like
// the other test knobs on ShareManager.
func (m *ShareManager) setFollowTimingForTest(ft followTiming) {
	m.followTimingOverride = &ft
}

// next returns the ladder rung for a follow whose session just ended.
// accepted reports whether the publisher accepted that session: one that
// actually worked resets the ladder to the floor, so the backoff measures
// *consecutive failed attempts* rather than the count of sessions since the
// app started. Failed dials and refused handshakes both grow it, capped at the
// ceiling.
func (ft followTiming) next(current time.Duration, accepted bool) time.Duration {
	if accepted {
		return ft.floor
	}
	next := current * 2
	if next > ft.cap {
		return ft.cap
	}
	return next
}

// wait is the pause actually taken before the next dial at rung backoff,
// drawn uniformly from [floor/2, backoff]: full jitter over the ladder, so the
// followers one publisher restart or pause dropped at the same instant spread
// out instead of redialing in lockstep.
//
// The draw never goes below half the floor. Drain-closed catch-up paging would
// be fine with a zero wait, since every lap delivers a batch, but the same
// floor reset follows any accepted session, including one that keeps failing
// right after acceptance (a staging write on a full disk, say). Unclamped,
// that follow could redial back to back; clamped, it makes at most two dials
// a second.
func (ft followTiming) wait(backoff time.Duration) time.Duration {
	lo := ft.floor / 2
	if ft.noJitter || backoff <= lo {
		return backoff
	}
	return lo + rand.N(backoff-lo+1)
}
