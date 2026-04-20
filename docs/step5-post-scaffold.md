# Step 5 Post Domain Scaffold

This step introduces the minimal content domain for future feed work:

- `posts` model and migration
- post repository (`Create`, `GetByID`)
- post handler routes:
  - `POST /api/v1/posts` (auth required)
  - `GET /api/v1/posts/:id` (public)

## TODOs Left for Learning

- implement `PostService.Create` core persistence logic
- optionally update user post count when a post is created

## Why this step

Feed generation requires source data. Before pull/push feed design,
the project needs a stable "publish content" path.
