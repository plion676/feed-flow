# Step 4 Auth Middleware + /users/me Scaffold

This step scaffolds the protected user profile flow:

- JWT auth middleware entry: `internal/middleware/auth_jwt.go`
- protected endpoint: `GET /api/v1/users/me`
- user profile service/repo wiring

## TODOs Left for Learning

- implement `jwt.Manager.ParseToken` core verification logic
- validate claims details (`exp/iat/iss`) and ensure `user_id > 0`

## Current Behavior

- login can still generate token
- `/api/v1/users/me` route is protected by Bearer token middleware
- until ParseToken TODO is completed, protected endpoint returns unauthorized
