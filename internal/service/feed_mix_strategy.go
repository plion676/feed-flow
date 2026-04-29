package service

import (
	"sort"

	"github.com/plion676/feed-flow/internal/model"
)

const (
	defaultFeedMixPushRatioNumerator   = 2
	defaultFeedMixPushRatioDenominator = 3
	defaultFeedMixMinPullItems         = 1
	defaultFeedMixMaxConsecutiveAuthor = 2
)

type feedMixSource string

const (
	feedMixSourceInbox feedMixSource = "inbox"
	feedMixSourcePull  feedMixSource = "pull"
)

type feedMixPolicy struct {
	pushRatioNumerator   int
	pushRatioDenominator int
	minPullItems         int
	maxConsecutiveAuthor int
}

type feedMixCandidate struct {
	post   *model.Post
	source feedMixSource
}

type feedMixPick struct {
	index           int
	candidate       feedMixCandidate
	ok              bool
	respectsScatter bool
}

func defaultFeedMixPolicy() feedMixPolicy {
	// TODO(user): tune mix policy by actual business goals
	// (push quota, pull reserve, and author scatter window).
	return feedMixPolicy{
		pushRatioNumerator:   defaultFeedMixPushRatioNumerator,
		pushRatioDenominator: defaultFeedMixPushRatioDenominator,
		minPullItems:         defaultFeedMixMinPullItems,
		maxConsecutiveAuthor: defaultFeedMixMaxConsecutiveAuthor,
	}
}

func mixFeedPostsForPage(inboxPosts []*model.Post, pullPosts []*model.Post, pageLimit int) []*model.Post {
	if pageLimit <= 0 {
		return []*model.Post{}
	}
	return defaultFeedMixPolicy().mixForPage(inboxPosts, pullPosts, pageLimit)
}

func (p feedMixPolicy) mixForPage(inboxPosts []*model.Post, pullPosts []*model.Post, pageLimit int) []*model.Post {
	if pageLimit <= 0 {
		return []*model.Post{}
	}

	resultLimit := pageLimit + 1
	inboxCandidates := buildFeedMixCandidates(inboxPosts, feedMixSourceInbox)
	pullCandidates := buildFeedMixCandidates(pullPosts, feedMixSourcePull)
	if len(inboxCandidates) == 0 {
		return takeFeedMixPosts(pullCandidates, resultLimit)
	}
	if len(pullCandidates) == 0 {
		return takeFeedMixPosts(inboxCandidates, resultLimit)
	}

	pushQuota := p.resolvePushQuota(pageLimit, len(pullCandidates))
	minPullItems := p.resolveMinPullItems(pageLimit, len(pullCandidates))
	result := make([]*model.Post, 0, resultLimit)
	seen := make(map[int64]struct{}, resultLimit)
	pushUsed := 0
	pullUsed := 0
	lastAuthorID := int64(0)
	authorStreak := 0

	for len(result) < pageLimit {
		inboxPick := nextFeedMixPick(inboxCandidates, seen, lastAuthorID, authorStreak, p.maxConsecutiveAuthor)
		pullPick := nextFeedMixPick(pullCandidates, seen, lastAuthorID, authorStreak, p.maxConsecutiveAuthor)
		source, pick, ok := chooseFeedMixPick(
			inboxPick,
			pullPick,
			pushUsed,
			pushQuota,
			pullUsed,
			minPullItems,
			pageLimit-len(result),
			lastAuthorID,
			authorStreak,
			p.maxConsecutiveAuthor,
		)
		if !ok {
			break
		}

		result = append(result, pick.candidate.post)
		seen[pick.candidate.post.ID] = struct{}{}
		if pick.candidate.post.UserID == lastAuthorID {
			authorStreak++
		} else {
			lastAuthorID = pick.candidate.post.UserID
			authorStreak = 1
		}

		switch source {
		case feedMixSourceInbox:
			pushUsed++
			inboxCandidates = removeFeedMixCandidate(inboxCandidates, pick.index)
		case feedMixSourcePull:
			pullUsed++
			pullCandidates = removeFeedMixCandidate(pullCandidates, pick.index)
		}
	}

	for len(result) < resultLimit {
		inboxPick := nextFeedMixPick(inboxCandidates, seen, lastAuthorID, authorStreak, p.maxConsecutiveAuthor)
		pullPick := nextFeedMixPick(pullCandidates, seen, lastAuthorID, authorStreak, p.maxConsecutiveAuthor)
		source, pick, ok := chooseFeedMixPickByRecency(inboxPick, pullPick, lastAuthorID, authorStreak, p.maxConsecutiveAuthor)
		if !ok {
			break
		}

		result = append(result, pick.candidate.post)
		seen[pick.candidate.post.ID] = struct{}{}
		if pick.candidate.post.UserID == lastAuthorID {
			authorStreak++
		} else {
			lastAuthorID = pick.candidate.post.UserID
			authorStreak = 1
		}

		switch source {
		case feedMixSourceInbox:
			inboxCandidates = removeFeedMixCandidate(inboxCandidates, pick.index)
		case feedMixSourcePull:
			pullCandidates = removeFeedMixCandidate(pullCandidates, pick.index)
		}
	}

	return result
}

