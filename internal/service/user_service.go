package service

import (
	"context"
	"time"

	"github.com/plion676/feed-flow/internal/model"
	"github.com/plion676/feed-flow/internal/pkg/xerror"
)

type userReadRepository interface {
	GetByID(ctx context.Context, userID int64) (*model.User, error)
	GetByIDs(ctx context.Context, userIDs []int64) ([]*model.User, error)
}

type userPostReadRepository interface {
	ListByUserID(ctx context.Context, userID int64, lastPostID int64, limit int) ([]*model.Post, error)
	CountPublishedByUserID(ctx context.Context, userID int64) (int64, error)
}

type userFollowReadRepository interface {
	CountFollowing(ctx context.Context, userID int64) (int64, error)
	CountFollowers(ctx context.Context, targetUserID int64) (int64, error)
	Exists(ctx context.Context, userID int64, targetUserID int64) (bool, error)
	ListFollowingUserIDs(ctx context.Context, userID int64) ([]int64, error)
	ListFollowerUserIDs(ctx context.Context, targetUserID int64) ([]int64, error)
	ListFollowingRelations(ctx context.Context, userID int64, lastFollowID int64, limit int) ([]*model.Follow, error)
	ListFollowerRelations(ctx context.Context, targetUserID int64, lastFollowID int64, limit int) ([]*model.Follow, error)
}

type userCountReadRepository interface {
	GetByUserID(ctx context.Context, userID int64) (*model.UserCount, error)
}

// UserService handles user profile related read operations.
type UserService struct {
	userRepo      userReadRepository
	postRepo      userPostReadRepository
	followRepo    userFollowReadRepository
	userCountRepo userCountReadRepository
}

type MeResult struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	Bio      string `json:"bio"`
}

const (
	defaultUserPostsLimit      = 20
	maxUserPostsLimit          = 50
	defaultUserFollowListLimit = 20
	maxUserFollowListLimit     = 50
)

type GetUserPostsRequest struct {
	UserID     int64
	LastPostID int64
	Limit      int
}

