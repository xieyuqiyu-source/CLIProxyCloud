# CPCloud Database Design

## Core tables

### users
- email login account
- includes `role`

### plans
- stable `plan_code`
- JSON `feature_flags`

### user_subscriptions
- current active plan assignment
- supports expiry

### devices
- one active device for normal users by default
- admin is unrestricted

### auth_files
- owner type: `user` or `shared`
- stores encrypted file metadata

### auth_file_versions
- keeps file version history for future sync rollback

### sync_logs
- audit trail for upload/download/delete/plan assignment

## First-phase notes

- use GORM auto-migrate locally
- use MySQL from day one
- use local disk encrypted storage before moving to object storage

