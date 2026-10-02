package app

import (
	"fmt"
	"testing"
)

// replayPlan plays a catch-up plan through the follower's two cursors the way
// consumeStream does, and fails the test if the stream it describes cannot be
// decrypted end to end or strands the follower. It returns the durable
// boundary the follower reconnects from once the connection ends.
//
//   - Every envelope must be sealed at exactly the seq the follower tries
//     next: the gap, if any, lands it one below the first row, and each row
//     follows the one before it.
//   - A plan that is not truncated stays registered for live fan-out, whose
//     next envelope is sealed at pubLastSeq+1. Unless the stream ends exactly
//     at pubLastSeq, the follower cannot decrypt it.
//   - A truncated plan is drain-closed and the follower reconnects from its
//     boundary, which only a gap or a clip_end moves. A truncated plan that
//     moves neither is answered by the identical plan on every reconnect.
func replayPlan(t *testing.T, plan catchupPlan, sinceSeq, pubLastSeq uint64) (boundary uint64) {
	t.Helper()
	wire, boundary := sinceSeq, sinceSeq
	if plan.GapTarget > 0 || plan.Rewind {
		wire, boundary = plan.GapTarget, plan.GapTarget
	}
	for _, r := range plan.Send {
		if r.Seq != wire+1 {
			t.Fatalf("row at seq %d is sent where the follower expects seq %d, so it cannot be decrypted (plan %+v)", r.Seq, wire+1, plan)
		}
		wire = r.Seq
		if r.Kind == KindClipEnd {
			boundary = wire
		}
	}
	if !plan.Truncated && wire != pubLastSeq {
		t.Fatalf("plan stays registered for live fan-out with the follower at seq %d, but the next live envelope is sealed at %d (plan %+v)", wire, pubLastSeq+1, plan)
	}
	if plan.Truncated && boundary <= sinceSeq {
		t.Fatalf("truncated plan leaves the follower's boundary at %d from since %d, so it reconnects into the same plan forever (plan %+v)", boundary, sinceSeq, plan)
	}
	return boundary
}

// catchUp pages a follower through a fixed ring the way reconnects do: one
// handshake per round, each starting from the boundary the last one left,
// until a plan is not truncated. It returns the clip_end seqs delivered, in
// order. Every truncated round must move the boundary (replayPlan checks), so
// the round count is bounded by the ring's size.
func catchUp(t *testing.T, ring []RingRowMeta, sinceSeq, pubLastSeq uint64, caps catchupCaps) (clipEnds []uint64) {
	t.Helper()
	for round := 0; round <= len(ring)+1; round++ {
		plan := planCatchupBatch(ringAbove(ring, sinceSeq), sinceSeq, pubLastSeq, caps, false)
		for _, r := range plan.Send {
			if r.Kind == KindClipEnd {
				clipEnds = append(clipEnds, r.Seq)
			}
		}
		next := replayPlan(t, plan, sinceSeq, pubLastSeq)
		if !plan.Truncated {
			return clipEnds
		}
		sinceSeq = next
	}
	t.Fatalf("follower still catching up after %d rounds over a %d-row ring", len(ring)+2, len(ring))
	return nil
}

// ringAbove is what RingRetransmitMeta returns for a ring with nothing past
// the TTL: the rows above sinceSeq.
func ringAbove(ring []RingRowMeta, sinceSeq uint64) []RingRowMeta {
	var out []RingRowMeta
	for _, r := range ring {
		if r.Seq > sinceSeq {
			out = append(out, r)
		}
	}
	return out
}

func seqsOf(rows []RingRowMeta) []uint64 {
	var out []uint64
	for _, r := range rows {
		out = append(out, r.Seq)
	}
	return out
}