type UserPostItem struct {
	PostID    int64     `json:"post_id"`
	UserID    int64     `json:"user_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type UserPostsResult struct {
	Items      []UserPostItem `json:"items"`
	NextCursor int64          `json:"next_cursor"`
	HasMore    bool           `json:"has_more"`
}

type UserProfileResult struct {
	UserID         int64  `json:"user_id"`
	Username       string `json:"username"`
	Nickname       string `json:"nickname"`
	Avatar         string `json:"avatar"`
	Bio            string `json:"bio"`
	FollowingCount int64  `json:"following_count"`
	FollowerCount  int64  `json:"follower_count"`
	PostCount      int64  `json:"post_count"`
	IsFollowing    bool   `json:"is_following"`
}

type UserFollowListRequest struct {
	UserID       int64
	ViewerUserID int64
	LastFollowID int64
	Limit        int
}

type UserFollowListItem struct {
	UserID      int64  `json:"user_id"`
	Username    string `json:"username"`
	Nickname    string `json:"nickname"`
	Avatar      string `json:"avatar"`
	Bio         string `json:"bio"`
	IsFollowing bool   `json:"is_following"`
}

type UserFollowListResult struct {
	Items      []UserFollowListItem `json:"items"`
	NextCursor int64                `json:"next_cursor"`
	HasMore    bool                 `json:"has_more"`
}

func NewUserService(userRepo userReadRepository) *UserService {
	return &UserService{userRepo: userRepo}
}

func (s *UserService) WithPostRepository(postRepo userPostReadRepository) *UserService {
	s.postRepo = postRepo
	return s
}

func (s *UserService) WithFollowRepository(followRepo userFollowReadRepository) *UserService {
	s.followRepo = followRepo
	return s
}

func (s *UserService) WithUserCountRepository(userCountRepo userCountReadRepository) *UserService {
	s.userCountRepo = userCountRepo
	return s
}

func (s *UserService) GetMe(ctx context.Context, userID int64) (*MeResult, *xerror.Error) {
	if userID <= 0 {
		return nil, xerror.ErrUnauthorized
	}

	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, xerror.ErrInternal
	}
	if user == nil {
		return nil, xerror.ErrUnauthorized
	}

	return &MeResult{
		UserID:   user.ID,
		Username: user.Username,
		Nickname: user.Nickname,
		Avatar:   user.Avatar,
		Bio:      user.Bio,
	}, nil
}

type GetUserProfileRequest struct {
	UserID       int64
	ViewerUserID int64
}

func (s *UserService) GetUserProfile(ctx context.Context, req GetUserProfileRequest) (*UserProfileResult, *xerror.Error) {
	if req.UserID <= 0 || req.ViewerUserID < 0 {
		return nil, xerror.ErrBadRequest
	}
	if s.postRepo == nil || s.followRepo == nil {
		return nil, xerror.ErrInternal
	}

	user, err := s.userRepo.GetByID(ctx, req.UserID)
	if err != nil {
		return nil, xerror.ErrInternal
	}
	if user == nil {
		return nil, xerror.ErrNotFound
	}

	followingCount, followerCount, postCount, err := s.resolveUserProfileCounts(ctx, req.UserID)
	if err != nil {
		return nil, xerror.ErrInternal
	}

	isFollowing := false
	if req.ViewerUserID > 0 && req.ViewerUserID != req.UserID {
		isFollowing, err = s.followRepo.Exists(ctx, req.ViewerUserID, req.UserID)
		if err != nil {
			return nil, xerror.ErrInternal
		}
	}

	return &UserProfileResult{
		UserID:         user.ID,
		Username:       user.Username,
		Nickname:       user.Nickname,
		Avatar:         user.Avatar,
		Bio:            user.Bio,
		FollowingCount: followingCount,
		FollowerCount:  followerCount,
		PostCount:      postCount,
		IsFollowing:    isFollowing,
	}, nil
}

func (s *UserService) resolveUserProfileCounts(ctx context.Context, userID int64) (int64, int64, int64, error) {
	if s.userCountRepo != nil {
		userCount, err := s.userCountRepo.GetByUserID(ctx, userID)
		if err != nil {
			return 0, 0, 0, err
		}
		if userCount != nil {
			return userCount.FollowingCount, userCount.FollowerCount, userCount.PostCount, nil
		}
	}

	followingCount, err := s.followRepo.CountFollowing(ctx, userID)
	if err != nil {
		return 0, 0, 0, err
	}

	followerCount, err := s.followRepo.CountFollowers(ctx, userID)
	if err != nil {
		return 0, 0, 0, err
	}

	postCount, err := s.postRepo.CountPublishedByUserID(ctx, userID)
	if err != nil {
		return 0, 0, 0, err
	}

	return followingCount, followerCount, postCount, nil
}

func (s *UserService) GetUserPosts(ctx context.Context, req GetUserPostsRequest) (*UserPostsResult, *xerror.Error) {
	if req.UserID <= 0 || req.LastPostID < 0 {
		return nil, xerror.ErrBadRequest
	}
	if s.postRepo == nil {
		return nil, xerror.ErrInternal
	}

	limit := req.Limit
	if limit <= 0 {
		limit = defaultUserPostsLimit
	}
	if limit > maxUserPostsLimit {
		limit = maxUserPostsLimit
	}

	user, err := s.userRepo.GetByID(ctx, req.UserID)
	if err != nil {
		return nil, xerror.ErrInternal
	}
	if user == nil {
		return nil, xerror.ErrNotFound
	}

	posts, err := s.postRepo.ListByUserID(ctx, req.UserID, req.LastPostID, limit+1)
	if err != nil {
		return nil, xerror.ErrInternal
	}

	hasMore := len(posts) > limit
	if hasMore {
		posts = posts[:limit]
	}

	items := make([]UserPostItem, 0, len(posts))
	for _, post := range posts {
		if post == nil {
			continue
		}
		items = append(items, UserPostItem{
			PostID:    post.ID,
			UserID:    post.UserID,
			Content:   post.Content,
			CreatedAt: post.CreatedAt,
		})
	}

	var nextCursor int64
	if len(items) > 0 {
		nextCursor = items[len(items)-1].PostID
	}

	return &UserPostsResult{
		Items:      items,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

func (s *UserService) GetUserFollowing(ctx context.Context, req UserFollowListRequest) (*UserFollowListResult, *xerror.Error) {
	return s.getUserFollowList(
		ctx,
		req,
		func(ctx context.Context, userID int64, lastFollowID int64, limit int) ([]*model.Follow, error) {
			return s.followRepo.ListFollowingRelations(ctx, userID, lastFollowID, limit)
		},
		func(follow *model.Follow) int64 {
			if follow == nil {
				return 0
			}
			return follow.TargetUserID
		},
	)
}

func (s *UserService) GetUserFollowers(ctx context.Context, req UserFollowListRequest) (*UserFollowListResult, *xerror.Error) {
	return s.getUserFollowList(
		ctx,
		req,
		func(ctx context.Context, userID int64, lastFollowID int64, limit int) ([]*model.Follow, error) {
			return s.followRepo.ListFollowerRelations(ctx, userID, lastFollowID, limit)
		},
		func(follow *model.Follow) int64 {
			if follow == nil {
				return 0
			}
			return follow.UserID
		},
	)
}

func (s *UserService) getUserFollowList(
	ctx context.Context,
	req UserFollowListRequest,
	loadRelations func(ctx context.Context, userID int64, lastFollowID int64, limit int) ([]*model.Follow, error),
	extractUserID func(follow *model.Follow) int64,
) (*UserFollowListResult, *xerror.Error) {
	if req.UserID <= 0 || req.ViewerUserID < 0 || req.LastFollowID < 0 {
		return nil, xerror.ErrBadRequest
	}
	if s.followRepo == nil {
		return nil, xerror.ErrInternal
	}

	limit := req.Limit
	if limit <= 0 {
		limit = defaultUserFollowListLimit
	}
	if limit > maxUserFollowListLimit {
		limit = maxUserFollowListLimit
	}

	user, err := s.userRepo.GetByID(ctx, req.UserID)
	if err != nil {
		return nil, xerror.ErrInternal
	}
	if user == nil {
		return nil, xerror.ErrNotFound
	}

	relations, err := loadRelations(ctx, req.UserID, req.LastFollowID, limit+1)
	if err != nil {
		return nil, xerror.ErrInternal
	}

	hasMore := len(relations) > limit
	if hasMore {
		relations = relations[:limit]
	}

	if len(relations) == 0 {
		return &UserFollowListResult{
			Items:      []UserFollowListItem{},
			NextCursor: 0,
			HasMore:    false,
		}, nil
	}

	userIDs := make([]int64, 0, len(relations))
	for _, relation := range relations {
		userID := extractUserID(relation)
		if userID <= 0 {
			continue
		}
		userIDs = append(userIDs, userID)
	}

	users, err := s.userRepo.GetByIDs(ctx, userIDs)
	if err != nil {
		return nil, xerror.ErrInternal
	}

	usersByID := make(map[int64]*model.User, len(users))
	for _, item := range users {
		if item == nil {
			continue
		}
		usersByID[item.ID] = item
	}

	items := make([]UserFollowListItem, 0, len(relations))
	for _, relation := range relations {
		userID := extractUserID(relation)
		if userID <= 0 {
			continue
		}
		item := usersByID[userID]
		if item == nil {
			continue
		}
		isFollowing := false
		if req.ViewerUserID > 0 && req.ViewerUserID != userID {
			// TODO(user): batch follow-state lookup if this list becomes a hot path.
			isFollowing, err = s.followRepo.Exists(ctx, req.ViewerUserID, userID)
			if err != nil {
				return nil, xerror.ErrInternal
			}
		}
		items = append(items, UserFollowListItem{
			UserID:      item.ID,
			Username:    item.Username,
			Nickname:    item.Nickname,
			Avatar:      item.Avatar,
			Bio:         item.Bio,
			IsFollowing: isFollowing,
		})
	}

	var nextCursor int64
	if len(relations) > 0 {
		nextCursor = relations[len(relations)-1].ID
	}

	return &UserFollowListResult{
		Items:      items,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}
