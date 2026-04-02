# CPCloud API Draft

## Public

### POST `/api/v1/auth/register`
- email
- password

### POST `/api/v1/auth/login`
- email
- password
- device_id
- device_name
- platform

## Authenticated

### GET `/api/v1/me`

### GET `/api/v1/me/plan`

### GET `/api/v1/me/features`

### POST `/api/v1/devices/register`

### GET `/api/v1/devices/me?device_id=...`

### GET `/api/v1/me/auth-files`

### POST `/api/v1/me/auth-files/upload`
- multipart file upload

### GET `/api/v1/me/auth-files/:id/download`

### DELETE `/api/v1/me/auth-files/:id`

### GET `/api/v1/shared/auth-files`
- requires `allow_shared_pool`

### GET `/api/v1/shared/auth-files/:id/download`
- requires `allow_shared_pool`

## Admin

### POST `/api/v1/admin/shared-auth-files/upload`

### PATCH `/api/v1/admin/users/:id/plan`
- `plan_code`
- optional `expires_at`

