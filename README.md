# feed-flow

`feed-flow` is a youth-edition Feed stream backend project built for interview-ready system design and hands-on implementation.

## Current Progress

This repository currently contains the initial engineering skeleton and Step 2 infrastructure:

- `cmd/server`: application entrypoint
- `configs`: configuration files
- `docs`: project notes and design docs
- `internal`: internal application code
- `migrations`: database schema scripts
- `scripts`: local helper scripts
- unified response envelope
- shared error codes
- request logging, recovery, and request id middleware
- health-check routes

## Local MySQL

The project includes a Docker Compose MySQL setup for local development:

- service: `mysql`
- database: `feed_flow`
- username: `root`
- password: `password`

Start it with:

```powershell
docker compose up -d mysql
```

Create the current GORM tables with:

```powershell
go run ./cmd/migrate
```

The current goal is to keep the structure clear and expandable before we implement the business modules.
