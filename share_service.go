package main

import (
	"fmt"

	coreapp "go-clipboard/internal/app"
)

// DisconnectFollowForTest cancels the active session for the given follow ID
// so the follow-loop re-enters its backoff → reconnect path. The follow row,
// last_seq, and follow-lifetime context are preserved (unlike Unfollow).
//
// test-only — do not call from production code paths.
func (s *ShareService) DisconnectFollowForTest(id int64) error {
	if s.app.ShareManager() == nil {
		return fmt.Errorf("share manager not initialized")
	}
	return s.app.ShareManager().DisconnectFollowForTest(id)
}

// ShareService exposes share operations to the frontend via Wails.
type ShareService struct {
	app *coreapp.App
}

func NewShareService(app *coreapp.App) *ShareService { return &ShareService{app: app} }

func (s *ShareService) StartShare(tagID int64) (ShareInfo, error) {
	if s.app.ShareManager() == nil {
		return ShareInfo{}, fmt.Errorf("share manager not initialized")
	}
	return s.app.ShareManager().StartShare(tagID)
}

func (s *ShareService) StopShare(tagID int64) error {
	if s.app.ShareManager() == nil {
		return fmt.Errorf("share manager not initialized")
	}
	return s.app.ShareManager().StopShare(tagID)
}

func (s *ShareService) Follow(shareString, localTagName string) (FollowInfo, error) {
	if s.app.ShareManager() == nil {
		return FollowInfo{}, fmt.Errorf("share manager not initialized")
	}
	return s.app.ShareManager().Follow(shareString, localTagName)
}

// TestFollowConnection probes peer reachability without persisting anything.
// UI wires this to the paste event so the user sees an immediate
// connecting-spinner, then either the tag picker (on success) or a friendly
// error with Retry + Follow-anyway options (on failure).
func (s *ShareService) TestFollowConnection(shareString string) error {
	if s.app.ShareManager() == nil {
		return fmt.Errorf("share manager not initialized")
	}
	return s.app.ShareManager().TestFollowConnection(shareString)
}

// FollowWithoutDial commits a follow without requiring an initial dial, for
// the "Follow anyway" UI flow after TestFollowConnection has already failed.
func (s *ShareService) FollowWithoutDial(shareString, localTagName string) (FollowInfo, error) {
	if s.app.ShareManager() == nil {
		return FollowInfo{}, fmt.Errorf("share manager not initialized")
	}
	return s.app.ShareManager().FollowWithoutDial(shareString, localTagName)
}

func (s *ShareService) Unfollow(followID int64) error {
	if s.app.ShareManager() == nil {
		return fmt.Errorf("share manager not initialized")
	}
	return s.app.ShareManager().Unfollow(followID)
}

// ReconnectFollow forces an immediate reconnection attempt for a follow,
// cancelling the live session (if any) and skipping the reconnect backoff
// wait. The UI wires this to the per-follow "Refresh" button.
func (s *ShareService) ReconnectFollow(followID int64) error {
	if s.app.ShareManager() == nil {
		return fmt.Errorf("share manager not initialized")
	}
	return s.app.ShareManager().ReconnectFollow(followID)
}

// PauseFollow stops reconnect attempts for a follow until ResumeFollow is
// called. Persisted so paused state survives app restart.
func (s *ShareService) PauseFollow(followID int64) error {
	if s.app.ShareManager() == nil {
		return fmt.Errorf("share manager not initialized")
	}
	return s.app.ShareManager().PauseFollow(followID)
}

// ResumeFollow clears the paused flag on a follow and kicks an immediate
// reconnection attempt.
func (s *ShareService) ResumeFollow(followID int64) error {
	if s.app.ShareManager() == nil {
		return fmt.Errorf("share manager not initialized")
	}
	return s.app.ShareManager().ResumeFollow(followID)
}

// PauseShare closes every active follower stream for the share, rejects
// further handshakes, and persists status="paused".
func (s *ShareService) PauseShare(tagID int64) error {
	if s.app.ShareManager() == nil {
		return fmt.Errorf("share manager not initialized")
	}
	return s.app.ShareManager().PauseShare(tagID)
}

// ResumeShare restores a paused share to status="active" so handshakes are
// accepted again. Followers reconnect on their own.
func (s *ShareService) ResumeShare(tagID int64) error {
	if s.app.ShareManager() == nil {
		return fmt.Errorf("share manager not initialized")
	}
	return s.app.ShareManager().ResumeShare(tagID)
}

// GetShareLogs returns recent share-system log entries, newest-first. Pass
// followID or publicationID > 0 to filter; pass both zero for all entries.
// The log is in-memory and capped at ~500 entries; older entries roll off.
func (s *ShareService) GetShareLogs(followID, publicationID int64) []ShareLogEntry {
	if s.app.ShareManager() == nil {
		return []ShareLogEntry{}
	}
	out := s.app.ShareManager().GetShareLogs(followID, publicationID)
	if out == nil {
		out = []ShareLogEntry{}
	}
	return out
}

// UpdateFollowTag changes the local tag used for incoming clips on an existing
// follow. Past clips are not re-tagged.
func (s *ShareService) UpdateFollowTag(followID int64, newLocalTagName string) (FollowInfo, error) {
	if s.app.ShareManager() == nil {
		return FollowInfo{}, fmt.Errorf("share manager not initialized")
	}
	return s.app.ShareManager().UpdateFollowTag(followID, newLocalTagName)
}

type ShareStatus struct {
	Shares  []ShareInfo  `json:"shares"`
	Follows []FollowInfo `json:"follows"`
}

func (s *ShareService) GetShareStatus() ShareStatus {
	if s.app.ShareManager() == nil {
		return ShareStatus{Shares: []ShareInfo{}, Follows: []FollowInfo{}}
	}
	ss, ff := s.app.ShareManager().GetShareStatus()
	if ss == nil {
		ss = []ShareInfo{}
	}
	if ff == nil {
		ff = []FollowInfo{}
	}
	return ShareStatus{Shares: ss, Follows: ff}
}

// AgeShareRingForTest shifts every share_ring row's `ts` column back by the
// given number of seconds. Used by e2e tests that simulate TTL expiry without
// waiting a real hour. Does nothing if shareManager is not initialized.
//
// test-only — do not call from production code paths.
func (s *ShareService) AgeShareRingForTest(seconds int64) error {
	if s.app.ShareManager() == nil {
		return fmt.Errorf("share manager not initialized")
	}
	return s.app.ShareManager().AgeShareRingForTest(seconds)
}
