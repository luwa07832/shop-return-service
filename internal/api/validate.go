package api

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/luwa07832/shop-return-service/internal/store"
)

type createReturnInput struct {
	OrderID  *string         `json:"order_id"`
	Items    *[]returnItemIn `json:"items"`
	Reason   *string         `json:"reason"`
	Evidence *[]string       `json:"evidence"`
}

type returnItemIn struct {
	ProductID *string `json:"product_id"`
	Quantity  *int64  `json:"quantity"`
}

type approveReturnInput struct {
	QualityConclusion *string `json:"quality_conclusion"`
	ReceivedAt        *string `json:"received_at"`
	RefundableAmount  *int64  `json:"refundable_amount"`
}

type rejectReturnInput struct {
	Reason *string `json:"reason"`
}

type createRefundInput struct {
	Amount         *int64    `json:"amount"`
	Reason         *string   `json:"reason"`
	Evidence       *[]string `json:"evidence"`
	IdempotencyKey *string   `json:"idempotency_key"`
}

// decodeJSONBody parses exactly one JSON value; trailing data, malformed JSON
// and wrong field types make the input invalid.
func decodeJSONBody(body io.Reader, dst any) error {
	raw, err := io.ReadAll(body)
	if err != nil {
		return errInvalidInput
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return errInvalidInput
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return errInvalidInput
	}
	if decoder.More() {
		return errInvalidInput
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return errInvalidInput
	}
	return nil
}

func validateCreateReturn(in *createReturnInput) (store.NewReturnInput, error) {
	out := store.NewReturnInput{}
	if in.OrderID == nil || strings.TrimSpace(*in.OrderID) == "" {
		return out, errInvalidInput
	}
	out.OrderID = *in.OrderID

	if in.Items == nil || len(*in.Items) == 0 {
		return out, errInvalidInput
	}
	seen := make(map[string]struct{}, len(*in.Items))
	items := make([]store.ReturnItem, 0, len(*in.Items))
	for _, item := range *in.Items {
		if item.ProductID == nil || strings.TrimSpace(*item.ProductID) == "" {
			return out, errInvalidInput
		}
		if item.Quantity == nil || *item.Quantity < 1 {
			return out, errInvalidInput
		}
		productID := *item.ProductID
		if _, dup := seen[productID]; dup {
			return out, errInvalidInput
		}
		seen[productID] = struct{}{}
		items = append(items, store.ReturnItem{ProductID: productID, Quantity: *item.Quantity})
	}
	out.Items = items

	if in.Reason != nil {
		out.Reason = *in.Reason
	}
	if in.Evidence != nil {
		evidence := make([]string, len(*in.Evidence))
		copy(evidence, *in.Evidence)
		out.Evidence = evidence
	} else {
		out.Evidence = []string{}
	}
	return out, nil
}

func validateApprove(in *approveReturnInput) (qualityConclusion, receivedAt string, refundableAmount int64, err error) {
	if in.QualityConclusion == nil || strings.TrimSpace(*in.QualityConclusion) == "" {
		return "", "", 0, errInvalidInput
	}
	if in.ReceivedAt == nil || strings.TrimSpace(*in.ReceivedAt) == "" {
		return "", "", 0, errInvalidInput
	}
	if _, parseErr := parseTimestamp(*in.ReceivedAt); parseErr != nil {
		return "", "", 0, errInvalidInput
	}
	if in.RefundableAmount == nil || *in.RefundableAmount < 0 {
		return "", "", 0, errInvalidInput
	}
	return *in.QualityConclusion, *in.ReceivedAt, *in.RefundableAmount, nil
}

func validateReject(in *rejectReturnInput) (string, error) {
	if in.Reason == nil || strings.TrimSpace(*in.Reason) == "" {
		return "", errInvalidInput
	}
	return *in.Reason, nil
}

func validateCreateRefund(in *createRefundInput) (store.RefundInput, error) {
	out := store.RefundInput{}
	if in.Amount == nil || *in.Amount < 1 {
		return out, errInvalidInput
	}
	out.Amount = *in.Amount
	if in.Reason == nil || strings.TrimSpace(*in.Reason) == "" {
		return out, errInvalidInput
	}
	out.Reason = *in.Reason
	if in.Evidence == nil || len(*in.Evidence) == 0 {
		return out, errInvalidInput
	}
	evidence := make([]string, 0, len(*in.Evidence))
	for _, entry := range *in.Evidence {
		if strings.TrimSpace(entry) == "" {
			return out, errInvalidInput
		}
		evidence = append(evidence, entry)
	}
	out.Evidence = evidence
	if in.IdempotencyKey == nil || strings.TrimSpace(*in.IdempotencyKey) == "" {
		return out, errInvalidInput
	}
	out.IdempotencyKey = *in.IdempotencyKey
	return out, nil
}

func parseTimestamp(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, errInvalidInput
}
