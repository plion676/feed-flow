# Docker MySQL

This project uses a local MySQL container for development.

## Default Setup

- service: `mysql`
- container name: `feed-flow-mysql`
- port: `3306`
- database: `feed_flow`
- username: `root`
- password: `password`

## Start

```powershell
docker compose up -d mysql
```

## Stop

```powershell
docker compose stop mysql
```

## Remove Container

```powershell
docker compose down
```

## Remove Container and Data Volume

```powershell
docker compose down -v
```

## Connection DSN

```text
root:password@tcp(127.0.0.1:3306)/feed_flow?charset=utf8mb4&parseTime=True&loc=Local
```
