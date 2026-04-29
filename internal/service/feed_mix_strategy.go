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

type feedMixPageResult struct {
	visible             []*model.Post
	probe               *model.Post
	hasMore             bool
	nextInboxCursor     int64
	nextPullCursor      int64
	inboxPendingPostIDs []int64
	pullPendingPostIDs  []int64
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
	page := defaultFeedMixPolicy().mixPageForCursor(inboxPosts, pullPosts, pageLimit, 0, 0)
	result := append([]*model.Post{}, page.visible...)
	if page.probe != nil {
		result = append(result, page.probe)
	}
	return result
}

func mixFeedPageForCursor(
	inboxPosts []*model.Post,
	pullPosts []*model.Post,
	pageLimit int,
	inboxCursor int64,
	pullCursor int64,
) feedMixPageResult {
	return defaultFeedMixPolicy().mixPageForCursor(inboxPosts, pullPosts, pageLimit, inboxCursor, pullCursor)
}

func (p feedMixPolicy) mixPageForCursor(
	inboxPosts []*model.Post,
	pullPosts []*model.Post,
	pageLimit int,
	inboxCursor int64,
	pullCursor int64,
) feedMixPageResult {
	if pageLimit <= 0 {
		return feedMixPageResult{}
	}

	inboxCandidates := buildFeedMixCandidates(inboxPosts, feedMixSourceInbox)
	pullCandidates := buildFeedMixCandidates(pullPosts, feedMixSourcePull)
	pushQuota := p.resolvePushQuota(pageLimit, len(pullCandidates))
	minPullItems := p.resolveMinPullItems(pageLimit, len(pullCandidates))
	nextInboxCursor := resolveSourceContinuationCursor(inboxCursor, inboxCandidates)
	nextPullCursor := resolveSourceContinuationCursor(pullCursor, pullCandidates)

	visible := make([]*model.Post, 0, pageLimit)
	seen := make(map[int64]struct{}, pageLimit)
	pushUsed := 0
	pullUsed := 0
	lastAuthorID := int64(0)
	authorStreak := 0

	for len(visible) < pageLimit {
		inboxPick := nextFeedMixPick(inboxCandidates, seen, lastAuthorID, authorStreak, p.maxConsecutiveAuthor)
		pullPick := nextFeedMixPick(pullCandidates, seen, lastAuthorID, authorStreak, p.maxConsecutiveAuthor)
		source, pick, ok := chooseFeedMixPick(
			inboxPick,
			pullPick,
			pushUsed,
			pushQuota,
			pullUsed,
			minPullItems,
			pageLimit-len(visible),
			lastAuthorID,
			authorStreak,
			p.maxConsecutiveAuthor,
		)
		if !ok {
			break
		}

		visible = append(visible, pick.candidate.post)
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

	probe, hasMore := p.probeNextPost(inboxCandidates, pullCandidates, seen, lastAuthorID, authorStreak)

	return feedMixPageResult{
		visible:             visible,
		probe:               probe,
		hasMore:             hasMore,
		nextInboxCursor:     nextInboxCursor,
		nextPullCursor:      nextPullCursor,
		inboxPendingPostIDs: collectFeedMixPendingIDs(inboxCandidates, seen),
		pullPendingPostIDs:  collectFeedMixPendingIDs(pullCandidates, seen),
	}
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

func (p feedMixPolicy) probeNextPost(
	inboxCandidates []feedMixCandidate,
	pullCandidates []feedMixCandidate,
	seen map[int64]struct{},
	lastAuthorID int64,
	authorStreak int,
) (*model.Post, bool) {
	inboxPick := nextFeedMixPick(inboxCandidates, seen, lastAuthorID, authorStreak, p.maxConsecutiveAuthor)
	pullPick := nextFeedMixPick(pullCandidates, seen, lastAuthorID, authorStreak, p.maxConsecutiveAuthor)
	_, pick, ok := chooseFeedMixPickByRecency(
		inboxPick,
		pullPick,
		lastAuthorID,
		authorStreak,
		p.maxConsecutiveAuthor,
	)
	if !ok {
		return nil, false
	}
	return pick.candidate.post, true
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

func collectFeedMixPendingIDs(candidates []feedMixCandidate, seen map[int64]struct{}) []int64 {
	if len(candidates) == 0 {
		return nil
	}

	pending := make([]int64, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.post == nil || candidate.post.ID <= 0 {
			continue
		}
		if _, ok := seen[candidate.post.ID]; ok {
			continue
		}
		pending = append(pending, candidate.post.ID)
	}
	return pending
}

func resolveSourceContinuationCursor(currentCursor int64, candidates []feedMixCandidate) int64 {
	if len(candidates) == 0 {
		return currentCursor
	}
	last := candidates[len(candidates)-1]
	if last.post == nil || last.post.ID <= 0 {
		return currentCursor
	}
	return last.post.ID
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
