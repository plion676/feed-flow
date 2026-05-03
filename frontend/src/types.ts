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
}
