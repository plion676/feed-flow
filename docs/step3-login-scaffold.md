# Step 3 Login Scaffold

This step adds the login flow skeleton:

- auth route: `POST /api/v1/auth/login`
- auth handler request parsing for login
- auth service login workflow
- invalid credentials error
- JWT generation support via dedicated manager
- login service unit tests

## Next Focus

- add JWT auth middleware for protected APIs
- add `/api/v1/users/me` endpoint to verify auth flow end-to-end
