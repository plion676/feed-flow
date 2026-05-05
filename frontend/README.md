# Frontend Demo

这是一个用于演示后端能力的前端页面，风格参考“小红书式笔记流”，方便快速验证：

- 注册 / 登录
- 发布帖子
- 查看关注流 / 发现流
- 点赞 / 收藏 / 评论
- 查看用户主页
- 关注 / 取关
- 浏览粉丝 / 关注列表

它的目标不是做完整产品，而是作为后端项目的可视化操作台。

## 技术栈

- React
- TypeScript
- Vite
- Ant Design
- Axios

## 启动

在 `frontend` 目录执行：

```powershell
npm install
npm run dev
```

默认地址：

- `http://127.0.0.1:5173`

## 与后端联调

Vite 已在开发模式下配置代理：

- `/api` -> `http://127.0.0.1:8080`

因此本地联调时只需要保证后端已启动：

```powershell
go run ./cmd/server
```

## 可配置 API 地址

默认 API Base：

- `/api/v1`

如果你需要显式指定远端地址，可以设置：

- `VITE_API_BASE`

例如：

```powershell
$env:VITE_API_BASE="http://127.0.0.1:8080/api/v1"
npm run dev
```

## 当前演示能力

- 多账号本地切换
- Feed 列表展示
- Discover 流展示
- 帖子详情弹窗
- 作者主页抽屉
- 粉丝 / 关注列表弹层
- 点赞 / 收藏 / 评论
- 删除自己的帖子

## 适合怎么用

推荐用它来做这几件事：

1. 快速验证后端接口
2. 演示 Feed 主链路
3. 给项目增加更直观的可操作体验
4. 调试删帖、取关、刷新、分页等行为
