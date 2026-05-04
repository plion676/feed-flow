import { useEffect, useMemo, useState, type KeyboardEvent, type MouseEvent } from 'react'
import {
  Avatar,
  Button,
  Drawer,
  Empty,
  Form,
  Input,
  Modal,
  Space,
  Spin,
  Tag,
  Typography,
  message,
} from 'antd'
import {
  EyeOutlined,
  FireOutlined,
  HeartFilled,
  HeartOutlined,
  HomeOutlined,
  MessageOutlined,
  PlusOutlined,
  ReloadOutlined,
  SearchOutlined,
  ShareAltOutlined,
  StarFilled,
  StarOutlined,
  TeamOutlined,
  UserAddOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { api, explainError } from './api'
import type {
  FeedItem,
  FeedResponse,
  CommentListResponse,
  CommentResponse,
  LoginResponse,
  MeResponse,
  PostInteraction,
  UserFollowListResponse,
  UserPostsResponse,
  UserProfileResponse,
} from './types'

const { Text, Title } = Typography

type Session = {
  key: string
  userID: number
  username: string
  nickname: string
  token: string
}

type FeedCard = FeedItem & {
  noteText: string
  heroTitle: string
  coverColor: string
  accentColor: string
  coverHeight: number
  topicTags: string[]
  likeCount: number
  commentCount: number
  collectCount: number
}

type FeedState = {
  items: FeedCard[]
  nextCursor: number
  nextCursorToken?: string
  hasMore: boolean
}

type FeedMode = 'following' | 'discover'
type MobileTab = 'feed' | 'compose' | 'profile'
type FollowListKind = 'followers' | 'following'

type NoteInteractionState = {
  liked: boolean
  collected: boolean
  likeCount: number
  collectCount: number
  commentCount: number
}

type CommentListState = {
  items: CommentResponse[]
  nextCursor: number
  hasMore: boolean
}

type AuthorPostsState = {
  items: FeedCard[]
  nextCursor: number
  hasMore: boolean
}

type FollowListState = {
  kind: FollowListKind
  open: boolean
  items: UserFollowListResponse['items']
  nextCursor: number
  hasMore: boolean
}

const beijingTimeZone = 'Asia/Shanghai'
const defaultFeedLimit = 12
const defaultAuthorPostsLimit = 9
const defaultFollowListLimit = 8
const defaultCommentLimit = 10
const storageKeys = {
  sessions: 'feed-flow-notes:sessions',
  activeSessionKey: 'feed-flow-notes:active-session-key',
  feedMode: 'feed-flow-notes:feed-mode',
}
const emptyFeedState: FeedState = {
  items: [],
  nextCursor: 0,
  hasMore: false,
}
const emptyAuthorPostsState: AuthorPostsState = {
  items: [],
  nextCursor: 0,
  hasMore: false,
}
const emptyFollowListState: FollowListState = {
  kind: 'followers',
  open: false,
  items: [],
  nextCursor: 0,
  hasMore: false,
}
const emptyCommentListState: CommentListState = {
  items: [],
  nextCursor: 0,
  hasMore: false,
}
const coverPalettes = [
  ['#f97316', '#fb7185'],
  ['#0f766e', '#22c55e'],
  ['#0f4c81', '#38bdf8'],
  ['#7c3aed', '#f59e0b'],
  ['#be123c', '#f43f5e'],
  ['#1d4ed8', '#34d399'],
]
const feedTagPool = [
  '信息流',
  '后端开发',
  '工程实践',
  '系统设计',
  '社区产品',
  '刷帖日常',
  '成长记录',
  '性能优化',
]

function formatBeijingDateTime(value: unknown): string {
  let date: Date | null = null
  if (typeof value === 'string') {
    const parsed = new Date(value)
    if (!Number.isNaN(parsed.getTime())) {
      date = parsed
    }
  } else if (typeof value === 'number') {
    const millis = value < 1_000_000_000_000 ? value * 1000 : value
    const parsed = new Date(millis)
    if (!Number.isNaN(parsed.getTime())) {
      date = parsed
    }
  }

  if (!date) {
    return String(value ?? '')
  }

  const parts = new Intl.DateTimeFormat('zh-CN', {
    timeZone: beijingTimeZone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  }).formatToParts(date)

  const pick = (type: Intl.DateTimeFormatPartTypes) =>
    parts.find((item) => item.type === type)?.value ?? ''

  return `${pick('year')}-${pick('month')}-${pick('day')} ${pick('hour')}:${pick('minute')}:${pick('second')}`
}

function buildCoverPalette(postID: number) {
  return coverPalettes[Math.abs(postID) % coverPalettes.length]
}

function buildCardCopy(item: FeedItem): string {
  const compact = item.content.trim().replace(/\s+/g, ' ')
  if (compact.length > 88) {
    return `${compact.slice(0, 88)}...`
  }
  return compact
}

function buildHeroTitle(item: FeedItem): string {
  const compact = item.content.trim().replace(/\s+/g, ' ')
  if (!compact) {
    return '分享一下今天的内容'
  }
  if (compact.length > 30) {
    return `${compact.slice(0, 30)}...`
  }
  return compact
}

function buildTopicTags(item: FeedItem): string[] {
  const inlineTags = Array.from(item.content.matchAll(/#([^\s#]+)/g))
    .map((matched) => `#${matched[1].slice(0, 12)}`)
    .filter(Boolean)

  const generated = [
    `#${feedTagPool[Math.abs(item.post_id) % feedTagPool.length]}`,
    `#${feedTagPool[(Math.abs(item.post_id) + 3) % feedTagPool.length]}`,
    item.content.length > 36 ? '#长文分享' : '#轻量记录',
  ]

  return Array.from(new Set([...inlineTags, ...generated])).slice(0, 3)
}

function buildSeededMetric(seed: number, base: number, span: number, factor: number): number {
  return base + Math.abs((seed * factor) % span)
}

function formatCount(value: number): string {
  if (value >= 10_000) {
    const displayed = value >= 100_000 ? Math.round(value / 10_000) : (value / 10_000).toFixed(1)
    return `${displayed}万`
  }
  if (value >= 1000) {
    return `${(value / 1000).toFixed(1)}k`
  }
  return String(value)
}

function mapFeedItems(items: FeedItem[]): FeedCard[] {
  return items.map((item, index) => {
    const [coverColor, accentColor] = buildCoverPalette(item.post_id)
    const noteText = buildCardCopy(item)
    return {
      ...item,
      noteText,
      heroTitle: buildHeroTitle(item),
      coverColor,
      accentColor,
      coverHeight: 220 + ((item.post_id + index) % 4) * 34,
      topicTags: buildTopicTags(item),
      likeCount: buildSeededMetric(item.post_id, 128, 2600, 37),
      commentCount: buildSeededMetric(item.post_id, 8, 220, 11),
      collectCount: buildSeededMetric(item.post_id, 36, 580, 19),
    }
  })
}

function mergeFeedState(prev: FeedCard[], next: FeedItem[]): FeedCard[] {
  const seen = new Map<number, FeedCard>()
  for (const item of prev) {
    seen.set(item.post_id, item)
  }
  for (const item of mapFeedItems(next)) {
    seen.set(item.post_id, item)
  }
  return Array.from(seen.values()).sort((a, b) => b.post_id - a.post_id)
}

function loadStorageValue<T>(key: string, fallback: T): T {
  if (typeof window === 'undefined') {
    return fallback
  }

  try {
    const raw = window.localStorage.getItem(key)
    if (!raw) {
      return fallback
    }
    return JSON.parse(raw) as T
  } catch {
    return fallback
  }
}

function App() {
  const [sessions, setSessions] = useState<Session[]>(() =>
    loadStorageValue<Session[]>(storageKeys.sessions, []),
  )
  const [activeSessionKey, setActiveSessionKey] = useState<string | undefined>(() =>
    loadStorageValue<string | undefined>(storageKeys.activeSessionKey, undefined),
  )
  const [feedState, setFeedState] = useState<FeedState>(emptyFeedState)
  const [feedMode, setFeedMode] = useState<FeedMode>(() =>
    loadStorageValue<FeedMode>(storageKeys.feedMode, 'following'),
  )
  const [authorSearchText, setAuthorSearchText] = useState('')
  const [loadingFeed, setLoadingFeed] = useState(false)
  const [busyAction, setBusyAction] = useState(false)
  const [showAuthModal, setShowAuthModal] = useState(false)
  const [showCreateDrawer, setShowCreateDrawer] = useState(false)
  const [selectedPostID, setSelectedPostID] = useState<number>()
  const [selectedAuthorID, setSelectedAuthorID] = useState<number>()
  const [postInteractions, setPostInteractions] = useState<Record<number, NoteInteractionState>>({})
  const [mobileTab, setMobileTab] = useState<MobileTab>('feed')
  const [, setLastResult] = useState('')
  const [meProfile, setMeProfile] = useState<MeResponse>()
  const [authorProfiles, setAuthorProfiles] = useState<Record<number, UserProfileResponse>>({})
  const [authorPostsState, setAuthorPostsState] = useState<AuthorPostsState>(emptyAuthorPostsState)
  const [loadingAuthorProfile, setLoadingAuthorProfile] = useState(false)
  const [loadingAuthorPosts, setLoadingAuthorPosts] = useState(false)
  const [followListState, setFollowListState] = useState<FollowListState>(emptyFollowListState)
  const [loadingFollowList, setLoadingFollowList] = useState(false)
  const [commentListState, setCommentListState] = useState<CommentListState>(emptyCommentListState)
  const [loadingComments, setLoadingComments] = useState(false)
  const [commentForm] = Form.useForm<{ content: string }>()
  const [msgApi, contextHolder] = message.useMessage()

  const activeSession = useMemo(
    () => sessions.find((item) => item.key === activeSessionKey),
    [sessions, activeSessionKey],
  )

  const selectedCard = useMemo(
    () => feedState.items.find((item) => item.post_id === selectedPostID),
    [feedState.items, selectedPostID],
  )

  const selectedAuthorProfile = useMemo(
    () => (selectedAuthorID ? authorProfiles[selectedAuthorID] : undefined),
    [authorProfiles, selectedAuthorID],
  )

  const displayItems = useMemo(() => {
    return feedState.items
  }, [feedState.items])

  const detailRecommendations = useMemo(() => {
    if (!selectedCard) {
      return []
    }
    return displayItems.filter((item) => item.post_id !== selectedCard.post_id).slice(0, 3)
  }, [displayItems, selectedCard])

  const detailParagraphs = selectedCard?.content.split(/\n+/).filter((part) => part.trim().length > 0) ?? []
  const selectedAuthorCards = useMemo(() => {
    if (!selectedAuthorID) {
      return []
    }
    return feedState.items.filter((item) => item.user_id === selectedAuthorID).slice(0, 3)
  }, [feedState.items, selectedAuthorID])

  useEffect(() => {
    if (!activeSession && sessions.length > 0) {
      setActiveSessionKey(sessions[0].key)
    }
  }, [activeSession, sessions])

  useEffect(() => {
    if (typeof window === 'undefined') {
      return
    }
    window.localStorage.setItem(storageKeys.sessions, JSON.stringify(sessions))
  }, [sessions])

  useEffect(() => {
    if (typeof window === 'undefined') {
      return
    }
    if (!activeSessionKey) {
      window.localStorage.removeItem(storageKeys.activeSessionKey)
      return
    }
    window.localStorage.setItem(storageKeys.activeSessionKey, JSON.stringify(activeSessionKey))
  }, [activeSessionKey])

  useEffect(() => {
    if (typeof window === 'undefined') {
      return
    }
    window.localStorage.setItem(storageKeys.feedMode, JSON.stringify(feedMode))
  }, [feedMode])

  useEffect(() => {
    if (!activeSession?.token) {
      setMeProfile(undefined)
      setAuthorProfiles({})
      return
    }

    let cancelled = false
    void (async () => {
      try {
        const res = await api.me(activeSession.token)
        if (!cancelled && res.data.data) {
          setMeProfile(res.data.data)
        }
      } catch {
        if (!cancelled) {
          setMeProfile(undefined)
        }
      }
    })()

    return () => {
      cancelled = true
    }
  }, [activeSession?.token])

  useEffect(() => {
    const missingAuthorIDs = Array.from(new Set(feedState.items.map((item) => item.user_id))).filter(
      (authorID) => !authorProfiles[authorID],
    )
    if (missingAuthorIDs.length === 0) {
      return
    }

    let cancelled = false
    void (async () => {
      const results = await Promise.all(
        missingAuthorIDs.map(async (authorID) => {
          try {
            const res = await api.getUserProfile(authorID, activeSession?.token)
            return res.data.data
          } catch {
            return undefined
          }
        }),
      )
      if (cancelled) {
        return
      }

      const nextProfiles: Record<number, UserProfileResponse> = {}
      for (const profile of results) {
        if (!profile) {
          continue
        }
        nextProfiles[profile.user_id] = profile
      }
      if (Object.keys(nextProfiles).length > 0) {
        setAuthorProfiles((prev) => ({
          ...prev,
          ...nextProfiles,
        }))
      }
    })()

    return () => {
      cancelled = true
    }
  }, [feedState.items, authorProfiles, activeSession?.token])

  useEffect(() => {
    const missingAuthorIDs = Array.from(new Set(commentListState.items.map((item) => item.user_id))).filter(
      (authorID) => !authorProfiles[authorID],
    )
    if (missingAuthorIDs.length === 0) {
      return
    }

    let cancelled = false
    void (async () => {
      const results = await Promise.all(
        missingAuthorIDs.map(async (authorID) => {
          try {
            const res = await api.getUserProfile(authorID, activeSession?.token)
            return res.data.data
          } catch {
            return undefined
          }
        }),
      )
      if (cancelled) {
        return
      }

      const nextProfiles: Record<number, UserProfileResponse> = {}
      for (const profile of results) {
        if (profile) {
          nextProfiles[profile.user_id] = profile
        }
      }
      if (Object.keys(nextProfiles).length > 0) {
        setAuthorProfiles((prev) => ({
          ...prev,
          ...nextProfiles,
        }))
      }
    })()

    return () => {
      cancelled = true
    }
  }, [commentListState.items, authorProfiles, activeSession?.token])

  const saveResult = (title: string, payload: unknown) => {
    setLastResult(`${title}\n${JSON.stringify(payload, null, 2)}`)
  }

  const withBusyAction = async (fn: () => Promise<void>) => {
    setBusyAction(true)
    try {
      await fn()
    } finally {
      setBusyAction(false)
    }
  }

  const requireToken = () => {
    if (!activeSession?.token) {
      msgApi.warning('先登录一个账号，我们再拉 feed。')
      setShowAuthModal(true)
      return null
    }
    return activeSession.token
  }

  const upsertSession = (login: LoginResponse) => {
    const next: Session = {
      key: `${login.user_id}:${login.username}`,
      userID: login.user_id,
      username: login.username,
      nickname: login.nickname,
      token: login.token,
    }
    setSessions((prev) => {
      const idx = prev.findIndex((item) => item.key === next.key)
      if (idx < 0) {
        return [next, ...prev]
      }
      const clone = [...prev]
      clone[idx] = next
      return clone
    })
    setActiveSessionKey(next.key)
  }

  const applyFeedResponse = (response: FeedResponse, append: boolean) => {
    const mappedItems = mapFeedItems(response.items)
    setFeedState((prev) => ({
      items: append ? mergeFeedState(prev.items, response.items) : mappedItems,
      nextCursor: response.next_cursor,
      nextCursorToken: response.next_cursor_token,
      hasMore: response.has_more,
    }))
    void syncPostInteractions(response.items.map((item) => item.post_id))
  }

  const getInteractionState = (postID: number): NoteInteractionState => {
    return postInteractions[postID] ?? buildFallbackInteraction(postID)
  }

  const buildFallbackInteraction = (postID: number): NoteInteractionState => {
    const card =
      feedState.items.find((item) => item.post_id === postID) ||
      authorPostsState.items.find((item) => item.post_id === postID)
    return {
      liked: false,
      collected: false,
      likeCount: card?.likeCount ?? 0,
      collectCount: card?.collectCount ?? 0,
      commentCount: card?.commentCount ?? 0,
    }
  }

  const applyPostInteraction = (item: PostInteraction) => {
    setPostInteractions((prev) => ({
      ...prev,
      [item.post_id]: {
        liked: item.liked,
        collected: item.collected,
        likeCount: item.like_count,
        collectCount: item.collect_count,
        commentCount: item.comment_count,
      },
    }))
  }

  const syncPostInteractions = async (postIDs: number[]) => {
    const uniqueIDs = Array.from(new Set(postIDs.filter((postID) => postID > 0)))
    if (uniqueIDs.length === 0) {
      return
    }
    try {
      const res = await api.getPostInteractionStatuses(uniqueIDs, activeSession?.token)
      for (const item of res.data.data?.items ?? []) {
        applyPostInteraction(item)
      }
      saveResult('posts/interactions/status', res.data)
    } catch {
      // Interaction status is additive UI data; feed rendering should survive failures.
    }
  }

  const openNoteDetail = (item: FeedCard) => {
    setSelectedPostID(item.post_id)
    setCommentListState(emptyCommentListState)
    void syncPostInteractions([item.post_id])
    void loadComments(item.post_id, false)
  }

  const closeNoteDetail = () => {
    setSelectedPostID(undefined)
  }

  const closeFollowList = () => {
    setFollowListState((prev) => ({
      ...prev,
      open: false,
      items: [],
      nextCursor: 0,
      hasMore: false,
    }))
  }

  const closeAuthorProfile = () => {
    closeFollowList()
    setSelectedAuthorID(undefined)
    setAuthorPostsState(emptyAuthorPostsState)
  }

  const onCardKeyDown = (event: KeyboardEvent<HTMLElement>, item: FeedCard) => {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault()
      openNoteDetail(item)
    }
  }

  const onLikeClick = async (event: MouseEvent<HTMLButtonElement>, postID: number) => {
    event.stopPropagation()
    const token = requireToken()
    if (!token) return

    try {
      const current = getInteractionState(postID)
      const res = current.liked ? await api.unlikePost(postID, token) : await api.likePost(postID, token)
      if (res.data.data) {
        applyPostInteraction(res.data.data)
      }
      saveResult(current.liked ? 'post/unlike' : 'post/like', res.data)
    } catch (err) {
      msgApi.error(explainError(err))
    }
  }

  const onCollectClick = async (event: MouseEvent<HTMLButtonElement>, postID: number) => {
    event.stopPropagation()
    const token = requireToken()
    if (!token) return

    try {
      const current = getInteractionState(postID)
      const res = current.collected ? await api.uncollectPost(postID, token) : await api.collectPost(postID, token)
      if (res.data.data) {
        applyPostInteraction(res.data.data)
      }
      saveResult(current.collected ? 'post/uncollect' : 'post/collect', res.data)
    } catch (err) {
      msgApi.error(explainError(err))
    }
  }

  const onCommentClick = (event: MouseEvent<HTMLButtonElement>, item: FeedCard) => {
    event.stopPropagation()
    openNoteDetail(item)
  }

  const onShareClick = async (event: MouseEvent<HTMLButtonElement>, item: FeedCard) => {
    event.stopPropagation()

    const shareText = `Feed Flow Notes｜作者 #${item.user_id}\n${item.content}`
    try {
      if (navigator.clipboard) {
        await navigator.clipboard.writeText(shareText)
        msgApi.success('分享文案已复制到剪贴板')
        return
      }
    } catch {
      // fallback to detail view below
    }

    openNoteDetail(item)
    msgApi.info('浏览器未开放剪贴板，已为你打开详情页。')
  }

  const refreshAuthorProfileCache = (profile: UserProfileResponse) => {
    setAuthorProfiles((prev) => ({
      ...prev,
      [profile.user_id]: profile,
    }))
  }

  const loadAuthorProfile = async (authorID: number) => {
    setLoadingAuthorProfile(true)
    try {
      const res = await api.getUserProfile(authorID, activeSession?.token)
      if (res.data.data) {
        refreshAuthorProfileCache(res.data.data)
      }
      saveResult(`users/${authorID}`, res.data)
      return res.data.data
    } catch (err) {
      msgApi.error(explainError(err))
      return undefined
    } finally {
      setLoadingAuthorProfile(false)
    }
  }

  const applyAuthorPostsResponse = (response: UserPostsResponse, append: boolean) => {
    setAuthorPostsState((prev) => ({
      items: append ? mergeFeedState(prev.items, response.items) : mapFeedItems(response.items),
      nextCursor: response.next_cursor,
      hasMore: response.has_more,
    }))
    void syncPostInteractions(response.items.map((item) => item.post_id))
  }

  const loadAuthorPosts = async (authorID: number, append: boolean) => {
    setLoadingAuthorPosts(true)
    try {
      const res = await api.getUserPosts(authorID, {
        token: activeSession?.token,
        limit: defaultAuthorPostsLimit,
        ...(append && authorPostsState.nextCursor > 0
          ? { lastPostID: authorPostsState.nextCursor }
          : {}),
      })
      if (res.data.data) {
        applyAuthorPostsResponse(res.data.data, append)
      }
      saveResult(append ? `users/${authorID}/posts/load-more` : `users/${authorID}/posts`, res.data)
    } catch (err) {
      msgApi.error(explainError(err))
    } finally {
      setLoadingAuthorPosts(false)
    }
  }

  const applyCommentListResponse = (response: CommentListResponse, append: boolean) => {
    setCommentListState((prev) => ({
      items: append ? [...prev.items, ...response.items] : response.items,
      nextCursor: response.next_cursor,
      hasMore: response.has_more,
    }))
  }

  const loadComments = async (postID: number, append: boolean) => {
    setLoadingComments(true)
    try {
      const res = await api.getPostComments(postID, {
        limit: defaultCommentLimit,
        ...(append && commentListState.nextCursor > 0 ? { lastCommentID: commentListState.nextCursor } : {}),
      })
      if (res.data.data) {
        applyCommentListResponse(res.data.data, append)
      }
      saveResult(append ? 'comments/load-more' : 'comments/list', res.data)
    } catch (err) {
      msgApi.error(explainError(err))
    } finally {
      setLoadingComments(false)
    }
  }

  const onCreateComment = async (values: { content: string }) => {
    if (!selectedCard) {
      return
    }
    const token = requireToken()
    if (!token) return

    await withBusyAction(async () => {
      try {
        const res = await api.createPostComment(selectedCard.post_id, { content: values.content }, token)
        saveResult('comments/create', res.data)
        commentForm.resetFields()
        await Promise.all([
          loadComments(selectedCard.post_id, false),
          syncPostInteractions([selectedCard.post_id]),
        ])
        msgApi.success('评论已发布')
      } catch (err) {
        msgApi.error(explainError(err))
      }
    })
  }

  const openAuthorProfile = async (authorID: number) => {
    closeFollowList()
    setSelectedAuthorID(authorID)
    setAuthorPostsState(emptyAuthorPostsState)
    await Promise.all([loadAuthorProfile(authorID), loadAuthorPosts(authorID, false)])
  }

  const loadFollowList = async (kind: FollowListKind, append: boolean) => {
    if (!selectedAuthorID) {
      return
    }

    setLoadingFollowList(true)
    try {
      const lastFollowID =
        append && followListState.kind === kind && followListState.nextCursor > 0
          ? followListState.nextCursor
          : undefined
      const res =
        kind === 'followers'
          ? await api.getUserFollowers(selectedAuthorID, {
              token: activeSession?.token,
              limit: defaultFollowListLimit,
              ...(typeof lastFollowID === 'number' ? { lastFollowID } : {}),
            })
          : await api.getUserFollowing(selectedAuthorID, {
              token: activeSession?.token,
              limit: defaultFollowListLimit,
              ...(typeof lastFollowID === 'number' ? { lastFollowID } : {}),
            })
      const payload = res.data.data
      setFollowListState({
        kind,
        open: true,
        items:
          append && followListState.kind === kind
            ? [...followListState.items, ...(payload?.items ?? [])]
            : payload?.items ?? [],
        nextCursor: payload?.next_cursor ?? 0,
        hasMore: payload?.has_more ?? false,
      })
      saveResult(`users/${selectedAuthorID}/${kind}`, res.data)
    } catch (err) {
      msgApi.error(explainError(err))
    } finally {
      setLoadingFollowList(false)
    }
  }

  const openFollowList = async (kind: FollowListKind) => {
    await loadFollowList(kind, false)
  }

  const openAuthorFromFollowList = async (userID: number) => {
    closeFollowList()
    await openAuthorProfile(userID)
  }

  const onFollowFromList = async (targetUserID: number) => {
    await onFollow({ target_user_id: targetUserID })
    if (followListState.open) {
      await loadFollowList(followListState.kind, false)
    }
  }

  const onUnfollowFromList = async (targetUserID: number) => {
    await onUnfollow(targetUserID)
    if (followListState.open) {
      await loadFollowList(followListState.kind, false)
    }
  }

  const loadFeed = async ({
    append,
    silent = false,
    manualRefresh = false,
  }: {
    append: boolean
    silent?: boolean
    manualRefresh?: boolean
  }) => {
    const token = requireToken()
    if (!token) return

    setLoadingFeed(true)
    try {
      const res =
        feedMode === 'discover'
          ? await api.getDiscoverFeed(token, {
              limit: defaultFeedLimit,
              ...(append && feedState.nextCursor > 0 ? { lastPostID: feedState.nextCursor } : {}),
            })
          : await api.getFeed(token, {
              limit: defaultFeedLimit,
              ...(append
                ? feedState.nextCursorToken
                  ? { cursor: feedState.nextCursorToken }
                  : { lastPostID: feedState.nextCursor }
                : manualRefresh
                  ? { refresh: true }
                  : {}),
            })
      const payload = res.data.data
      if (payload) {
        applyFeedResponse(payload, append)
        if (!append && manualRefresh && payload.fallback_mode === 'latest') {
          msgApi.info('暂无更多新内容，已为你展示最近内容')
        }
      }
      const feedLabel = feedMode === 'discover' ? 'feed/discover' : 'feed'
      saveResult(append ? `${feedLabel}/load-more` : `${feedLabel}/refresh`, res.data)
      if (!append && !silent) {
        msgApi.success(feedMode === 'discover' ? '发现页已刷新' : '首页已刷新')
      }
    } catch (err) {
      msgApi.error(explainError(err))
    } finally {
      setLoadingFeed(false)
    }
  }

  useEffect(() => {
    if (!activeSession?.token) {
      return
    }

    void loadFeed({ append: false, silent: true })
    // loadFeed intentionally stays outside deps; this effect is keyed by the feed source.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeSession?.token, feedMode])

  const switchFeedMode = async (nextMode: FeedMode) => {
    setMobileTab('feed')
    setFeedState(emptyFeedState)
    if (nextMode === feedMode) {
      await loadFeed({ append: false, silent: true })
      return
    }
    setFeedMode(nextMode)
  }

  const onRegister = async (values: {
    username: string
    password: string
    nickname: string
  }) => {
    await withBusyAction(async () => {
      try {
        const res = await api.register(values)
        saveResult('register', res.data)
        msgApi.success(`注册成功，user_id=${res.data.data?.user_id ?? '-'}`)
      } catch (err) {
        msgApi.error(explainError(err))
      }
    })
  }

  const onLogin = async (values: { username: string; password: string }) => {
    await withBusyAction(async () => {
      try {
        const res = await api.login(values)
        if (res.data.data) {
          upsertSession(res.data.data)
          setShowAuthModal(false)
          msgApi.success(`欢迎回来，${res.data.data.nickname}`)
        }
        saveResult('login', res.data)
      } catch (err) {
        msgApi.error(explainError(err))
      }
    })
  }

  const onCreatePost = async (values: { content: string }) => {
    const token = requireToken()
    if (!token) return

    await withBusyAction(async () => {
      try {
        const res = await api.createPost({ content: values.content }, token)
        saveResult('create-post', res.data)
        msgApi.success('笔记发布成功')
        setShowCreateDrawer(false)
        await loadFeed({ append: false, silent: true })
      } catch (err) {
        msgApi.error(explainError(err))
      }
    })
  }

  const onFollow = async (values: { target_user_id: number }) => {
    const token = requireToken()
    if (!token) return

    await withBusyAction(async () => {
      try {
        const res = await api.follow(values.target_user_id, token)
        saveResult('follow', res.data)
        msgApi.success(`已关注用户 #${values.target_user_id}`)
        await loadFeed({ append: false, silent: true })
        if (selectedAuthorID === values.target_user_id) {
          await Promise.all([
            loadAuthorProfile(values.target_user_id),
            loadAuthorPosts(values.target_user_id, false),
          ])
        }
      } catch (err) {
        msgApi.error(explainError(err))
      }
    })
  }

  const onUnfollow = async (targetUserID: number) => {
    const token = requireToken()
    if (!token) return

    await withBusyAction(async () => {
      try {
        const res = await api.unfollow(targetUserID, token)
        saveResult('unfollow', res.data)
        msgApi.success(`已取消关注用户 #${targetUserID}`)
        await loadFeed({ append: false, silent: true })
        if (selectedAuthorID === targetUserID) {
          await Promise.all([loadAuthorProfile(targetUserID), loadAuthorPosts(targetUserID, false)])
        }
      } catch (err) {
        msgApi.error(explainError(err))
      }
    })
  }

  const performDeletePost = async (postID: number, authorUserID: number) => {
    const token = requireToken()
    if (!token) return

    await withBusyAction(async () => {
      try {
        const res = await api.deletePost(postID, token)
        saveResult('delete-post', res.data)
        msgApi.success(`已删除帖子 #${postID}`)

        setFeedState((prev) => ({
          ...prev,
          items: prev.items.filter((item) => item.post_id !== postID),
        }))
        setAuthorPostsState((prev) => ({
          ...prev,
          items: prev.items.filter((item) => item.post_id !== postID),
        }))

        if (selectedPostID === postID) {
          closeNoteDetail()
        }

        await loadFeed({ append: false, silent: true })

        if (selectedAuthorID === authorUserID) {
          await Promise.all([loadAuthorProfile(authorUserID), loadAuthorPosts(authorUserID, false)])
        } else if (authorProfiles[authorUserID]) {
          await loadAuthorProfile(authorUserID)
        }
      } catch (err) {
        msgApi.error(explainError(err))
      }
    })
  }

  const onDeletePost = (postID: number, authorUserID: number) => {
    Modal.confirm({
      title: '确认删除这条帖子吗？',
      content: `删除后会从当前展示列表中移除，post_id=${postID}`,
      okText: '确认删除',
      okButtonProps: { danger: true },
      cancelText: '取消',
      onOk: async () => {
        await performDeletePost(postID, authorUserID)
      },
    })
  }

  const onSearchAuthor = async () => {
    const raw = authorSearchText.trim()
    const authorID = Number(raw)
    if (!raw || !Number.isInteger(authorID) || authorID <= 0) {
      msgApi.warning('请输入有效的用户 ID')
      return
    }

    await openAuthorProfile(authorID)
  }

  const emptyState = !loadingFeed && displayItems.length === 0
  const showFeedStageOnMobile = mobileTab === 'feed'
  const showLeftPanelOnMobile = mobileTab === 'profile'
  const canDeleteSelectedPost = Boolean(
    activeSession && selectedCard && activeSession.userID === selectedCard.user_id,
  )
  const selectedInteraction = selectedCard ? getInteractionState(selectedCard.post_id) : undefined

  const renderNoteCard = (item: FeedCard, variant: 'warm' | 'cool') => {
    const interaction = getInteractionState(item.post_id)
    const displayedLikeCount = interaction.likeCount
    const coverBackground =
      variant === 'warm'
        ? `linear-gradient(145deg, ${item.coverColor}, ${item.accentColor})`
        : `linear-gradient(150deg, ${item.accentColor}, ${item.coverColor})`

    return (
      <article
        className={`note-card ${variant === 'cool' ? 'note-card-cool' : ''}`}
        key={item.post_id}
        role="button"
        tabIndex={0}
        onClick={() => openNoteDetail(item)}
        onKeyDown={(event) => onCardKeyDown(event, item)}
      >
        <div
          className="note-cover"
          style={{
            height: item.coverHeight,
            background: coverBackground,
          }}
        >
          <div className={`cover-badge ${variant === 'cool' ? 'hot' : ''}`}>
            {variant === 'cool' ? <FireOutlined /> : <span>#{item.post_id}</span>}
            {variant === 'cool' ? <span>探索氛围</span> : <span>关注流</span>}
          </div>

          <button
            type="button"
            className="note-open-chip"
            onClick={(event) => {
              event.stopPropagation()
              openNoteDetail(item)
            }}
          >
            <EyeOutlined />
            <span>详情</span>
          </button>

          <div className="cover-overlay">
            <div className="cover-eyebrow">{variant === 'cool' ? '生活记录' : 'Feed Flow'}</div>
            <div className="cover-title">{item.heroTitle}</div>
          </div>
        </div>

        <div className="note-body">
          <div className="note-text">{item.noteText}</div>

          <div className="note-tag-row">
            {item.topicTags.map((tag) => (
              <span className="topic-chip" key={`${item.post_id}-${tag}`}>
                {tag}
              </span>
            ))}
          </div>

          <div className="note-meta">
            <div className="author-chip">
              <button
                type="button"
                className="author-avatar-btn"
                onClick={(event) => {
                  event.stopPropagation()
                  void openAuthorProfile(item.user_id)
                }}
              >
                <Avatar
                  size={28}
                  src={authorProfiles[item.user_id]?.avatar || undefined}
                  icon={<UserOutlined />}
                />
              </button>
              <div>
                <button
                  type="button"
                  className="author-name-btn"
                  onClick={(event) => {
                    event.stopPropagation()
                    void openAuthorProfile(item.user_id)
                  }}
                >
                  {authorProfiles[item.user_id]?.nickname ?? `作者 #${item.user_id}`}
                </button>
                <div className="author-time">{formatBeijingDateTime(item.created_at)}</div>
              </div>
            </div>
            <div className="note-stats">
              <span>{variant === 'cool' ? '热度感' : '收藏感'}</span>
              <strong>
                {variant === 'cool'
                  ? formatCount(item.likeCount + item.commentCount)
                  : formatCount(item.collectCount)}
              </strong>
            </div>
          </div>

          <div className="note-actions">
            <button
              type="button"
              className={`note-action-btn ${interaction.liked ? 'active' : ''}`}
              onClick={(event) => onLikeClick(event, item.post_id)}
            >
              {interaction.liked ? <HeartFilled /> : <HeartOutlined />}
              <span>{formatCount(displayedLikeCount)}</span>
            </button>
          </div>
        </div>
      </article>
    )
  }

  return (
    <div className="app-shell">
      {contextHolder}

      <header className="topbar">
        <div className="brand-block">
          <div className="brand-mark">FeedFlow</div>
        </div>

        <div className="search-box">
          <input
            value={authorSearchText}
            onChange={(event) => setAuthorSearchText(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') {
                event.preventDefault()
                void onSearchAuthor()
              }
            }}
            inputMode="numeric"
            placeholder="搜索用户 ID"
          />
          <button
            type="button"
            className="search-submit-btn"
            onClick={() => void onSearchAuthor()}
            disabled={loadingAuthorProfile || loadingAuthorPosts}
            aria-label="查看用户卡片"
          >
            <SearchOutlined />
          </button>
        </div>

        <div className="topbar-actions">
          <button type="button" className="topbar-link" onClick={() => setShowCreateDrawer(true)}>
            创作中心
          </button>
          <button type="button" className="topbar-link" onClick={() => setShowAuthModal(true)}>
            账号管理
          </button>
        </div>
      </header>

      <main className="layout-grid">
        <aside className={`left-panel ${showLeftPanelOnMobile ? 'mobile-visible' : 'mobile-hidden'}`}>
          <nav className="side-nav" aria-label="主要导航">
            <button
              type="button"
              className={`side-nav-item ${feedMode === 'discover' ? 'active' : ''}`}
              onClick={() => void switchFeedMode('discover')}
            >
              <HomeOutlined />
              <span>发现</span>
            </button>
            <button
              type="button"
              className={`side-nav-item ${feedMode === 'following' ? 'active' : ''}`}
              onClick={() => void switchFeedMode('following')}
            >
              <TeamOutlined />
              <span>关注</span>
            </button>
            <button type="button" className="side-nav-item" onClick={() => setShowCreateDrawer(true)}>
              <PlusOutlined />
              <span>发布</span>
            </button>
            <button
              type="button"
              className="side-nav-item"
              onClick={() => {
                if (activeSession) {
                  void openAuthorProfile(activeSession.userID)
                } else {
                  setShowAuthModal(true)
                }
              }}
            >
              <UserOutlined />
              <span>我</span>
            </button>
          </nav>

          <div className="side-spacer" />

          <button type="button" className="side-nav-item more-item" onClick={() => setShowAuthModal(true)}>
            <UserAddOutlined />
            <span>{activeSession ? meProfile?.nickname ?? activeSession.nickname : '登录'}</span>
          </button>
        </aside>

        <section className={`feed-stage ${showFeedStageOnMobile ? 'mobile-visible' : 'mobile-hidden'}`}>
          <div className="feed-stage-head">
            <div>
              <Title level={3} className="feed-title">
                {feedMode === 'following' ? '关注页' : '发现页'}
              </Title>
              <Text className="muted-text">
                {activeSession
                  ? `当前账号 ${activeSession.nickname} 的内容流`
                  : '先登录账号，再体验发帖和刷 feed'}
              </Text>
            </div>
            <button
              type="button"
              className="feed-refresh-btn"
              onClick={() => void loadFeed({ append: false, manualRefresh: true })}
              disabled={loadingFeed}
            >
              <ReloadOutlined />
              <span>刷新</span>
            </button>
          </div>

          {loadingFeed && feedState.items.length === 0 ? (
            <div className="loading-shell">
              <Spin size="large" />
            </div>
          ) : emptyState ? (
            <div className="empty-shell">
              <Empty
                description={
                  activeSession
                    ? '还没有内容，先关注一个用户或者发一条帖子试试。'
                    : '登录后再进入首页体验。'
                }
              />
              {!activeSession ? (
                <Button type="primary" onClick={() => setShowAuthModal(true)}>
                  去登录
                </Button>
              ) : (
                <Button onClick={() => void loadFeed({ append: false, manualRefresh: true })}>立即刷新</Button>
              )}
            </div>
          ) : (
            <>
              <div className="masonry-grid">
                {displayItems.map((item, index) => renderNoteCard(item, index % 2 === 0 ? 'warm' : 'cool'))}
              </div>

              <div className="feed-footer">
                <Button
                  type="primary"
                  size="large"
                  loading={loadingFeed}
                  disabled={!feedState.hasMore}
                  onClick={() => void loadFeed({ append: true })}
                >
                  {feedState.hasMore ? '继续往下看' : '已经到底了'}
                </Button>
              </div>
            </>
          )}
        </section>

      </main>

      <Modal
        open={showAuthModal}
        footer={null}
        onCancel={() => setShowAuthModal(false)}
        width={760}
        destroyOnHidden
        title="账号中心"
      >
        <div className="auth-grid">
          <section className="auth-card">
            <Title level={4}>注册</Title>
            <Form layout="vertical" onFinish={onRegister}>
              <Form.Item
                name="username"
                label="用户名"
                rules={[{ required: true, min: 3, message: '至少 3 位' }]}
              >
                <Input placeholder="alice" />
              </Form.Item>
              <Form.Item
                name="password"
                label="密码"
                rules={[{ required: true, min: 6, message: '至少 6 位' }]}
              >
                <Input.Password placeholder="123456" />
              </Form.Item>
              <Form.Item
                name="nickname"
                label="昵称"
                rules={[{ required: true, message: '填一个展示昵称' }]}
              >
                <Input placeholder="Alice" />
              </Form.Item>
              <Button htmlType="submit" type="primary" block loading={busyAction}>
                创建账号
              </Button>
            </Form>
          </section>

          <section className="auth-card">
            <Title level={4}>登录</Title>
            <Form layout="vertical" onFinish={onLogin}>
              <Form.Item
                name="username"
                label="用户名"
                rules={[{ required: true, message: '请输入用户名' }]}
              >
                <Input placeholder="alice" />
              </Form.Item>
              <Form.Item
                name="password"
                label="密码"
                rules={[{ required: true, message: '请输入密码' }]}
              >
                <Input.Password placeholder="123456" />
              </Form.Item>
              <Button htmlType="submit" block loading={busyAction}>
                登录并进入首页
              </Button>
            </Form>
          </section>
        </div>
      </Modal>

      <Drawer
        open={showCreateDrawer}
        onClose={() => setShowCreateDrawer(false)}
        title="发布一条新笔记"
        width={420}
        destroyOnHidden
      >
        <Form layout="vertical" onFinish={onCreatePost}>
          <Form.Item
            name="content"
            label="内容"
            rules={[{ required: true, min: 1, max: 500, message: '内容长度 1-500' }]}
          >
            <Input.TextArea
              rows={8}
              showCount
              maxLength={500}
              placeholder="写一条今天想分享的内容..."
            />
          </Form.Item>
          <Button htmlType="submit" type="primary" block loading={busyAction}>
            立即发布
          </Button>
        </Form>
      </Drawer>

      <nav className="mobile-bottom-nav" aria-label="移动端导航">
        <button
          type="button"
          className={`mobile-nav-btn ${mobileTab === 'feed' ? 'active' : ''}`}
          onClick={() => void switchFeedMode('discover')}
        >
          <HomeOutlined />
          <span>发现</span>
        </button>

        <button
          type="button"
          className="mobile-nav-btn mobile-nav-compose"
          onClick={() => {
            setMobileTab('compose')
            setShowCreateDrawer(true)
          }}
        >
          <PlusOutlined />
          <span>发布</span>
        </button>

        <button
          type="button"
          className={`mobile-nav-btn ${mobileTab === 'profile' ? 'active' : ''}`}
          onClick={() => setMobileTab('profile')}
        >
          <UserOutlined />
          <span>我的</span>
        </button>
      </nav>

      <Modal
        open={Boolean(selectedCard)}
        footer={null}
        onCancel={closeNoteDetail}
        width={1040}
        className="note-detail-modal"
        destroyOnHidden
      >
        {selectedCard ? (
          <div className="detail-layout">
            <section
              className="detail-hero"
              style={{
                background: `linear-gradient(155deg, ${selectedCard.coverColor}, ${selectedCard.accentColor})`,
              }}
            >
              <div className="detail-hero-badge">
                {feedMode === 'discover' ? <FireOutlined /> : <HomeOutlined />}
                <span>{feedMode === 'discover' ? '探索页视角' : '关注流视角'}</span>
              </div>
              <div className="detail-hero-overlay">
                <div className="detail-hero-kicker">Feed Flow Notes</div>
                <h2 className="detail-hero-title">{selectedCard.heroTitle}</h2>
                <div className="detail-hero-meta">
                  <button
                    type="button"
                    className="detail-author-link"
                    onClick={() => void openAuthorProfile(selectedCard.user_id)}
                  >
                    {authorProfiles[selectedCard.user_id]?.nickname ?? `作者 #${selectedCard.user_id}`}
                  </button>
                  <span>{formatBeijingDateTime(selectedCard.created_at)}</span>
                </div>
              </div>
            </section>

            <section className="detail-panel">
              <div className="detail-author-row">
                <div className="detail-author-main">
                  <button
                    type="button"
                    className="detail-author-avatar-btn"
                    onClick={() => void openAuthorProfile(selectedCard.user_id)}
                  >
                    <Avatar
                      size={44}
                      src={authorProfiles[selectedCard.user_id]?.avatar || undefined}
                      icon={<UserOutlined />}
                    />
                  </button>
                  <div>
                    <button
                      type="button"
                      className="detail-author-name-btn"
                      onClick={() => void openAuthorProfile(selectedCard.user_id)}
                    >
                      {authorProfiles[selectedCard.user_id]?.nickname ?? `作者 #${selectedCard.user_id}`}
                    </button>
                    <div className="detail-author-subtitle">
                      {authorProfiles[selectedCard.user_id]?.bio || `post_id=${selectedCard.post_id} · 北京时间 ${formatBeijingDateTime(selectedCard.created_at)}`}
                    </div>
                  </div>
                </div>
                <Tag color={feedMode === 'discover' ? 'volcano' : 'blue'}>
                  {feedMode === 'discover' ? '探索陈列' : '关注结果'}
                </Tag>
              </div>

              <div className="detail-tag-row">
                {selectedCard.topicTags.map((tag) => (
                  <span className="topic-chip large" key={`detail-${selectedCard.post_id}-${tag}`}>
                    {tag}
                  </span>
                ))}
              </div>

              <div className="detail-action-row">
                <button
                  type="button"
                  className={`detail-action-btn ${getInteractionState(selectedCard.post_id).liked ? 'active' : ''}`}
                  onClick={(event) => onLikeClick(event, selectedCard.post_id)}
                >
                  {getInteractionState(selectedCard.post_id).liked ? <HeartFilled /> : <HeartOutlined />}
                  <span>
                    喜欢 {formatCount(selectedInteraction?.likeCount ?? selectedCard.likeCount)}
                  </span>
                </button>

                <button
                  type="button"
                  className={`detail-action-btn ${getInteractionState(selectedCard.post_id).collected ? 'active' : ''}`}
                  onClick={(event) => onCollectClick(event, selectedCard.post_id)}
                >
                  {getInteractionState(selectedCard.post_id).collected ? <StarFilled /> : <StarOutlined />}
                  <span>
                    收藏 {formatCount(selectedInteraction?.collectCount ?? selectedCard.collectCount)}
                  </span>
                </button>

                <button
                  type="button"
                  className="detail-action-btn"
                  onClick={(event) => onCommentClick(event, selectedCard)}
                >
                  <MessageOutlined />
                  <span>评论 {formatCount(selectedInteraction?.commentCount ?? selectedCard.commentCount)}</span>
                </button>

                <button
                  type="button"
                  className="detail-action-btn"
                  onClick={(event) => void onShareClick(event, selectedCard)}
                >
                  <ShareAltOutlined />
                  <span>分享</span>
                </button>

                {canDeleteSelectedPost ? (
                  <button
                    type="button"
                    className="detail-action-btn danger"
                    onClick={() => void onDeletePost(selectedCard.post_id, selectedCard.user_id)}
                  >
                    <span>删除帖子</span>
                  </button>
                ) : null}
              </div>

              <div className="detail-stat-grid">
                <div className="detail-stat-card">
                  <span>喜欢感</span>
                  <strong>{formatCount(selectedInteraction?.likeCount ?? selectedCard.likeCount)}</strong>
                </div>
                <div className="detail-stat-card">
                  <span>收藏感</span>
                  <strong>{formatCount(selectedInteraction?.collectCount ?? selectedCard.collectCount)}</strong>
                </div>
                <div className="detail-stat-card">
                  <span>评论感</span>
                  <strong>{formatCount(selectedInteraction?.commentCount ?? selectedCard.commentCount)}</strong>
                </div>
              </div>

              <div className="detail-copy">
                {detailParagraphs.length > 0 ? (
                  detailParagraphs.map((paragraph, index) => (
                    <p className="detail-paragraph" key={`${selectedCard.post_id}-paragraph-${index + 1}`}>
                      {paragraph}
                    </p>
                  ))
                ) : (
                  <p className="detail-paragraph">{selectedCard.content}</p>
                )}
              </div>

              <div className="comment-section">
                <div className="detail-section-title">评论</div>
                <Form form={commentForm} layout="vertical" onFinish={onCreateComment}>
                  <Form.Item
                    name="content"
                    rules={[{ required: true, min: 1, max: 300, message: '评论长度 1-300' }]}
                  >
                    <Input.TextArea rows={3} showCount maxLength={300} placeholder="写下你的评论..." />
                  </Form.Item>
                  <Button htmlType="submit" type="primary" loading={busyAction}>
                    发布评论
                  </Button>
                </Form>

                {loadingComments && commentListState.items.length === 0 ? (
                  <div className="comment-loading">
                    <Spin />
                  </div>
                ) : commentListState.items.length === 0 ? (
                  <Empty description="还没有评论，来坐第一排。" />
                ) : (
                  <div className="comment-list">
                    {commentListState.items.map((comment) => (
                      <div className="comment-item" key={`comment-${comment.comment_id}`}>
                        <Avatar
                          size={34}
                          src={authorProfiles[comment.user_id]?.avatar || undefined}
                          icon={<UserOutlined />}
                        />
                        <div className="comment-main">
                          <button
                            type="button"
                            className="comment-author-btn"
                            onClick={() => void openAuthorProfile(comment.user_id)}
                          >
                            {authorProfiles[comment.user_id]?.nickname ?? `用户 #${comment.user_id}`}
                          </button>
                          <div className="comment-content">{comment.content}</div>
                          <div className="comment-time">{formatBeijingDateTime(comment.created_at)}</div>
                        </div>
                      </div>
                    ))}
                    {commentListState.hasMore ? (
                      <Button
                        block
                        loading={loadingComments}
                        onClick={() => void loadComments(selectedCard.post_id, true)}
                      >
                        查看更多评论
                      </Button>
                    ) : null}
                  </div>
                )}
              </div>

              {detailRecommendations.length > 0 ? (
                <div className="detail-recommendations">
                  <div className="detail-section-title">继续浏览</div>
                  <div className="detail-rec-list">
                    {detailRecommendations.map((item) => (
                      <button
                        type="button"
                        className="detail-rec-item"
                        key={`rec-${item.post_id}`}
                        onClick={() => openNoteDetail(item)}
                      >
                        <span className="detail-rec-tag">{item.topicTags[0] ?? '#推荐'}</span>
                        <strong>{item.heroTitle}</strong>
                        <span className="detail-rec-meta">
                          {authorProfiles[item.user_id]?.nickname ?? `作者 #${item.user_id}`} · {formatBeijingDateTime(item.created_at)}
                        </span>
                      </button>
                    ))}
                  </div>
                </div>
              ) : null}
            </section>
          </div>
        ) : null}
      </Modal>

      <Drawer
        open={Boolean(selectedAuthorID)}
        onClose={closeAuthorProfile}
        width={480}
        destroyOnHidden
        title="作者主页"
        className="author-drawer"
      >
        {selectedAuthorID ? (
          <div className="author-drawer-body">
            <div className="author-profile-header">
              <Avatar
                size={72}
                src={selectedAuthorProfile?.avatar || undefined}
                icon={<UserOutlined />}
              />
              <div className="author-profile-meta">
                <div className="author-profile-name">
                  {selectedAuthorProfile?.nickname ?? `作者 #${selectedAuthorID}`}
                </div>
                <div className="author-profile-handle">
                  @{selectedAuthorProfile?.username ?? `user-${selectedAuthorID}`}
                </div>
                <div className="author-profile-bio">
                  {selectedAuthorProfile?.bio || '这个作者还没有填写简介。'}
                </div>
              </div>
            </div>

            {loadingAuthorProfile && !selectedAuthorProfile ? (
              <div className="author-profile-loading">
                <Spin />
              </div>
            ) : null}

            {selectedAuthorProfile ? (
              <>
                <div className="author-profile-stats">
                  <button type="button" className="author-profile-stat interactive" disabled>
                    <span>作品</span>
                    <strong>{formatCount(selectedAuthorProfile.post_count)}</strong>
                  </button>
                  <button
                    type="button"
                    className="author-profile-stat interactive"
                    onClick={() => void openFollowList('followers')}
                  >
                    <span>粉丝</span>
                    <strong>{formatCount(selectedAuthorProfile.follower_count)}</strong>
                  </button>
                  <button
                    type="button"
                    className="author-profile-stat interactive"
                    onClick={() => void openFollowList('following')}
                  >
                    <span>关注</span>
                    <strong>{formatCount(selectedAuthorProfile.following_count)}</strong>
                  </button>
                </div>

                <div className="author-profile-actions">
                  <Tag color={selectedAuthorProfile.is_following ? 'magenta' : 'gold'}>
                    {selectedAuthorProfile.is_following ? '已关注' : '未关注'}
                  </Tag>
                  {activeSession && activeSession.userID !== selectedAuthorProfile.user_id ? (
                    <Space wrap>
                      {selectedAuthorProfile.is_following ? (
                        <Button
                          danger
                          icon={<UserAddOutlined />}
                          loading={busyAction}
                          onClick={() => void onUnfollow(selectedAuthorProfile.user_id)}
                        >
                          取消关注
                        </Button>
                      ) : (
                        <Button
                          type="primary"
                          icon={<UserAddOutlined />}
                          loading={busyAction}
                          onClick={() => void onFollow({ target_user_id: selectedAuthorProfile.user_id })}
                        >
                          关注作者
                        </Button>
                      )}
                    </Space>
                  ) : null}
                </div>
              </>
            ) : null}

            <div className="author-posts-section">
              <div className="author-posts-head">
                <span>作者发布的内容</span>
                <Button
                  size="small"
                  icon={<ReloadOutlined />}
                  loading={loadingAuthorPosts}
                  onClick={() => void loadAuthorPosts(selectedAuthorID, false)}
                >
                  刷新
                </Button>
              </div>

              {loadingAuthorPosts && authorPostsState.items.length === 0 ? (
                <div className="author-profile-loading">
                  <Spin />
                </div>
              ) : authorPostsState.items.length === 0 ? (
                <Empty description="这个作者暂时还没有公开作品。" />
              ) : (
                <>
                  <div className="author-post-list">
                    {authorPostsState.items.map((item) => (
                      <div className="author-post-item" key={`author-post-${item.post_id}`}>
                        <button
                          type="button"
                          className="author-post-main"
                          onClick={() => {
                            setSelectedAuthorID(undefined)
                            setSelectedPostID(item.post_id)
                          }}
                        >
                          <div className="author-post-title">{item.heroTitle}</div>
                          <div className="author-post-copy">{item.noteText}</div>
                          <div className="author-post-meta">
                            <span>{formatBeijingDateTime(item.created_at)}</span>
                            <span>#{item.post_id}</span>
                          </div>
                        </button>

                        {activeSession && activeSession.userID === item.user_id ? (
                          <button
                            type="button"
                            className="author-post-delete-btn"
                            onClick={() => onDeletePost(item.post_id, item.user_id)}
                          >
                            删除
                          </button>
                        ) : null}
                      </div>
                    ))}
                  </div>

                  {authorPostsState.hasMore ? (
                    <Button
                      block
                      loading={loadingAuthorPosts}
                      onClick={() => void loadAuthorPosts(selectedAuthorID, true)}
                    >
                      查看更多作品
                    </Button>
                  ) : null}
                </>
              )}

              {selectedAuthorCards.length > 0 ? (
                <div className="author-local-preview">
                  <div className="author-local-preview-title">
                    <TeamOutlined />
                    <span>当前首页里也出现过这些内容</span>
                  </div>
                  <div className="author-local-preview-list">
                    {selectedAuthorCards.map((item) => (
                      <button
                        type="button"
                        className="author-local-preview-item"
                        key={`author-local-${item.post_id}`}
                        onClick={() => {
                          setSelectedAuthorID(undefined)
                          setSelectedPostID(item.post_id)
                        }}
                      >
                        <strong>{item.heroTitle}</strong>
                        <span>{formatBeijingDateTime(item.created_at)}</span>
                      </button>
                    ))}
                  </div>
                </div>
              ) : null}
            </div>
          </div>
        ) : null}
      </Drawer>

      <Modal
        open={followListState.open}
        footer={null}
        onCancel={closeFollowList}
        width={520}
        destroyOnHidden
        title={followListState.kind === 'followers' ? '粉丝列表' : '关注列表'}
      >
        {loadingFollowList ? (
          <div className="follow-list-loading">
            <Spin />
          </div>
        ) : followListState.items.length === 0 ? (
          <Empty
            description={followListState.kind === 'followers' ? '还没有粉丝。' : '还没有关注任何人。'}
          />
        ) : (
          <div className="follow-list-panel">
            {followListState.items.map((item) => (
              <div className="follow-list-item" key={`${followListState.kind}-${item.user_id}`}>
                <button
                  type="button"
                  className="follow-list-item-link"
                  onClick={() => void openAuthorFromFollowList(item.user_id)}
                >
                  <Avatar size={48} src={item.avatar || undefined} icon={<UserOutlined />} />
                  <div className="follow-list-item-main">
                    <div className="follow-list-item-head">
                      <strong>{item.nickname || `用户 #${item.user_id}`}</strong>
                      <span>@{item.username || `user-${item.user_id}`}</span>
                    </div>
                    <div className="follow-list-item-bio">
                      {item.bio || '这个用户还没有填写简介。'}
                    </div>
                  </div>
                </button>

                {activeSession && activeSession.userID !== item.user_id ? (
                  item.is_following ? (
                    <Button danger loading={busyAction} onClick={() => void onUnfollowFromList(item.user_id)}>
                      取消关注
                    </Button>
                  ) : (
                    <Button type="primary" loading={busyAction} onClick={() => void onFollowFromList(item.user_id)}>
                      关注
                    </Button>
                  )
                ) : null}
              </div>
            ))}

            {followListState.hasMore ? (
              <Button
                block
                loading={loadingFollowList}
                onClick={() => void loadFollowList(followListState.kind, true)}
              >
                查看更多
              </Button>
            ) : null}
          </div>
        )}
      </Modal>
    </div>
  )
}

export default App
