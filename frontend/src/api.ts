import axios, { AxiosError } from 'axios'
import type {
  ApiEnvelope,
  CreatePostRequest,
  FeedResponse,
  CommentListResponse,
  CommentResponse,
  LoginRequest,
  LoginResponse,
  MeResponse,
  PostInteraction,
  PostInteractionStatusResponse,
  PostResponse,
  RegisterRequest,
  RegisterResponse,
  UserPostsResponse,
  UserFollowListResponse,
  UserProfileResponse,
} from './types'

const apiBase = import.meta.env.VITE_API_BASE || '/api/v1'

const client = axios.create({
  baseURL: apiBase,
  timeout: 10000,
})

const authHeader = (token: string | undefined) =>
  token ? { Authorization: `Bearer ${token}` } : {}

export const api = {
  baseURL: apiBase,

  register(payload: RegisterRequest) {
    return client.post<ApiEnvelope<RegisterResponse>>('/auth/register', payload)
  },

  login(payload: LoginRequest) {
    return client.post<ApiEnvelope<LoginResponse>>('/auth/login', payload)
  },

  me(token: string) {
    return client.get<ApiEnvelope<MeResponse>>('/users/me', {
      headers: authHeader(token),
    })
  },

  getUserProfile(userID: number, token?: string) {
    return client.get<ApiEnvelope<UserProfileResponse>>(`/users/${userID}`, {
      headers: authHeader(token),
    })
  },

  getUserPosts(userID: number, options?: { token?: string; limit?: number; lastPostID?: number }) {
    return client.get<ApiEnvelope<UserPostsResponse>>(`/users/${userID}/posts`, {
      headers: authHeader(options?.token),
      params: {
        ...(typeof options?.limit === 'number' ? { limit: options.limit } : {}),
        ...(typeof options?.lastPostID === 'number' ? { last_post_id: options.lastPostID } : {}),
      },
    })
  },

  getUserFollowers(userID: number, options?: { token?: string; limit?: number; lastFollowID?: number }) {
    return client.get<ApiEnvelope<UserFollowListResponse>>(`/users/${userID}/followers`, {
      headers: authHeader(options?.token),
      params: {
        ...(typeof options?.limit === 'number' ? { limit: options.limit } : {}),
        ...(typeof options?.lastFollowID === 'number' ? { last_follow_id: options.lastFollowID } : {}),
      },
    })
  },

  getUserFollowing(userID: number, options?: { token?: string; limit?: number; lastFollowID?: number }) {
    return client.get<ApiEnvelope<UserFollowListResponse>>(`/users/${userID}/following`, {
      headers: authHeader(options?.token),
      params: {
        ...(typeof options?.limit === 'number' ? { limit: options.limit } : {}),
        ...(typeof options?.lastFollowID === 'number' ? { last_follow_id: options.lastFollowID } : {}),
      },
    })
  },

  follow(targetUserID: number, token: string) {
    return client.post<ApiEnvelope<{ followed: boolean }>>(
      `/follows/${targetUserID}`,
      {},
      { headers: authHeader(token) },
    )
  },

  unfollow(targetUserID: number, token: string) {
    return client.delete<ApiEnvelope<{ followed: boolean }>>(`/follows/${targetUserID}`, {
      headers: authHeader(token),
    })
  },

  createPost(payload: CreatePostRequest, token: string) {
    return client.post<ApiEnvelope<PostResponse>>('/posts', payload, {
      headers: authHeader(token),
    })
  },

  deletePost(postID: number, token: string) {
    return client.delete<ApiEnvelope<{ post_id: number; user_id: number; deleted: boolean }>>(
      `/posts/${postID}`,
      {
        headers: authHeader(token),
      },
    )
  },

  likePost(postID: number, token: string) {
    return client.post<ApiEnvelope<PostInteraction>>(`/posts/${postID}/like`, {}, {
      headers: authHeader(token),
    })
  },

  unlikePost(postID: number, token: string) {
    return client.delete<ApiEnvelope<PostInteraction>>(`/posts/${postID}/like`, {
      headers: authHeader(token),
    })
  },

  collectPost(postID: number, token: string) {
    return client.post<ApiEnvelope<PostInteraction>>(`/posts/${postID}/collect`, {}, {
      headers: authHeader(token),
    })
  },

  uncollectPost(postID: number, token: string) {
    return client.delete<ApiEnvelope<PostInteraction>>(`/posts/${postID}/collect`, {
      headers: authHeader(token),
    })
  },

  getPostInteractionStatuses(postIDs: number[], token?: string) {
    return client.get<ApiEnvelope<PostInteractionStatusResponse>>('/posts/interactions/status', {
      headers: authHeader(token),
      params: {
        post_ids: postIDs.join(','),
      },
    })
  },

  getPostComments(postID: number, options?: { limit?: number; lastCommentID?: number }) {
    return client.get<ApiEnvelope<CommentListResponse>>(`/posts/${postID}/comments`, {
      params: {
        ...(typeof options?.limit === 'number' ? { limit: options.limit } : {}),
        ...(typeof options?.lastCommentID === 'number' ? { last_comment_id: options.lastCommentID } : {}),
      },
    })
  },

  createPostComment(postID: number, payload: { content: string }, token: string) {
    return client.post<ApiEnvelope<CommentResponse>>(`/posts/${postID}/comments`, payload, {
      headers: authHeader(token),
    })
  },

  getFeed(token: string, options?: { limit?: number; lastPostID?: number; cursor?: string; refresh?: boolean }) {
    return client.get<ApiEnvelope<FeedResponse>>('/feed', {
      headers: authHeader(token),
      params: {
        ...(typeof options?.limit === 'number' ? { limit: options.limit } : {}),
        ...(typeof options?.lastPostID === 'number' ? { last_post_id: options.lastPostID } : {}),
        ...(options?.cursor ? { cursor: options.cursor } : {}),
        ...(options?.refresh ? { refresh: 1 } : {}),
      },
    })
  },

  getDiscoverFeed(token: string, options?: { limit?: number; lastPostID?: number }) {
    return client.get<ApiEnvelope<FeedResponse>>('/feed/discover', {
      headers: authHeader(token),
      params: {
        ...(typeof options?.limit === 'number' ? { limit: options.limit } : {}),
        ...(typeof options?.lastPostID === 'number' ? { last_post_id: options.lastPostID } : {}),
      },
    })
  },
}

export function explainError(err: unknown): string {
  if (axios.isAxiosError(err)) {
    const axiosErr = err as AxiosError<ApiEnvelope<unknown>>
    const status = axiosErr.response?.status
    const code = axiosErr.response?.data?.code
    const message = axiosErr.response?.data?.message || axiosErr.message
    if (status) {
      return `[HTTP ${status}] code=${code ?? '-'} message=${message}`
    }
    return message
  }
  if (err instanceof Error) {
    return err.message
  }
  return 'unknown error'
}
