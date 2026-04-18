package main

import "fmt"

// DisconnectFollowForTest cancels the active session for the given follow ID
// so the follow-loop re-enters its backoff → reconnect path. The follow row,
// last_seq, and follow-lifetime context are preserved (unlike Unfollow).
//
// test-only — do not call from production code paths.
func (s *ShareService) DisconnectFollowForTest(id int64) error {
	if s.app.shareManager == nil {
		return fmt.Errorf("share manager not initialized")
	}
	return s.app.shareManager.DisconnectFollowForTest(id)
}

// ShareService exposes share operations to the frontend via Wails.
type ShareService struct {
	app *App
}

func NewShareService(app *App) *ShareService { return &ShareService{app: app} }

func (s *ShareService) StartShare(tagID int64) (ShareInfo, error) {
	if s.app.shareManager == nil {
		return ShareInfo{}, fmt.Errorf("share manager not initialized")
	}
	return s.app.shareManager.StartShare(tagID)
}

func (s *ShareService) StopShare(tagID int64) error {
	if s.app.shareManager == nil {
		return fmt.Errorf("share manager not initialized")
	}
	return s.app.shareManager.StopShare(tagID)
}

func (s *ShareService) Follow(shareString, localTagName string) (FollowInfo, error) {
	if s.app.shareManager == nil {
		return FollowInfo{}, fmt.Errorf("share manager not initialized")
	}
	return s.app.shareManager.Follow(shareString, localTagName)
}

func (s *ShareService) Unfollow(followID int64) error {
	if s.app.shareManager == nil {
		return fmt.Errorf("share manager not initialized")
	}
	return s.app.shareManager.Unfollow(followID)
}

type ShareStatus struct {
	Shares  []ShareInfo  `json:"shares"`
	Follows []FollowInfo `json:"follows"`
}

func (s *ShareService) GetShareStatus() ShareStatus {
	if s.app.shareManager == nil {
		return ShareStatus{Shares: []ShareInfo{}, Follows: []FollowInfo{}}
	}
	ss, ff := s.app.shareManager.GetShareStatus()
	if ss == nil {
		ss = []ShareInfo{}
	}
	if ff == nil {
		ff = []FollowInfo{}
	}
	return ShareStatus{Shares: ss, Follows: ff}
}