func buildFeedMixCandidates(posts []*model.Post, source feedMixSource) []feedMixCandidate {
	if len(posts) == 0 {
		return nil
	}

	seen := make(map[int64]struct{}, len(posts))
	candidates := make([]feedMixCandidate, 0, len(posts))
	for _, post := range posts {
		if post == nil || post.ID <= 0 {
			continue
		}
		if _, ok := seen[post.ID]; ok {
			continue
		}
		seen[post.ID] = struct{}{}
		candidates = append(candidates, feedMixCandidate{
			post:   post,
			source: source,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].post.ID > candidates[j].post.ID
	})
	return candidates
}

func takeFeedMixPosts(candidates []feedMixCandidate, limit int) []*model.Post {
	if limit <= 0 || len(candidates) == 0 {
		return []*model.Post{}
	}
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	result := make([]*model.Post, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, candidate.post)
	}
	return result
}

func nextFeedMixPick(
	candidates []feedMixCandidate,
	seen map[int64]struct{},
	lastAuthorID int64,
	authorStreak int,
	maxConsecutiveAuthor int,
) feedMixPick {
	preferDifferentAuthor := shouldAvoidSameAuthor(lastAuthorID, authorStreak, maxConsecutiveAuthor)
	fallback := feedMixPick{}

	for i, candidate := range candidates {
		if candidate.post == nil || candidate.post.ID <= 0 {
			continue
		}
		if _, ok := seen[candidate.post.ID]; ok {
			continue
		}
		if !preferDifferentAuthor {
			return feedMixPick{
				index:           i,
				candidate:       candidate,
				ok:              true,
				respectsScatter: true,
			}
		}
		if candidate.post.UserID != lastAuthorID {
			return feedMixPick{
				index:           i,
				candidate:       candidate,
				ok:              true,
				respectsScatter: true,
			}
		}
		if !fallback.ok {
			fallback = feedMixPick{
				index:           i,
				candidate:       candidate,
				ok:              true,
				respectsScatter: false,
			}
		}
	}
	return fallback
}

