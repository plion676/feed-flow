package service

type FeedDeliveryMode string

const (
	FeedDeliveryPullOnly    FeedDeliveryMode = "pull_only"
	FeedDeliveryPushAndPull FeedDeliveryMode = "push_and_pull"
)

// FeedHybridPolicy decides push/pull split by author follower count.
type FeedHybridPolicy struct {
	pushFollowerThreshold int
}

func NewFeedHybridPolicy(pushFollowerThreshold int) *FeedHybridPolicy {
	return &FeedHybridPolicy{pushFollowerThreshold: pushFollowerThreshold}
}

func (p *FeedHybridPolicy) DecideByFollowerCount(followerCount int) FeedDeliveryMode {
	if p == nil || p.pushFollowerThreshold <= 0 {
		return FeedDeliveryPullOnly
	}
	if followerCount <= p.pushFollowerThreshold {
		return FeedDeliveryPushAndPull
	}
	return FeedDeliveryPullOnly
}
