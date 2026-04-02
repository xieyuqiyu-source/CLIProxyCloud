# CPCloud Architecture

## Goal

`CLIProxyCloud` is a lightweight backend for:

- email registration and login
- plan-based feature control via `plan_code`
- device binding
- personal auth file cloud sync
- shared auth file pool for higher plans

It does **not** replace `CLIProxyApi`. It only manages account, plan, sync, and shared pool concerns.

## Boundaries

- `CLIProxyApi`: local proxy and auth rotation engine
- `CLIProxyApp`: desktop UI and local enforcement of plan rules
- `CLIProxyCloud`: cloud account and auth-file backend

## Plan model

- `free`
- `vip1`
- `vip2`
- `admin`

All plan rules are driven by `feature_flags`.

## Key rules

- `free`: can only keep one local auth file enabled
- `vip1`: can use multiple auth files, auto rotation, personal cloud sync
- `vip2`: includes `vip1`, can also download shared auth files
- `vip2` expiry: downloaded shared auth files should be disabled locally by `CLIProxyApp`, but not deleted
- `admin`: full local and cloud maintenance authority

## Storage

- metadata: MySQL
- auth file bodies: encrypted local disk storage
- later migration target: server disk or object storage

## API shape

- `/api/v1/auth/*`
- `/api/v1/me/*`
- `/api/v1/devices/*`
- `/api/v1/shared/*`
- `/api/v1/admin/*`