func chooseFeedMixPick(
	inboxPick feedMixPick,
	pullPick feedMixPick,
	pushUsed int,
	pushQuota int,
	pullUsed int,
	minPullItems int,
	remainingCoreSlots int,
	lastAuthorID int64,
	authorStreak int,
	maxConsecutiveAuthor int,
) (feedMixSource, feedMixPick, bool) {
	if !inboxPick.ok && !pullPick.ok {
		return "", feedMixPick{}, false
	}
	if !inboxPick.ok {
		return feedMixSourcePull, pullPick, true
	}
	if !pullPick.ok {
		return feedMixSourceInbox, inboxPick, true
	}

	pullNeeded := minPullItems - pullUsed
	if pullNeeded < 0 {
		pullNeeded = 0
	}
	if pullNeeded > 0 && remainingCoreSlots <= pullNeeded {
		return feedMixSourcePull, pullPick, true
	}
	if pushUsed >= pushQuota {
		return feedMixSourcePull, pullPick, true
	}

	if shouldAvoidSameAuthor(lastAuthorID, authorStreak, maxConsecutiveAuthor) &&
		inboxPick.respectsScatter != pullPick.respectsScatter {
		if inboxPick.respectsScatter {
			return feedMixSourceInbox, inboxPick, true
		}
		return feedMixSourcePull, pullPick, true
	}

	if inboxPick.candidate.post.ID >= pullPick.candidate.post.ID {
		return feedMixSourceInbox, inboxPick, true
	}
	return feedMixSourcePull, pullPick, true
}

func chooseFeedMixPickByRecency(
	inboxPick feedMixPick,
	pullPick feedMixPick,
	lastAuthorID int64,
	authorStreak int,
	maxConsecutiveAuthor int,
) (feedMixSource, feedMixPick, bool) {
	if !inboxPick.ok && !pullPick.ok {
		return "", feedMixPick{}, false
	}
	if !inboxPick.ok {
		return feedMixSourcePull, pullPick, true
	}
	if !pullPick.ok {
		return feedMixSourceInbox, inboxPick, true
	}

	if shouldAvoidSameAuthor(lastAuthorID, authorStreak, maxConsecutiveAuthor) &&
		inboxPick.respectsScatter != pullPick.respectsScatter {
		if inboxPick.respectsScatter {
			return feedMixSourceInbox, inboxPick, true
		}
		return feedMixSourcePull, pullPick, true
	}

	if inboxPick.candidate.post.ID >= pullPick.candidate.post.ID {
		return feedMixSourceInbox, inboxPick, true
	}
	return feedMixSourcePull, pullPick, true
}

func removeFeedMixCandidate(candidates []feedMixCandidate, index int) []feedMixCandidate {
	if index < 0 || index >= len(candidates) {
		return candidates
	}
	return append(candidates[:index], candidates[index+1:]...)
}

func shouldAvoidSameAuthor(lastAuthorID int64, authorStreak int, maxConsecutiveAuthor int) bool {
	return lastAuthorID > 0 && maxConsecutiveAuthor > 0 && authorStreak >= maxConsecutiveAuthor
}

func (p feedMixPolicy) resolvePushQuota(pageLimit int, pullCount int) int {
	if pageLimit <= 0 {
		return 0
	}
	if pullCount <= 0 {
		return pageLimit
	}

	numerator := p.pushRatioNumerator
	denominator := p.pushRatioDenominator
	if numerator <= 0 || denominator <= 0 || numerator > denominator {
		numerator = defaultFeedMixPushRatioNumerator
		denominator = defaultFeedMixPushRatioDenominator
	}

	pushQuota := (pageLimit*numerator + denominator - 1) / denominator
	if pushQuota >= pageLimit && pageLimit > 1 {
		pushQuota = pageLimit - 1
	}
	if pushQuota < 1 {
		pushQuota = 1
	}
	return pushQuota
}

func (p feedMixPolicy) resolveMinPullItems(pageLimit int, pullCount int) int {
	if pageLimit <= 0 || pullCount <= 0 {
		return 0
	}

	minPullItems := p.minPullItems
	if minPullItems <= 0 {
		minPullItems = defaultFeedMixMinPullItems
	}
	if minPullItems > pageLimit {
		minPullItems = pageLimit
	}
	if minPullItems > pullCount {
		minPullItems = pullCount
	}
	return minPullItems
}
