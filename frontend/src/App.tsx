import { useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Col,
  Form,
  Input,
  InputNumber,
  Layout,
  List,
  Row,
  Select,
  Space,
  Table,
  Tag,
  Typography,
  message,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { api, explainError } from './api'
import type { FeedItem, FeedResponse, LoginResponse } from './types'

const { Header, Content } = Layout
const { Title, Text } = Typography

type Session = {
  key: string
  userID: number
  username: string
  nickname: string
  token: string
}

type FeedForm = {
  limit?: number
  last_post_id?: number
}

const beijingTimeZone = 'Asia/Shanghai'

function formatBeijingDateTime(value: unknown): string {
  let date: Date | null = null
  if (typeof value === 'string') {
    const parsed = new Date(value)
    if (!Number.isNaN(parsed.getTime())) {
      date = parsed
    }
  } else if (typeof value === 'number') {
    // Unix timestamp compatibility: seconds or milliseconds.
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

  return `${pick('year')}-${pick('month')}-${pick('day')} ${pick('hour')}:${pick('minute')}:${pick('second')} (北京时间)`
}

function normalizeAtFields(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map((item) => normalizeAtFields(item))
  }
  if (value && typeof value === 'object') {
    const entries = Object.entries(value as Record<string, unknown>).map(
      ([key, val]) => {
        if (key.endsWith('_at')) {
          return [key, formatBeijingDateTime(val)]
        }
        return [key, normalizeAtFields(val)]
      },
    )
    return Object.fromEntries(entries)
  }
  return value
}

function App() {
  const [sessions, setSessions] = useState<Session[]>([])
  const [activeSessionKey, setActiveSessionKey] = useState<string>()
  const [feed, setFeed] = useState<FeedResponse>()
  const [busy, setBusy] = useState(false)
  const [lastResult, setLastResult] = useState<string>('')
  const [msgApi, contextHolder] = message.useMessage()

  const activeSession = useMemo(
    () => sessions.find((item) => item.key === activeSessionKey),
    [sessions, activeSessionKey],
  )

  const feedColumns: ColumnsType<FeedItem> = [
    {
      title: 'PostID',
      dataIndex: 'post_id',
      width: 110,
    },
    {
      title: 'AuthorID',
      dataIndex: 'user_id',
      width: 120,
    },
    {
      title: 'Content',
      dataIndex: 'content',
      ellipsis: true,
    },
    {
      title: 'CreatedAt',
      dataIndex: 'created_at',
      width: 220,
      render: (value: string) => formatBeijingDateTime(value),
    },
  ]

  const withBusy = async (fn: () => Promise<void>) => {
    setBusy(true)
    try {
      await fn()
    } finally {
      setBusy(false)
    }
  }

  const saveResult = (title: string, payload: unknown) => {
    const normalizedPayload = normalizeAtFields(payload)
    setLastResult(`${title}\n${JSON.stringify(normalizedPayload, null, 2)}`)
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

  const requireToken = (): string | null => {
    if (!activeSession?.token) {
      msgApi.warning('请先登录并选择一个会话。')
      return null
    }
    return activeSession.token
  }

  const onRegister = async (values: {
    username: string
    password: string
    nickname: string
  }) => {
    await withBusy(async () => {
      try {
        const res = await api.register(values)
        msgApi.success(`注册成功: user_id=${res.data.data?.user_id}`)
        saveResult('register', res.data)
      } catch (err) {
        msgApi.error(explainError(err))
      }
    })
  }

  const onLogin = async (values: { username: string; password: string }) => {
    await withBusy(async () => {
      try {
        const res = await api.login(values)
        if (res.data.data) {
          upsertSession(res.data.data)
          msgApi.success(`登录成功: ${res.data.data.username}`)
        }
        saveResult('login', res.data)
      } catch (err) {
        msgApi.error(explainError(err))
      }
    })
  }

  const onGetMe = async () => {
    const token = requireToken()
    if (!token) return
    await withBusy(async () => {
      try {
        const res = await api.me(token)
        msgApi.success('获取 /users/me 成功')
        saveResult('users/me', res.data)
      } catch (err) {
        msgApi.error(explainError(err))
      }
    })
  }

  const onFollow = async (values: { target_user_id: number }) => {
    const token = requireToken()
    if (!token) return
    await withBusy(async () => {
      try {
        const res = await api.follow(values.target_user_id, token)
        msgApi.success(`关注成功: target_user_id=${values.target_user_id}`)
        saveResult('follow', res.data)
      } catch (err) {
        msgApi.error(explainError(err))
      }
    })
  }

  const onCreatePost = async (values: { content: string }) => {
    const token = requireToken()
    if (!token) return
    await withBusy(async () => {
      try {
        const res = await api.createPost({ content: values.content }, token)
        msgApi.success(`发帖成功: post_id=${res.data.data?.post_id}`)
        saveResult('create post', res.data)
      } catch (err) {
        msgApi.error(explainError(err))
      }
    })
  }

  const onGetFeed = async (values: FeedForm) => {
    const token = requireToken()
    if (!token) return
    await withBusy(async () => {
      try {
        const res = await api.getFeed(token, values.limit, values.last_post_id)
        setFeed(res.data.data)
        msgApi.success('拉取 Feed 成功')
        saveResult('feed', res.data)
      } catch (err) {
        msgApi.error(explainError(err))
      }
    })
  }

  return (
    <Layout className="page">
      {contextHolder}
      <Header className="hero">
        <Title level={2} className="hero-title">
          FeedFlow Front Console
        </Title>
        <Text className="hero-desc">
          Base URL: <code>{api.baseURL}</code>
        </Text>
      </Header>

      <Content className="content">
        <Row gutter={[16, 16]}>
          <Col xs={24} xl={12}>
            <Card title="1) 注册 / 登录">
              <Space direction="vertical" size="large" style={{ width: '100%' }}>
                <Form layout="vertical" onFinish={onRegister} autoComplete="off">
                  <Row gutter={12}>
                    <Col span={8}>
                      <Form.Item
                        name="username"
                        label="用户名"
                        rules={[{ required: true, min: 3 }]}
                      >
                        <Input placeholder="alice" />
                      </Form.Item>
                    </Col>
                    <Col span={8}>
                      <Form.Item
                        name="password"
                        label="密码"
                        rules={[{ required: true, min: 6 }]}
                      >
                        <Input.Password placeholder="123456" />
                      </Form.Item>
                    </Col>
                    <Col span={8}>
                      <Form.Item
                        name="nickname"
                        label="昵称"
                        rules={[{ required: true }]}
                      >
                        <Input placeholder="Alice" />
                      </Form.Item>
                    </Col>
                  </Row>
                  <Button type="primary" htmlType="submit" loading={busy}>
                    注册
                  </Button>
                </Form>

                <Form layout="vertical" onFinish={onLogin} autoComplete="off">
                  <Row gutter={12}>
                    <Col span={12}>
                      <Form.Item
                        name="username"
                        label="用户名"
                        rules={[{ required: true }]}
                      >
                        <Input placeholder="alice / bob" />
                      </Form.Item>
                    </Col>
                    <Col span={12}>
                      <Form.Item
                        name="password"
                        label="密码"
                        rules={[{ required: true }]}
                      >
                        <Input.Password placeholder="123456" />
                      </Form.Item>
                    </Col>
                  </Row>
                  <Button htmlType="submit" loading={busy}>
                    登录
                  </Button>
                </Form>

                <Alert
                  type="info"
                  showIcon
                  message="会话管理"
                  description={
                    <Space direction="vertical" style={{ width: '100%' }}>
                      <Select
                        placeholder="选择当前操作会话"
                        value={activeSessionKey}
                        onChange={setActiveSessionKey}
                        options={sessions.map((item) => ({
                          label: `${item.username} (#${item.userID})`,
                          value: item.key,
                        }))}
                      />
                      {activeSession ? (
                        <Text>
                          当前会话: {activeSession.username} / token 前缀:{' '}
                          {activeSession.token.slice(0, 18)}...
                        </Text>
                      ) : (
                        <Text type="secondary">未选择会话</Text>
                      )}
                    </Space>
                  }
                />
              </Space>
            </Card>
          </Col>

          <Col xs={24} xl={12}>
            <Card title="2) 业务操作">
              <Space direction="vertical" size="large" style={{ width: '100%' }}>
                <Button onClick={onGetMe} loading={busy}>
                  调用 /users/me
                </Button>

                <Form layout="inline" onFinish={onFollow} autoComplete="off">
                  <Form.Item
                    name="target_user_id"
                    label="关注用户ID"
                    rules={[{ required: true }]}
                  >
                    <InputNumber min={1} placeholder="1" />
                  </Form.Item>
                  <Form.Item>
                    <Button htmlType="submit" loading={busy}>
                      关注
                    </Button>
                  </Form.Item>
                </Form>

                <Form layout="vertical" onFinish={onCreatePost} autoComplete="off">
                  <Form.Item
                    name="content"
                    label="发帖内容"
                    rules={[{ required: true, min: 1, max: 500 }]}
                  >
                    <Input.TextArea rows={3} placeholder="hello feed from alice" />
                  </Form.Item>
                  <Button htmlType="submit" loading={busy}>
                    发帖
                  </Button>
                </Form>
              </Space>
            </Card>
          </Col>

          <Col xs={24}>
            <Card title="3) 拉取 Feed">
              <Form layout="inline" onFinish={onGetFeed} initialValues={{ limit: 10 }}>
                <Form.Item name="limit" label="limit">
                  <InputNumber min={1} max={50} />
                </Form.Item>
                <Form.Item name="last_post_id" label="last_post_id">
                  <InputNumber min={0} />
                </Form.Item>
                <Form.Item>
                  <Button type="primary" htmlType="submit" loading={busy}>
                    拉取
                  </Button>
                </Form.Item>
              </Form>

              <Space style={{ marginTop: 12 }}>
                <Tag color="blue">items: {feed?.items?.length ?? 0}</Tag>
                <Tag color="green">next_cursor: {feed?.next_cursor ?? 0}</Tag>
                <Tag color="orange">has_more: {String(feed?.has_more ?? false)}</Tag>
              </Space>

              <Table
                style={{ marginTop: 12 }}
                rowKey="post_id"
                dataSource={feed?.items ?? []}
                columns={feedColumns}
                pagination={false}
                size="small"
              />
            </Card>
          </Col>

          <Col xs={24}>
            <Card title="4) 最近一次响应">
              <List
                dataSource={lastResult ? [lastResult] : []}
                locale={{ emptyText: '还没有请求记录' }}
                renderItem={(item) => (
                  <List.Item>
                    <pre className="result-block">{item}</pre>
                  </List.Item>
                )}
              />
            </Card>
          </Col>
        </Row>
      </Content>
    </Layout>
  )
}

export default App