func concatRows(parts ...[]RingRowMeta) []RingRowMeta {
	var out []RingRowMeta
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// TestPlanCatchupBatchEndsAtSeqDiscontinuity pins how catch-up crosses a hole
// in the ring's seqs. The main source is an identity-adopting restore: it
// keeps the backup's ring (seqs up to the backup's head, here 3) but resumes
// the publication a stride higher, so a follower that was behind the backup
// has a backlog below the hole and the publication's head above it. Eviction
// can leave smaller ones: the age sweep deletes by timestamp, so a clock that
// stepped back between two emissions can expire a later clip before an
// earlier one.
//
// A plan's one gap envelope opens the connection, so no stream carries a
// follower over a hole once it has been sent rows. The batch stops at the
// hole and drain-closes instead: the follower stores what it got, reconnects
// from the clip_end below the hole, and the head gap carries it over. Before
// this, the restore purged the backup's ring to keep it hole-free, and a
// lagging follower was gapped straight past clips the backup still held.
func TestPlanCatchupBatchEndsAtSeqDiscontinuity(t *testing.T) {
	// A restored head: the backup's 3 plus the stride. A real restore also
	// takes the wall-clock floor, which only moves it higher.
	const restoredHead = uint64(1)<<32 + 3

	cases := []struct {
		name          string
		ring          []RingRowMeta
		sinceSeq      uint64
		pubLastSeq    uint64
		caps          catchupCaps
		wantSendSeqs  []uint64
		wantGapTo     uint64
		wantTruncated bool
		wantSkipped   []seqRange
		wantClipEnds  []uint64 // what catchUp delivers over every round
	}{
		{
			name:          "backup backlog under a restored head",
			ring:          clipRows(1, 100),
			pubLastSeq:    restoredHead,
			caps:          generousCaps(),
			wantSendSeqs:  []uint64{1, 2, 3},
			wantTruncated: true,
			wantClipEnds:  []uint64{3},
		},
		{
			name:          "backup backlog and post-restore clips in one window",
			ring:          concatRows(clipRows(1, 100), clipRows(restoredHead+1, 100)),
			pubLastSeq:    restoredHead + 3,
			caps:          generousCaps(),
			wantSendSeqs:  []uint64{1, 2, 3},
			wantTruncated: true,
			wantClipEnds:  []uint64{3, restoredHead + 3},
		},
		{
			name:          "the next round from the backup head gaps over the hole",
			ring:          concatRows(clipRows(1, 100), clipRows(restoredHead+1, 100)),
			sinceSeq:      3,
			pubLastSeq:    restoredHead + 3,
			caps:          generousCaps(),
			wantSendSeqs:  []uint64{restoredHead + 1, restoredHead + 2, restoredHead + 3},
			wantGapTo:     restoredHead,
			wantTruncated: false,
			wantClipEnds:  []uint64{restoredHead + 3},
		},
		{
			name:         "the next round with nothing published since the restore",
			ring:         clipRows(1, 100),
			sinceSeq:     3,
			pubLastSeq:   restoredHead,
			caps:         generousCaps(),
			wantGapTo:    restoredHead,
			wantClipEnds: nil,
		},
		{
			name:          "hole between clips left by the age sweep",
			ring:          concatRows(clipRows(1, 100), clipRows(7, 100)),
			pubLastSeq:    9,
			caps:          generousCaps(),
			wantSendSeqs:  []uint64{1, 2, 3},
			wantTruncated: true,
			wantClipEnds:  []uint64{3, 9},
		},
		{
			// Nothing is on the wire yet when the hole is reached, so the gap
			// that skips the oversized clip can carry the follower over the
			// hole as well. Landing it on the skipped clip's end instead put
			// the next row four seqs above where the follower expected it.
			name:         "hole right after a skipped clip folds into the gap",
			ring:         concatRows(clipRows(1, 300), clipRows(7, 100)),
			pubLastSeq:   9,
			caps:         catchupCaps{softBytes: 400, softSlots: 1 << 20, hardBytes: 500, hardSlots: 1 << 20},
			wantSendSeqs: []uint64{7, 8, 9},
			wantGapTo:    6,
			wantSkipped:  []seqRange{{Start: 1, End: 3}},
			wantClipEnds: []uint64{9},
		},
		{
			name:         "skipping the only clip under a restored head gaps to the head",
			ring:         clipRows(1, 300),
			pubLastSeq:   restoredHead,
			caps:         catchupCaps{softBytes: 400, softSlots: 1 << 20, hardBytes: 500, hardSlots: 1 << 20},
			wantGapTo:    restoredHead,
			wantSkipped:  []seqRange{{Start: 1, End: 3}},
			wantClipEnds: nil,
		},
		{
			// The steady state: the ring runs right up to the head, so the
			// follower ends exactly where live fan-out resumes. Truncating
			// here would drain-close every handshake and the follower would
			// reconnect forever.
			name:         "contiguous ring ending at the head stays registered",
			ring:         concatRows(clipRows(1, 100), clipRows(4, 100), clipRows(7, 100)),
			pubLastSeq:   9,
			caps:         generousCaps(),
			wantSendSeqs: []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9},
			wantClipEnds: []uint64{3, 6, 9},
		},
		{
			name:         "follower partway through a contiguous ring stays registered",
			ring:         concatRows(clipRows(4, 100), clipRows(7, 100)),
			sinceSeq:     3,
			pubLastSeq:   9,
			caps:         generousCaps(),
			wantSendSeqs: []uint64{4, 5, 6, 7, 8, 9},
			wantClipEnds: []uint64{6, 9},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := planCatchupBatch(ringAbove(tc.ring, tc.sinceSeq), tc.sinceSeq, tc.pubLastSeq, tc.caps, false)
			if got := seqsOf(plan.Send); fmt.Sprint(got) != fmt.Sprint(tc.wantSendSeqs) {
				t.Fatalf("send seqs %v want %v", got, tc.wantSendSeqs)
			}
			if plan.GapTarget != tc.wantGapTo {
				t.Fatalf("gap target %d want %d", plan.GapTarget, tc.wantGapTo)
			}
			if plan.Truncated != tc.wantTruncated {
				t.Fatalf("truncated %v want %v", plan.Truncated, tc.wantTruncated)
			}
			if fmt.Sprint(plan.Skipped) != fmt.Sprint(tc.wantSkipped) {
				t.Fatalf("skipped %v want %v", plan.Skipped, tc.wantSkipped)
			}
			replayPlan(t, plan, tc.sinceSeq, tc.pubLastSeq)

			if got := catchUp(t, tc.ring, tc.sinceSeq, tc.pubLastSeq, tc.caps); fmt.Sprint(got) != fmt.Sprint(tc.wantClipEnds) {
				t.Fatalf("catching up delivered the clips ending at %v, want %v", got, tc.wantClipEnds)
			}
		})
	}
}

