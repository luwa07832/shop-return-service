package store

import "errors"

// Return statuses published by the service.
const (
	StatusRequested = "requested"
	StatusApproved  = "approved"
	StatusReceived  = "received"
	StatusInspected = "inspected"
	StatusRefunding = "refunding"
	StatusRefunded  = "refunded"
	StatusRejected  = "rejected"
	StatusClosed    = "closed"
)

// Refund statuses published by the service.
const (
	RefundSucceeded = "succeeded"
	RefundFailed    = "failed"
)

// Sentinel errors the storage layer returns. The HTTP layer maps each one to a
// published error code, so callers never need to interpret SQL errors.
var (
	ErrReturnNotFound       = errors.New("return not found")
	ErrRefundNotFound       = errors.New("refund not found")
	ErrAlreadyProcessed     = errors.New("return has already been processed")
	ErrReturnNotApproved    = errors.New("return has not been approved for refund")
	ErrRefundExceedsBalance = errors.New("refund amount exceeds the remaining refundable balance")
	ErrDuplicateRefund      = errors.New("a successful refund with this idempotency key already exists")
)

// ItemInput is one line of a return request.
type ItemInput struct {
	ProductID string `json:"product_id"`
	Quantity  int64  `json:"quantity"`
}

// NewReturnInput carries the data for POST /v1/returns.
type NewReturnInput struct {
	OrderID  string      `json:"order_id"`
	Items    []ItemInput `json:"items"`
	Reason   string      `json:"reason"`
	Evidence []string    `json:"evidence"`
}

// ReturnItem is one line as stored with a return.
type ReturnItem struct {
	ProductID string `json:"product_id"`
	Quantity  int64  `json:"quantity"`
}

// Return is a return record as persisted.
type Return struct {
	ID                string       `json:"id"`
	OrderID           string       `json:"order_id"`
	Status            string       `json:"status"`
	Reason            string       `json:"reason"`
	Evidence          []string     `json:"evidence"`
	QualityConclusion string       `json:"quality_conclusion"`
	ReceivedAt        string       `json:"received_at"`
	RefundableAmount  int64        `json:"refundable_amount"`
	RejectReason      string       `json:"reject_reason"`
	Items             []ReturnItem `json:"items"`
	CreatedAt         string       `json:"created_at"`
	UpdatedAt         string       `json:"updated_at"`
}

// NewRefundInput carries the data for POST /v1/returns/:id/refunds.
type NewRefundInput struct {
	Amount         int64    `json:"amount"`
	Reason         string   `json:"reason"`
	Evidence       []string `json:"evidence"`
	IdempotencyKey string   `json:"idempotency_key"`
}

// Refund is a refund record as persisted. Records are append-only.
type Refund struct {
	ID             string   `json:"refund_id"`
	ReturnID       string   `json:"return_id"`
	Amount         int64    `json:"amount"`
	Reason         string   `json:"reason"`
	Evidence       []string `json:"evidence"`
	Status         string   `json:"status"`
	FailureReason  string   `json:"failure_reason"`
	IdempotencyKey string   `json:"idempotency_key"`
	CreatedAt      string   `json:"created_at"`
}
