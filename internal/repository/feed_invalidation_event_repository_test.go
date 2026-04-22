package repository

import (
	"errors"
	"testing"
)

type fakeRedisError string

func (e fakeRedisError) Error() string { return string(e) }
func (e fakeRedisError) RedisError()   {}

func TestIsBusyGroupError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     error
		wantHit bool
	}{
		{
			name:    "nil error",
			err:     nil,
			wantHit: false,
		},
		{
			name:    "busy group redis error",
			err:     fakeRedisError("BUSYGROUP Consumer Group name already exists"),
			wantHit: true,
		},
		{
			name:    "non busy redis error",
			err:     fakeRedisError("NOGROUP No such key"),
			wantHit: false,
		},
		{
			name:    "normal go error",
			err:     errors.New("random error"),
			wantHit: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := isBusyGroupError(tc.err); got != tc.wantHit {
				t.Fatalf("unexpected busy-group detection: got=%v want=%v err=%v", got, tc.wantHit, tc.err)
			}
		})
	}
}

func TestDecodeFeedInvalidationEvent(t *testing.T) {
	t.Parallel()

	validJSON := `{"type":"post_created","author_id":1001,"occurred_at":1700000000}`

	tests := []struct {
		name     string
		payload  any
		wantOK   bool
		wantType string
		wantID   int64
	}{
		{
			name:     "string payload",
			payload:  validJSON,
			wantOK:   true,
			wantType: "post_created",
			wantID:   1001,
		},
		{
			name:     "byte payload",
			payload:  []byte(validJSON),
			wantOK:   true,
			wantType: "post_created",
			wantID:   1001,
		},
		{
			name:    "invalid json",
			payload: `{"type":"post_created"`,
			wantOK:  false,
		},
		{
			name:    "unsupported payload type",
			payload: 12345,
			wantOK:  false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := decodeFeedInvalidationEvent(tc.payload)
			if ok != tc.wantOK {
				t.Fatalf("unexpected decode result: got=%v want=%v payload=%v", ok, tc.wantOK, tc.payload)
			}
			if !tc.wantOK {
				return
			}
			if got.Type != tc.wantType || got.AuthorID != tc.wantID {
				t.Fatalf("unexpected event: got=%+v", got)
			}
		})
	}
}
