import { useEffect, useMemo, useState, type KeyboardEvent, type MouseEvent } from 'react'
import {
  Alert,
  Avatar,
  Button,
  Drawer,
  Empty,
  Form,
  Input,
  InputNumber,
  Modal,
  Segmented,
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
  UserAddOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { api, explainError } from './api'
import type { FeedItem, FeedResponse, LoginResponse } from './types'

const { Paragraph, Text, Title } = Typography

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

type NoteInteractionState = {
  liked: boolean
  collected: boolean
}

const beijingTimeZone = 'Asia/Shanghai'
const defaultFeedLimit = 12
const storageKeys = {
  sessions: 'feed-flow-notes:sessions',
  activeSessionKey: 'feed-flow-notes:active-session-key',
  feedMode: 'feed-flow-notes:feed-mode',
  noteInteractions: 'feed-flow-notes:note-interactions',
}
const emptyFeedState: FeedState = {
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

function buildFeedScore(item: FeedCard): number {
  return item.likeCount * 3 + item.collectCount * 4 + item.commentCount * 5 + (item.post_id % 17)
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

function splitColumns(items: FeedCard[]) {
  const left: FeedCard[] = []
  const right: FeedCard[] = []
  let leftHeight = 0
  let rightHeight = 0

  for (const item of items) {
    const estimate = item.coverHeight + Math.ceil(item.noteText.length / 18) * 9 + 216
    if (leftHeight <= rightHeight) {
      left.push(item)
      leftHeight += estimate
    } else {
      right.push(item)
      rightHeight += estimate
    }
  }

  return [left, right] as const
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
  const [searchText, setSearchText] = useState('')
  const [loadingFeed, setLoadingFeed] = useState(false)
  const [busyAction, setBusyAction] = useState(false)
  const [showAuthModal, setShowAuthModal] = useState(false)
  const [showCreateDrawer, setShowCreateDrawer] = useState(false)
  const [selectedPostID, setSelectedPostID] = useState<number>()
  const [noteInteractions, setNoteInteractions] = useState<Record<number, NoteInteractionState>>(() =>
    loadStorageValue<Record<number, NoteInteractionState>>(storageKeys.noteInteractions, {}),
  )
  const [mobileTab, setMobileTab] = useState<MobileTab>('feed')
  const [lastResult, setLastResult] = useState('')
  const [msgApi, contextHolder] = message.useMessage()

  const activeSession = useMemo(
    () => sessions.find((item) => item.key === activeSessionKey),
    [sessions, activeSessionKey],
  )

  const selectedCard = useMemo(
    () => feedState.items.find((item) => item.post_id === selectedPostID),
    [feedState.items, selectedPostID],
  )

  const filteredItems = useMemo(() => {
    const keyword = searchText.trim().toLowerCase()
    if (!keyword) {
      return feedState.items
    }
    return feedState.items.filter((item) => {
      return (
        item.content.toLowerCase().includes(keyword) ||
        item.noteText.toLowerCase().includes(keyword) ||
        String(item.user_id).includes(keyword)
      )
    })
  }, [feedState.items, searchText])

  const displayItems = useMemo(() => {
    if (feedMode === 'following') {
      return filteredItems
    }
    return [...filteredItems].sort((a, b) => {
      const scoreDiff = buildFeedScore(b) - buildFeedScore(a)
      if (scoreDiff !== 0) {
        return scoreDiff
      }
      return b.post_id - a.post_id
    })
  }, [feedMode, filteredItems])

  const [leftColumn, rightColumn] = useMemo(() => splitColumns(displayItems), [displayItems])

  const detailRecommendations = useMemo(() => {
    if (!selectedCard) {
      return []
    }
    return displayItems.filter((item) => item.post_id !== selectedCard.post_id).slice(0, 3)
  }, [displayItems, selectedCard])

  const detailParagraphs = selectedCard?.content.split(/\n+/).filter((part) => part.trim().length > 0) ?? []

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
    if (typeof window === 'undefined') {
      return
    }
    window.localStorage.setItem(storageKeys.noteInteractions, JSON.stringify(noteInteractions))
  }, [noteInteractions])

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
    setFeedState((prev) => ({
      items: append ? mergeFeedState(prev.items, response.items) : mapFeedItems(response.items),
      nextCursor: response.next_cursor,
      nextCursorToken: response.next_cursor_token,
      hasMore: response.has_more,
    }))
  }

  const getInteractionState = (postID: number): NoteInteractionState => {
    return noteInteractions[postID] ?? {
      liked: false,
      collected: false,
    }
  }

  const openNoteDetail = (item: FeedCard) => {
    setSelectedPostID(item.post_id)
  }

  const closeNoteDetail = () => {
    setSelectedPostID(undefined)
  }

  const toggleInteraction = (postID: number, field: keyof NoteInteractionState) => {
    setNoteInteractions((prev) => {
      const current = prev[postID] ?? {
        liked: false,
        collected: false,
      }
      return {
        ...prev,
        [postID]: {
          ...current,
          [field]: !current[field],
        },
      }
    })
  }

  const onCardKeyDown = (event: KeyboardEvent<HTMLElement>, item: FeedCard) => {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault()
      openNoteDetail(item)
    }
  }

  const onLikeClick = (event: MouseEvent<HTMLButtonElement>, postID: number) => {
    event.stopPropagation()
    toggleInteraction(postID, 'liked')
  }

  const onCollectClick = (event: MouseEvent<HTMLButtonElement>, postID: number) => {
    event.stopPropagation()
    toggleInteraction(postID, 'collected')
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

  const loadFeed = async ({ append, silent = false }: { append: boolean; silent?: boolean }) => {
    const token = requireToken()
    if (!token) return

    setLoadingFeed(true)
    try {
      const res = await api.getFeed(token, {
        limit: defaultFeedLimit,
        ...(append
          ? feedState.nextCursorToken
            ? { cursor: feedState.nextCursorToken }
            : { lastPostID: feedState.nextCursor }
          : {}),
      })
      const payload = res.data.data
      if (payload) {
        applyFeedResponse(payload, append)
      }
      saveResult(append ? 'feed/load-more' : 'feed/refresh', res.data)
      if (!append && !silent) {
        msgApi.success('首页已刷新')
      }
    } catch (err) {
      msgApi.error(explainError(err))
    } finally {
      setLoadingFeed(false)
    }
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
      } catch (err) {
        msgApi.error(explainError(err))
      }
    })
  }

  const onGetMe = async () => {
    const token = requireToken()
    if (!token) return

    await withBusyAction(async () => {
      try {
        const res = await api.me(token)
        saveResult('users/me', res.data)
        msgApi.success('账号信息已同步')
      } catch (err) {
        msgApi.error(explainError(err))
      }
    })
  }

  const emptyState = !loadingFeed && displayItems.length === 0
  const showFeedStageOnMobile = mobileTab === 'feed'
  const showLeftPanelOnMobile = mobileTab === 'profile'

  const renderNoteCard = (item: FeedCard, variant: 'warm' | 'cool') => {
    const interaction = getInteractionState(item.post_id)
    const displayedLikeCount = item.likeCount + (interaction.liked ? 1 : 0)
    const displayedCollectCount = item.collectCount + (interaction.collected ? 1 : 0)
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
              <Avatar size={28} icon={<UserOutlined />} />
              <div>
                <div className="author-name">作者 #{item.user_id}</div>
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

            <button
              type="button"
              className={`note-action-btn ${interaction.collected ? 'active' : ''}`}
              onClick={(event) => onCollectClick(event, item.post_id)}
            >
              {interaction.collected ? <StarFilled /> : <StarOutlined />}
              <span>{formatCount(displayedCollectCount)}</span>
            </button>

            <button
              type="button"
              className="note-action-btn"
              onClick={(event) => onCommentClick(event, item)}
            >
              <MessageOutlined />
              <span>{formatCount(item.commentCount)}</span>
            </button>

            <button
              type="button"
              className="note-action-btn note-action-link"
              onClick={(event) => void onShareClick(event, item)}
            >
              <ShareAltOutlined />
              <span>分享</span>
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
          <div className="brand-mark">FF</div>
          <div>
            <div className="brand-title">Feed Flow Notes</div>
            <div className="brand-subtitle">青春版内容社区 · 信息流演示</div>
          </div>
        </div>

        <div className="topbar-actions">
          <div className="search-box">
            <SearchOutlined />
            <input
              value={searchText}
              onChange={(event) => setSearchText(event.target.value)}
              placeholder="搜索文案 / 作者 ID"
            />
          </div>

          <Button
            className="ghost-btn"
            icon={<ReloadOutlined />}
            onClick={() => void loadFeed({ append: false })}
            loading={loadingFeed}
          >
            刷新
          </Button>

          <Button
            type="primary"
            className="create-btn"
            icon={<PlusOutlined />}
            onClick={() => setShowCreateDrawer(true)}
          >
            发布
          </Button>
        </div>
      </header>

      <main className="layout-grid">
        <aside className={`left-panel ${showLeftPanelOnMobile ? 'mobile-visible' : 'mobile-hidden'}`}>
          <section className="panel-card profile-card">
            <div className="profile-head">
              <Avatar size={54} icon={<UserOutlined />} />
              <div>
                <div className="panel-kicker">当前账号</div>
                <div className="profile-name">{activeSession ? activeSession.nickname : '还没有登录'}</div>
                <Text className="muted-text">
                  {activeSession
                    ? `@${activeSession.username} · user_id=${activeSession.userID}`
                    : '登录后即可发帖、关注和拉取 feed'}
                </Text>
              </div>
            </div>

            <div className="session-list">
              {sessions.length === 0 ? (
                <Button block onClick={() => setShowAuthModal(true)}>
                  登录 / 注册
                </Button>
              ) : (
                sessions.map((session) => (
                  <button
                    key={session.key}
                    className={`session-pill ${session.key === activeSessionKey ? 'active' : ''}`}
                    onClick={() => setActiveSessionKey(session.key)}
                  >
                    <span>{session.nickname}</span>
                    <span className="session-meta">@{session.username}</span>
                  </button>
                ))
              )}
            </div>

            <Space wrap>
              <Button onClick={onGetMe} disabled={!activeSession} loading={busyAction}>
                同步我的资料
              </Button>
              <Button onClick={() => setShowAuthModal(true)}>管理账号</Button>
            </Space>
          </section>

          <section className="panel-card trend-card">
            <div className="panel-head">
              <span className="panel-kicker">发现灵感</span>
              <Segmented<FeedMode>
                value={feedMode}
                onChange={(value) => setFeedMode(value)}
                options={[
                  { label: '关注流', value: 'following', icon: <HomeOutlined /> },
                  { label: '探索感', value: 'discover', icon: <FireOutlined /> },
                ]}
              />
            </div>
            <Paragraph className="panel-copy">
              当前后端提供的是关注 feed。这里的“探索感”先只做前端展示层切换，用不同文案氛围和卡片排序观感去模拟内容社区的产品态。
            </Paragraph>
            <Alert
              type="warning"
              showIcon
              message="当前是后端 Feed 演示版"
              description="还没有推荐召回，但我们已经把登录、关注、发帖、混排、曝光去重这条主链路接起来了。"
            />
          </section>

          <section className="panel-card follow-card">
            <div className="panel-head">
              <span className="panel-kicker">快速关注</span>
            </div>
            <Form layout="vertical" onFinish={onFollow}>
              <Form.Item
                name="target_user_id"
                label="目标用户 ID"
                rules={[{ required: true, message: '填一个用户 ID' }]}
              >
                <InputNumber min={1} placeholder="例如 2" style={{ width: '100%' }} />
              </Form.Item>
              <Button
                htmlType="submit"
                type="primary"
                icon={<UserAddOutlined />}
                block
                loading={busyAction}
              >
                关注这个作者
              </Button>
            </Form>
          </section>
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
            <Space wrap>
              <Tag color="magenta">items {feedState.items.length}</Tag>
              <Tag color="blue">next {feedState.nextCursor}</Tag>
              <Tag color="orange">has_more {String(feedState.hasMore)}</Tag>
            </Space>
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
                <Button onClick={() => void loadFeed({ append: false })}>立即刷新</Button>
              )}
            </div>
          ) : (
            <>
              <div className="masonry-grid">
                <div className="masonry-column">{leftColumn.map((item) => renderNoteCard(item, 'warm'))}</div>

                <div className="masonry-column">{rightColumn.map((item) => renderNoteCard(item, 'cool'))}</div>
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

        <aside className={`right-panel ${showLeftPanelOnMobile ? 'mobile-visible' : 'mobile-hidden'}`}>
          <section className="panel-card result-card">
            <div className="panel-head">
              <span className="panel-kicker">接口回显</span>
            </div>
            <pre className="result-block">{lastResult || '这里会展示最近一次接口响应。'}</pre>
          </section>

          <section className="panel-card guide-card">
            <div className="panel-head">
              <span className="panel-kicker">演示顺序</span>
            </div>
            <ol className="guide-list">
              <li>注册两个账号，例如 `alice` 和 `bob`。</li>
              <li>切到 `bob`，先关注 `alice`。</li>
              <li>切回 `alice`，发几条帖子。</li>
              <li>回到 `bob`，点击刷新，观察关注 feed 的混排结果。</li>
              <li>点击任一卡片，查看详情页和前端交互层。</li>
            </ol>
          </section>
        </aside>
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
          onClick={() => setMobileTab('feed')}
        >
          <HomeOutlined />
          <span>首页</span>
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
                  <span>作者 #{selectedCard.user_id}</span>
                  <span>{formatBeijingDateTime(selectedCard.created_at)}</span>
                </div>
              </div>
            </section>

            <section className="detail-panel">
              <div className="detail-author-row">
                <div className="detail-author-main">
                  <Avatar size={44} icon={<UserOutlined />} />
                  <div>
                    <div className="detail-author-name">作者 #{selectedCard.user_id}</div>
                    <div className="detail-author-subtitle">
                      post_id={selectedCard.post_id} · 北京时间 {formatBeijingDateTime(selectedCard.created_at)}
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
                    喜欢 {formatCount(selectedCard.likeCount + (getInteractionState(selectedCard.post_id).liked ? 1 : 0))}
                  </span>
                </button>

                <button
                  type="button"
                  className={`detail-action-btn ${getInteractionState(selectedCard.post_id).collected ? 'active' : ''}`}
                  onClick={(event) => onCollectClick(event, selectedCard.post_id)}
                >
                  {getInteractionState(selectedCard.post_id).collected ? <StarFilled /> : <StarOutlined />}
                  <span>
                    收藏 {formatCount(selectedCard.collectCount + (getInteractionState(selectedCard.post_id).collected ? 1 : 0))}
                  </span>
                </button>

                <button
                  type="button"
                  className="detail-action-btn"
                  onClick={(event) => onCommentClick(event, selectedCard)}
                >
                  <MessageOutlined />
                  <span>评论 {formatCount(selectedCard.commentCount)}</span>
                </button>

                <button
                  type="button"
                  className="detail-action-btn"
                  onClick={(event) => void onShareClick(event, selectedCard)}
                >
                  <ShareAltOutlined />
                  <span>分享</span>
                </button>
              </div>

              <div className="detail-stat-grid">
                <div className="detail-stat-card">
                  <span>喜欢感</span>
                  <strong>{formatCount(selectedCard.likeCount)}</strong>
                </div>
                <div className="detail-stat-card">
                  <span>收藏感</span>
                  <strong>{formatCount(selectedCard.collectCount)}</strong>
                </div>
                <div className="detail-stat-card">
                  <span>评论感</span>
                  <strong>{formatCount(selectedCard.commentCount)}</strong>
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

              <Alert
                className="detail-alert"
                type="info"
                showIcon
                message="当前详情来自首页 feed 数据展开"
                description="目前还没有独立的帖子详情接口，所以这里展示的是 feed 已经拿到的内容；这样能先把产品交互跑通。"
              />

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
                          作者 #{item.user_id} · {formatBeijingDateTime(item.created_at)}
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
    </div>
  )
}

export default App
