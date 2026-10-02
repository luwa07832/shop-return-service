# shop-return-service

把退货申请、质检结论、部分退款金额与退款凭证记录成可查询的服务，支持按订单和退货单读取逆向进度，并追溯每一次退款的发起原因与处理结果。

## 运行要求

- Go 1.26 或以上
- SQLite（本服务自带存储，不需要外部数据库）

## 构建、测试与启动

```bash
go build ./...
go test ./...
go run .
```

服务默认监听 `127.0.0.1:8080`。可用环境变量覆盖：

| 变量 | 默认值 | 用途 |
|---|---|---|
| `ADDR` | `127.0.0.1:8080` | HTTP 监听地址 |
| `DB_PATH` | `shop-return-service.db` | SQLite 数据库文件路径 |

## 已公开的入口

### `GET /healthz`

返回服务与存储状态。正常时 HTTP 200：

```json
{"status":"ok","database":"ok"}
```

存储不可用时 HTTP 503：

```json
{"error":{"code":"storage_unavailable","message":"database is not available"}}
```

## 退货接口

### `POST /v1/returns`

提交退货申请。请求体：

```json
{
  "order_id": "ord_1",
  "reason": "商品破损",
  "evidence": ["voucher-1"],
  "items": [
    {"product_id": "sku-1", "quantity": 2},
    {"product_id": "sku-2", "quantity": 1}
  ]
}
```

- 每个条目都必须带非空 `product_id` 与正整数 `quantity`，同一 `product_id` 不可重复。
- 成功返回 201 与完整退货记录，初始状态为 `requested`。

### `GET /v1/returns/:id`

读取退货记录，成功返回 200；不存在返回 404 `return_not_found`。

### `POST /v1/returns/:id/approve`

质检通过：`quality_conclusion`（非空字符串）、`received_at`（RFC 3339 时间）与
`refundable_amount`（非负整数）必填。仅 `requested` 状态可处理，成功后状态为
`approved`，返回 200；重复处理返回 409 `return_already_processed`。

### `POST /v1/returns/:id/reject`

驳回退货：`reason` 必填且非空。规则与审核一致，成功后状态为 `rejected`。

退货状态限定为 `requested`、`approved`、`received`、`inspected`、`refunding`、
`refunded`、`rejected`、`closed`；未授权的迁移会被拒绝，历史记录不会被改写。

## 退款接口

### `POST /v1/returns/:id/refunds`

发起一次退款。请求体：

```json
{
  "amount": 1000,
  "reason": "部分退款",
  "evidence": ["refund-voucher"],
  "idempotency_key": "unique-key"
}
```

- `amount` 为正整数，`reason`、`evidence`（至少一个非空条目）、`idempotency_key` 均必填。
- 仅 `approved`（或已在 `refunding`）的退货单可发起，否则返回 409 `return_not_approved`。
- 超过剩余可退金额返回 409 `refund_exceeds_returnable`，同时追加一条 `failed`
  记录并保留失败原因；退货单的累计金额与状态不会被改写。
- 同一 `idempotency_key` 已有成功记录返回 409 `duplicate_refund`；仅有失败记录时
  可用同键重试。
- 累计成功退款达到可退金额时状态变为 `refunded`，否则保持 `refunding`。累计金额、
  余额校验与状态更新在同一个事务内完成。
- 退款记录只追加金额、状态、失败原因、幂等键与时间，不修改历史。

### `GET /v1/returns/:id/refunds`

按创建时间升序返回该退货单的全部退款记录（包含失败记录）：

```json
{"refunds":[{"refund_id":"ref_...","amount":1000,"status":"succeeded","idempotency_key":"k","created_at":"..."}]}
```

带 `?refund_id=ref_...` 读取单条记录；退货单不存在返回 404 `return_not_found`，
退款记录不存在返回 404 `refund_not_found`。

## 错误约定

所有错误响应都是单个顶层 `error` 对象，包含 `code` 与 `message` 两个字符串字段；`message` 不包含 SQL、堆栈或文件路径。

| HTTP | `code` |
|---|---|
| 400 | `invalid_return_input` |
| 404 | `return_not_found` / `refund_not_found` / `route_not_found` |
| 409 | `return_already_processed` / `return_not_approved` / `refund_exceeds_returnable` / `duplicate_refund` |
| 503 | `storage_unavailable` |

