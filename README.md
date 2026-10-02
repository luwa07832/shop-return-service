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

### `POST /v1/returns`

提交退货申请。`items` 每行必须有非空 `product_id` 与正整数 `quantity`，同一商品不可重复；
`order_id`、`reason`、`evidence` 均必填，凭证为非空字符串数组。成功返回 HTTP 201，
新退货单状态为 `requested`。

```json
{
  "order_id": "ORD-1001",
  "reason": "到货破损",
  "evidence": ["receipt-1", "photo-1"],
  "items": [
    {"product_id": "SKU-1", "quantity": 1},
    {"product_id": "SKU-2", "quantity": 2}
  ]
}
```

### `GET /v1/returns/:id`

读取退货单及其商品行，成功返回 HTTP 200；不存在返回 404 `return_not_found`。

### `POST /v1/returns/:id/approve`

记录质检审核结论。`quality_conclusion`、RFC 3339 格式的 `received_at` 必填，
`refundable_amount` 为非负整数。只有 `requested` 状态可处理，成功后状态为 `approved`，
返回 HTTP 200；重复审核返回 409 `return_already_processed`。

### `POST /v1/returns/:id/reject`

驳回退货单。`reason` 必填。只有 `requested` 状态可处理，成功后状态为 `rejected`，
返回 HTTP 200；重复审核返回 409 `return_already_processed`。

### `POST /v1/returns/:id/refunds`

追加一笔退款。`amount` 为正整数，`reason`、`evidence`、`idempotency_key` 均非空。
只有审核通过（`approved`，以及进行中的 `refunding`）的退货单可以发起退款；未审核通过
返回 409 `return_not_approved`，金额超过剩余可退余额返回 409 `refund_exceeds_returnable`。

退款记录只追加金额、状态、失败原因、幂等键与时间，从不改写历史。累计成功金额、余额校验与
退货单状态更新在同一个数据库事务内完成：累计达到可退金额时退货单改为 `refunded`，否则保持
`refunding`。同一 `idempotency_key` 已有成功记录返回 409 `duplicate_refund`；失败记录保留
`failure_reason`，可使用同一个键重试。成功返回 HTTP 201。

### `GET /v1/returns/:id/refunds`

按创建时间升序返回退货单下的全部退款记录。带 `?refund_id=<id>` 查询参数时返回单条退款记录；
退款不存在返回 404 `refund_not_found`。

## 退货单状态

`requested`、`approved`、`received`、`inspected`、`refunding`、`refunded`、`rejected`、`closed`。
审核接口只允许 `requested -> approved|rejected`，退款只允许从已审核通过的状态推进；任何未授权
的状态迁移都不会改写既有记录。

## 错误约定

所有错误响应都是单个顶层 `error` 对象，包含 `code` 与 `message` 两个字符串字段；`message`
不包含 SQL、堆栈或文件路径。

| HTTP | code | 场景 |
|---|---|---|
| 400 | `invalid_return_input` | 创建退货或审核/驳回输入非法 |
| 400 | `invalid_refund_input` | 创建退款输入非法 |
| 404 | `return_not_found` | 退货单不存在 |
| 404 | `refund_not_found` | 退款记录不存在 |
| 409 | `return_already_processed` | 退货单已被处理，重复审核 |
| 409 | `return_not_approved` | 退货单未审核通过即发起退款 |
| 409 | `refund_exceeds_returnable` | 退款金额超出剩余可退余额 |
| 409 | `duplicate_refund` | 同幂等键已有成功退款 |
| 404 | `route_not_found` | 路径不匹配任何入口 |
| 503 | `storage_unavailable` | 健康检查时存储不可用 |
