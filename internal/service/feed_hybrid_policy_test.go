package service

import "testing"

func TestFeedHybridPolicyDecideByFollowerCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		threshold     int
		followerCount int
		want          FeedDeliveryMode
	}{
		{
			name:          "threshold zero means pull only",
			threshold:     0,
			followerCount: 1,
			want:          FeedDeliveryPullOnly,
		},
		{
			name:          "follower count equal threshold uses push",
			threshold:     1000,
			followerCount: 1000,
			want:          FeedDeliveryPushAndPull,
		},
		{
			name:          "follower count below threshold uses push",
			threshold:     1000,
			followerCount: 999,
			want:          FeedDeliveryPushAndPull,
		},
		{
			name:          "follower count above threshold uses pull",
			threshold:     1000,
			followerCount: 1001,
			want:          FeedDeliveryPullOnly,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			policy := NewFeedHybridPolicy(tc.threshold)
			if got := policy.DecideByFollowerCount(tc.followerCount); got != tc.want {
				t.Fatalf("unexpected mode: got=%s want=%s", got, tc.want)
			}
		})
	}
}
