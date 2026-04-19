# Step 3.1 JWT Config Wiring

This step wires JWT config into the application flow without implementing
the signing details yet.

## What changed

- added a dedicated `jwt.Manager`
- validated JWT config at application startup
- injected JWT dependencies into `AuthService`
- added `configs/config.example.yaml`

## Why this matters

- token generation no longer depends on hidden globals
- JWT secret and expiration come from config, not hard-coded service logic
- the next step can focus only on claims and signing

## TODOs Left for Learning

- decide whether to keep claims minimal (`user_id/iat/exp`) or add `jti` for revocation support
- implement JWT verification middleware that reuses the same config
