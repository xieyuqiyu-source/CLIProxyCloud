# CLIProxyCloud

`CLIProxyCloud` is the lightweight cloud backend for the CLIProxy ecosystem.

It manages:

- email registration and login
- `plan_code` based subscription control
- device binding
- personal auth file cloud sync
- shared auth file pool

It works together with:

- `CLIProxyApi`: local proxy and auth rotation engine
- `CLIProxyApp`: desktop client and plan-rule enforcement UI

## Tech Stack

- Go
- Gin
- GORM
- MySQL
- JWT
- encrypted local file storage

## Project Structure

```text
CLIProxyCloud/
├── cmd/server
├── docs
├── internal/config
├── internal/database
├── internal/handlers
├── internal/middleware
├── internal/models
├── internal/server
├── internal/services
├── internal/storage
└── internal/crypto
```

## Local Development

### 1. Prepare MySQL

Create a local database:

```sql
CREATE DATABASE cliproxy_cloud CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
```

### 2. Configure environment

Copy the example file and adjust values:

```bash
cp .env.example .env
```

Important variables:

- `CP_CLOUD_MYSQL_DSN`
- `CP_CLOUD_JWT_SECRET`
- `CP_CLOUD_STORAGE_ROOT`
- `CP_CLOUD_STORAGE_KEY`
- `CP_CLOUD_ADMIN_EMAIL`
- `CP_CLOUD_ADMIN_PASSWORD`

### 3. Install dependencies

```bash
go mod tidy
```

### 4. Run

```bash
go run ./cmd/server
```

The server defaults to:

- address: `:8090`
- health endpoint: `/healthz`

## Plan Codes

### `free`
- max enabled auth files: `1`
- no auto rotation
- no personal cloud sync
- no shared pool

### `vip1`
- multiple auth files
- auto rotation
- personal cloud sync
- no shared pool

### `vip2`
- includes `vip1`
- shared auth file download enabled

### `admin`
- full access
- unrestricted devices
- shared auth file maintenance

## API Summary

### Auth
- `POST /api/v1/auth/register`
- `POST /api/v1/auth/login`

### Account
- `GET /api/v1/me`
- `GET /api/v1/me/plan`
- `GET /api/v1/me/features`

### Devices
- `POST /api/v1/devices/register`
- `GET /api/v1/devices/me`

### Personal Auth Files
- `GET /api/v1/me/auth-files`
- `POST /api/v1/me/auth-files/upload`
- `GET /api/v1/me/auth-files/:id/download`
- `DELETE /api/v1/me/auth-files/:id`

### Shared Pool
- `GET /api/v1/shared/auth-files`
- `GET /api/v1/shared/auth-files/:id/download`

### Admin
- `POST /api/v1/admin/shared-auth-files/upload`
- `PATCH /api/v1/admin/users/:id/plan`

## Current Scope

This first implementation focuses on:

- plan-aware account backend
- one-device enforcement for non-admin users
- encrypted auth file storage
- shared auth file pool upload and download

Not yet implemented:

- email verification
- refresh tokens
- object storage
- background sync orchestration
- admin console UI

