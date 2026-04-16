# CPCloud Payment Integration Plan

## Goal

Add a real payment system into `CLIProxyCloud` so `CLIProxyApp` users can:

- buy `Pro`
- buy `Pro Max`
- refresh plan status after payment
- let `admin` manage prices and product availability from the backend

The first release should focus on stable QR-code based payments for desktop users.

## Recommended payment route

### WeChat Pay

Use **WeChat Pay API v3 Native Pay**.

Reason:

- `CLIProxyApp` is a desktop app
- users can scan a QR code directly with WeChat
- server-side signing and callback flow are clear

### Alipay

Use **Alipay QR-code based order creation** for desktop payment.

Reason:

- same user experience as WeChat
- easier to unify the desktop payment flow
- no need to force a browser redirect flow in the first phase

## Scope split

### CLIProxyApp

Responsible for:

- showing purchasable plans
- requesting order creation
- rendering payment QR code
- polling payment status when needed
- refreshing current `plan_code` after payment success

### CLIProxyCloud

Responsible for:

- product and price management
- order creation
- calling WeChat Pay and Alipay official APIs
- payment callback verification
- idempotent payment confirmation
- subscription update after successful payment
- admin-side price adjustment

### CLIProxyApi

Not involved in payment.

## Payment products

The system should not hardcode only `vip1` and `vip2`.
The display layer in `CLIProxyApp` can still show:

- `Pro`
- `Pro Max`

But backend should store them as configurable products mapped to plan codes.

Example:

- `pro_monthly` -> `vip1`
- `pro_max_monthly` -> `vip2`

Later expansion:

- `pro_yearly`
- `pro_max_yearly`
- `enterprise`

## Admin price management

This is required.

`admin` should be able to manage:

- product name
- product code
- mapped `plan_code`
- duration in days
- price in cents
- currency
- enabled/disabled state
- sort order
- display label
- product description

This means payment pricing should be stored in database, not hardcoded in client code.

## Database design additions

### `payment_products`

- `id`
- `product_code`
- `name`
- `display_name`
- `plan_code`
- `price_amount`
- `currency`
- `duration_days`
- `status`
- `sort_order`
- `description`
- `created_at`
- `updated_at`

### `payment_orders`

- `id`
- `order_no`
- `user_id`
- `product_id`
- `plan_code`
- `payment_provider` (`xunhu`)
- `amount`
- `currency`
- `status` (`pending`, `paid`, `closed`, `failed`, `refunded`)
- `provider_order_id`
- `provider_trade_no`
- `expires_at`
- `paid_at`
- `created_at`
- `updated_at`

### `payment_callbacks`

- `id`
- `provider`
- `order_no`
- `provider_trade_no`
- `payload`
- `status`
- `created_at`

### optional later: `refund_orders`

- keep for later

## Server-side services

Add a payment module under:

```text
internal/payments/
  provider.go
  service.go
  xunhu.go
```

### provider interface

The backend should expose a common provider interface:

- `CreateOrder`
- `QueryOrder`
- `CloseOrder`
- `HandleNotify`

This keeps provider integrations isolated. The current first-phase implementation uses XunhuPay.

## First-phase API plan

### User-side

#### GET `/api/v1/pay/products`

Return enabled payment products for display in `CLIProxyApp`.

#### POST `/api/v1/pay/orders`

Request body:

- `product_code`
- `provider` (`xunhu`)

Response:

- local order info
- qr code content or provider payment url
- expire time

#### GET `/api/v1/pay/orders/:orderNo`

Return current order status.

Used by `CLIProxyApp` to poll after QR display.

### Payment callback

#### POST `/api/v1/pay/xunhu/notify`

XunhuPay callback.

### Admin-side

#### GET `/api/v1/admin/pay/products`

List all products and prices.

#### POST `/api/v1/admin/pay/products`

Create a new product.

#### PATCH `/api/v1/admin/pay/products/:id`

Update product price and settings.

#### GET `/api/v1/admin/pay/orders`

List orders for admin review.

#### POST `/api/v1/admin/pay/orders/:orderNo/confirm`

Optional manual confirm tool for exceptional cases.

## Payment success flow

1. `CLIProxyApp` requests product list
2. user chooses `Pro` or `Pro Max`
3. `CLIProxyApp` creates order
4. `CLIProxyCloud` calls WeChat/Alipay official API
5. `CLIProxyApp` shows QR code
6. payment provider notifies `CLIProxyCloud`
7. `CLIProxyCloud` verifies signature
8. `CLIProxyCloud` marks order paid
9. `CLIProxyCloud` updates user subscription and `plan_code`
10. `CLIProxyApp` polls order state or refreshes `/me`
11. new features take effect immediately

## Idempotency rules

Required:

- payment callback must be idempotent
- order confirmation must be idempotent
- subscription update must be idempotent

This is mandatory because providers may retry callbacks.

## Callback hosting requirement

Payment callback must use a public HTTPS endpoint.

Recommended:

- `https://pay.router-for.me`
- or another dedicated payment subdomain

Do not rely on desktop local callbacks.

## Security requirements

- provider keys only in `CLIProxyCloud`
- never store merchant private keys in `CLIProxyApp`
- verify all payment callbacks
- log every callback payload
- keep admin operations auditable

## CLIProxyApp UI plan

Later `CLIProxyApp` can add:

- `Open Membership`
- product cards for `Pro` and `Pro Max`
- provider switch: `WeChat` / `Alipay`
- QR code modal
- order status polling
- payment success refresh

## Admin panel plan

Later `admin` panel should add:

- product list
- price editing
- enable/disable products
- order search
- manual compensation tools

## Suggested implementation order

1. database tables for products and orders
2. admin product management
3. order creation API
4. WeChat Native Pay integration
5. WeChat callback verification
6. plan upgrade after successful payment
7. `CLIProxyApp` product UI
8. Alipay integration
9. admin order management

## Notes for current plan naming

Backend can still map:

- `vip1` -> `Pro`
- `vip2` -> `Pro Max`

The display name and price should be controlled by product records, not fixed strings in the app.