// TestPlanCatchupBatchStopsAtHoleInPagedWindow covers the hole inside a
// LIMIT-truncated metadata window: the batch still stops at it, and the window
// clamp does not turn it into a gap past rows the window never saw.
func TestPlanCatchupBatchStopsAtHoleInPagedWindow(t *testing.T) {
	const restoredHead = uint64(1)<<32 + 3
	rows := concatRows(clipRows(1, 100), clipRows(restoredHead+1, 100))
	plan := planCatchupBatch(rows, 0, restoredHead+100, generousCaps(), true)
	if got := seqsOf(plan.Send); fmt.Sprint(got) != fmt.Sprint([]uint64{1, 2, 3}) {
		t.Fatalf("send seqs %v, want the rows below the hole only", got)
	}
	if plan.GapTarget != 0 || !plan.Truncated {
		t.Fatalf("gap %d truncated %v, want no gap and a truncated batch", plan.GapTarget, plan.Truncated)
	}
}

// TestPlanCatchupBatchLeavesBatchEndingMidClipRegistered pins the guard on
// the rule above: a batch that stops below the head is drain-closed only if
// it ends on a clip_end. One that ends inside a clip (a corrupt ring; no
// emission or eviction leaves that) moves no boundary, so truncating it would
// answer every reconnect with the identical batch, about once a second for as
// long as the rows survive.
func TestPlanCatchupBatchLeavesBatchEndingMidClipRegistered(t *testing.T) {
	rows := []RingRowMeta{ringRowOfSize(1, KindClipStart, 100), ringRowOfSize(2, KindClipChunk, 100)}
	plan := planCatchupBatch(rows, 0, 5, generousCaps(), false)
	if plan.Truncated {
		t.Fatalf("batch ending mid-clip was truncated (plan %+v): the follower's boundary stays at 0 and it reconnects into the same plan forever", plan)
	}
}
