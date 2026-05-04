export type ApiEnvelope<T> = {
  code: number
  message: string
  data?: T
  request_id?: string
}

export type RegisterRequest = {
  username: string
  password: string
  nickname: string
}

export type RegisterResponse = {
  user_id: number
  username: string
  nickname: string
}

export type LoginRequest = {
  username: string
  password: string
}

export type LoginResponse = {
  token: string
  user_id: number
  username: string
  nickname: string
}

export type MeResponse = {
  user_id: number
  username: string
  nickname: string
  avatar: string
  bio: string
}

export type UserProfileResponse = {
  user_id: number
  username: string
  nickname: string
  avatar: string
  bio: string
  following_count: number
  follower_count: number
  post_count: number
  is_following: boolean
}

export type UserFollowListItem = {
  user_id: number
  username: string
  nickname: string
  avatar: string
  bio: string
  is_following: boolean
}

export type UserFollowListResponse = {
  items: UserFollowListItem[]
  next_cursor: number
  has_more: boolean
}

export type CreatePostRequest = {
  content: string
}

export type PostResponse = {
  post_id: number
  user_id: number
  content: string
  created_at: string
}

export type FeedItem = {
  post_id: number
  user_id: number
  content: string
  created_at: string
}

export type FeedResponse = {
  items: FeedItem[]
  next_cursor: number
  next_cursor_token?: string
  has_more: boolean
  fallback_mode?: string
}

export type UserPostsResponse = {
  items: FeedItem[]
  next_cursor: number
  has_more: boolean
}

export type PostInteraction = {
  post_id: number
  liked: boolean
  collected: boolean
  like_count: number
  collect_count: number
  comment_count: number
}

export type PostInteractionStatusResponse = {
  items: PostInteraction[]
}

export type CommentResponse = {
  comment_id: number
  post_id: number
  user_id: number
  content: string
  created_at: string
}

export type CommentListResponse = {
  items: CommentResponse[]
  next_cursor: number
  has_more: boolean
}
