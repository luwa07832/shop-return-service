package store

import (
	"encoding/json"
)

// ReturnItem is one product line in a return request.
type ReturnItem struct {
	ProductID string `json:"product_id"`
	Quantity  int64  `json:"quantity"`
}

// Return is the stored record of a return request.
type Return struct {
	ID                string       `json:"id"`
	OrderID           string       `json:"order_id"`
	Status            string       `json:"status"`
	Reason            string       `json:"reason"`
	Evidence          []string     `json:"evidence"`
	Items             []ReturnItem `json:"items"`
	RefundableAmount  int64        `json:"refundable_amount"`
	RefundedAmount    int64        `json:"refunded_amount"`
	QualityConclusion string       `json:"quality_conclusion"`
	ReceivedAt        *string      `json:"received_at,omitempty"`
	RejectReason      string       `json:"reject_reason,omitempty"`
	CreatedAt         string       `json:"created_at"`
	UpdatedAt         string       `json:"updated_at"`
}

// Refund is one append-only refund record.
type Refund struct {
	RefundID       string `json:"refund_id"`
	ReturnID       string `json:"-"`
	Amount         int64  `json:"amount"`
	Status         string `json:"status"`
	FailureReason  string `json:"failure_reason,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
	CreatedAt      string `json:"created_at"`
}

// NewReturnInput is the validated payload for creating a return.
type NewReturnInput struct {
	OrderID  string
	Items    []ReturnItem
	Reason   string
	Evidence []string
}

// RefundInput is the validated payload for initiating a refund.
type RefundInput struct {
	Amount         int64
	Reason         string
	Evidence       []string
	IdempotencyKey string
}

func encodeEvidence(evidence []string) (string, error) {
	if evidence == nil {
		evidence = []string{}
	}
	raw, err := json.Marshal(evidence)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func decodeEvidence(raw string, target *[]string) error {
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		return err
	}
	if *target == nil {
		*target = []string{}
	}
	return nil
}
