# Step 6 Follow + Pull Feed Scaffold

This step introduces relationship and timeline skeleton:

- follow relation model/table: `follows`
- follow endpoints (auth required):
  - `POST /api/v1/follows/:target_user_id`
  - `DELETE /api/v1/follows/:target_user_id`
- feed endpoint (auth required):
  - `GET /api/v1/feed?last_post_id=<id>&limit=<n>`

## TODOs Left for Learning

- implement `FeedService.GetHomeFeed` core pull logic:
  - query following ids
  - pull posts by candidate author ids
  - map to feed items and build cursor

## Why this step

Feed system requires both social graph (who follows whom) and
timeline query logic. This scaffold prepares both while keeping
the feed aggregation core as your coding exercise.
