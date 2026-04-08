# CPCloud 支付接入方案

## 目标

在 `CLIProxyCloud` 中正式接入支付能力，让 `CLIProxyApp` 用户可以：

- 购买 `Pro`
- 购买 `Pro Max`
- 支付成功后自动刷新套餐状态
- 由 `admin` 在后台管理商品价格、套餐映射和上下架状态

第一阶段优先做稳定、可控、适合桌面端的二维码支付流程。

## 推荐接入路线

### 微信支付

优先接入 **微信支付 API v3 Native 支付**。

原因：

- `CLIProxyApp` 是桌面端应用
- 用户最自然的支付方式就是手机微信扫码
- 服务端签名、回调、验签流程成熟

### 支付宝

优先接入 **支付宝二维码下单能力**，保持与微信相同的桌面扫码体验。

原因：

- 用户体验统一
- 前端不需要额外跳浏览器收银台
- 第一阶段实现复杂度更低

## 系统职责划分

### CLIProxyApp

负责：

- 展示可购买套餐
- 发起下单请求
- 展示支付二维码
- 轮询订单状态
- 支付完成后刷新当前账号的 `plan_code`

### CLIProxyCloud

负责：

- 商品和价格管理
- 创建订单
- 调用微信支付和支付宝官方接口
- 处理支付回调
- 验签
- 做幂等支付确认
- 支付成功后更新用户订阅和套餐
- 提供 `admin` 的价格调整和订单管理能力

### CLIProxyApi

不参与支付。

## 商品模型

后端不要把商品写死成只有 `vip1` 和 `vip2`。

前端可以继续展示：

- `Pro`
- `Pro Max`

但后端应存成“商品 -> plan_code”的可配置映射。

例如：

- `pro_monthly` -> `vip1`
- `pro_max_monthly` -> `vip2`

后续可扩展：

- `pro_yearly`
- `pro_max_yearly`
- `enterprise`

## Admin 价格管理

这是必须要做的。

`admin` 后台应可管理：

- 商品名称
- 商品编码
- 显示名称
- 对应的 `plan_code`
- 价格（分）
- 币种
- 有效时长（天）
- 是否启用
- 排序
- 商品说明

也就是说，价格必须存数据库，不能写死在 `CLIProxyApp` 里。

## 数据库新增设计

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
- `payment_provider`（`wechat`、`alipay`）
- `amount`
- `currency`
- `status`（`pending`、`paid`、`closed`、`failed`、`refunded`）
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

### 后续预留：`refund_orders`

第一阶段可以先只预留，不急着实现。

## 服务端模块设计

建议在 `CLIProxyCloud` 中新增：

```text
internal/payments/
  provider.go
  service.go
  wechat/
  alipay/
```

### 支付 Provider 抽象

统一抽象出：

- `CreateOrder`
- `QueryOrder`
- `CloseOrder`
- `HandleNotify`

这样微信和支付宝可以独立实现，外层订单流程保持一致。

## 第一阶段 API 设计

### 用户侧

#### GET `/api/v1/pay/products`

返回当前可售商品列表，供 `CLIProxyApp` 展示。

#### POST `/api/v1/pay/orders`

请求体：

- `product_code`
- `provider`（`wechat` 或 `alipay`）

返回：

- 本地订单信息
- 二维码内容或支付链接
- 订单过期时间

#### GET `/api/v1/pay/orders/:orderNo`

返回订单当前状态。

供 `CLIProxyApp` 在展示二维码后轮询。

### 支付回调

#### POST `/api/v1/pay/wechat/notify`

微信支付官方回调。

#### POST `/api/v1/pay/alipay/notify`

支付宝官方回调。

### Admin 侧

#### GET `/api/v1/admin/pay/products`

查看商品和价格配置。

#### POST `/api/v1/admin/pay/products`

新增商品。

#### PATCH `/api/v1/admin/pay/products/:id`

调整商品价格、有效期、上下架状态。

#### GET `/api/v1/admin/pay/orders`

查看订单列表。

#### POST `/api/v1/admin/pay/orders/:orderNo/confirm`

后续可选，作为异常情况下的人工补单工具。

## 支付成功流程

1. `CLIProxyApp` 请求商品列表
2. 用户选择 `Pro` 或 `Pro Max`
3. `CLIProxyApp` 创建订单
4. `CLIProxyCloud` 调用微信/支付宝官方接口
5. `CLIProxyApp` 展示二维码
6. 用户支付
7. 支付平台回调 `CLIProxyCloud`
8. `CLIProxyCloud` 验签并确认支付成功
9. `CLIProxyCloud` 更新订单状态
10. `CLIProxyCloud` 更新用户订阅和 `plan_code`
11. `CLIProxyApp` 轮询订单状态或刷新 `/me`
12. 新权限立即生效

## 幂等要求

必须保证：

- 支付回调幂等
- 订单确认幂等
- 套餐升级幂等

因为微信和支付宝都可能重复发送回调。

## 回调域名要求

支付回调必须使用公网 `HTTPS` 域名。

建议：

- `https://pay.router-for.me`
- 或单独的支付子域名

不要依赖桌面端本地地址。

## 安全要求

- 商户私钥、API 密钥只放 `CLIProxyCloud`
- 不允许把支付私钥放进 `CLIProxyApp`
- 所有回调必须验签
- 所有支付回调要落日志
- admin 操作要可审计

## CLIProxyApp 后续界面方案

后续客户端可增加：

- `开通会员`
- `Pro / Pro Max` 商品卡片
- 支付渠道切换：`微信` / `支付宝`
- 二维码弹窗
- 订单状态轮询
- 支付成功后自动刷新套餐

## Admin 后续后台方案

后续 `admin` 页面可增加：

- 商品列表
- 价格调整
- 商品上下架
- 订单查询
- 手工补单

## 建议开发顺序

1. 先补数据库表
2. 先做 admin 商品管理
3. 再做下单接口
4. 先接微信 Native 支付
5. 做微信回调验签
6. 做支付成功后的套餐升级
7. 再把 `CLIProxyApp` 购买流程接上
8. 再接支付宝
9. 最后补 admin 订单管理

## 当前套餐名称映射

后端仍可以保持：

- `vip1` -> `Pro`
- `vip2` -> `Pro Max`

前端展示名和价格不应写死，而应由商品表控制。
