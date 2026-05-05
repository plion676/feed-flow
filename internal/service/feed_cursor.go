package service

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

const (
	feedCursorTokenVersion = 1
	feedCursorModeHybrid   = "hybrid"
)

type feedReadCursor struct {
	InboxLastPostID int64
	PullLastPostID  int64
	InboxPendingIDs []int64
	PullPendingIDs  []int64
	RecentAuthorIDs []int64
}

type feedCursorToken struct {
	Version         int     `json:"v"`
	Mode            string  `json:"mode"`
	InboxLastPostID int64   `json:"inbox_last_post_id,omitempty"`
	PullLastPostID  int64   `json:"pull_last_post_id,omitempty"`
	InboxPendingIDs []int64 `json:"inbox_pending_ids,omitempty"`
	PullPendingIDs  []int64 `json:"pull_pending_ids,omitempty"`
	RecentAuthorIDs []int64 `json:"recent_author_ids,omitempty"`
}

func encodeFeedCursorToken(cursor feedReadCursor) (string, error) {
	for _, authorID := range cursor.RecentAuthorIDs {
		if authorID <= 0 {
			return "", fmt.Errorf("feed recent author ids must be positive")
		}
	}

	token := feedCursorToken{
		Version:         feedCursorTokenVersion,
		Mode:            feedCursorModeHybrid,
		InboxLastPostID: cursor.InboxLastPostID,
		PullLastPostID:  cursor.PullLastPostID,
		InboxPendingIDs: append([]int64(nil), cursor.InboxPendingIDs...),
		PullPendingIDs:  append([]int64(nil), cursor.PullPendingIDs...),
		RecentAuthorIDs: append([]int64(nil), cursor.RecentAuthorIDs...),
	}

	payload, err := json.Marshal(token)
	if err != nil {
		return "", fmt.Errorf("marshal feed cursor token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeFeedCursorToken(raw string) (feedReadCursor, error) {
	if raw == "" {
		return feedReadCursor{}, fmt.Errorf("cursor token is empty")
	}

	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return feedReadCursor{}, fmt.Errorf("decode feed cursor token: %w", err)
	}

	var token feedCursorToken
	if err := json.Unmarshal(payload, &token); err != nil {
		return feedReadCursor{}, fmt.Errorf("unmarshal feed cursor token: %w", err)
	}
	if token.Version != feedCursorTokenVersion {
		return feedReadCursor{}, fmt.Errorf("unsupported feed cursor version=%d", token.Version)
	}
	if token.Mode != feedCursorModeHybrid {
		return feedReadCursor{}, fmt.Errorf("unsupported feed cursor mode=%q", token.Mode)
	}
	if token.InboxLastPostID < 0 || token.PullLastPostID < 0 {
		return feedReadCursor{}, fmt.Errorf("feed cursor ids must be non-negative")
	}
	for _, postID := range token.InboxPendingIDs {
		if postID <= 0 {
			return feedReadCursor{}, fmt.Errorf("feed inbox pending ids must be positive")
		}
	}
	for _, postID := range token.PullPendingIDs {
		if postID <= 0 {
			return feedReadCursor{}, fmt.Errorf("feed pull pending ids must be positive")
		}
	}
	for _, authorID := range token.RecentAuthorIDs {
		if authorID <= 0 {
			return feedReadCursor{}, fmt.Errorf("feed recent author ids must be positive")
		}
	}
	if token.InboxLastPostID == 0 &&
		token.PullLastPostID == 0 &&
		len(token.InboxPendingIDs) == 0 &&
		len(token.PullPendingIDs) == 0 {
		return feedReadCursor{}, fmt.Errorf("feed cursor token must contain at least one continuation field")
	}

	return feedReadCursor{
		InboxLastPostID: token.InboxLastPostID,
		PullLastPostID:  token.PullLastPostID,
		InboxPendingIDs: append([]int64(nil), token.InboxPendingIDs...),
		PullPendingIDs:  append([]int64(nil), token.PullPendingIDs...),
		RecentAuthorIDs: append([]int64(nil), token.RecentAuthorIDs...),
	}, nil
}
