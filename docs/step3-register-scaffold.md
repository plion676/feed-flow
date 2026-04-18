# Step 3 Register Scaffold

This step adds the first business-flow skeleton: user registration.

## Added Layers

- auth route: `POST /api/v1/auth/register`
- auth handler request parsing
- auth service registration flow skeleton
- user and user_count models
- repository placeholders for user persistence

## TODOs Left for Learning

- query user by username
- insert user and user_count records
- hash password securely
- add database transaction support
