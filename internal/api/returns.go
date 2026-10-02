package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/luwa07832/shop-return-service/internal/store"
)

type handlers struct {
	store *store.Store
}

const invalidReturnMessage = "the return input is invalid"
const invalidRefundMessage = "the refund input is invalid"

// decodeJSON reads a single JSON object. A missing or malformed body, a value
// of the wrong type or a non-positive integer are all rejected by the decoder
// or by validation; callers only need to report the same 400 code.
func decodeJSON(c *gin.Context, dest any) error {
	decoder := json.NewDecoder(c.Request.Body)
	if err := decoder.Decode(dest); err != nil {
		if err == io.EOF {
			return errInvalidInput
		}
		return errInvalidInput
	}
	return nil
}

func nonEmpty(value string) bool { return strings.TrimSpace(value) != "" }

func validEvidence(evidence []string) bool {
	if len(evidence) == 0 {
		return false
	}
	for _, item := range evidence {
		if !nonEmpty(item) {
			return false
		}
	}
	return true
}

type createReturnRequest struct {
	OrderID  string            `json:"order_id"`
	Items    []returnItemInput `json:"items"`
	Reason   string            `json:"reason"`
	Evidence []string          `json:"evidence"`
}

type returnItemInput struct {
	ProductID string `json:"product_id"`
	Quantity  int64  `json:"quantity"`
}

func (h *handlers) createReturn(c *gin.Context) {
	var request createReturnRequest
	if err := decodeJSON(c, &request); err != nil {
		respondError(c, http.StatusBadRequest, "invalid_return_input", invalidReturnMessage)
		return
	}

	if !nonEmpty(request.OrderID) || !nonEmpty(request.Reason) || !validEvidence(request.Evidence) {
		respondError(c, http.StatusBadRequest, "invalid_return_input", invalidReturnMessage)
		return
	}
	if len(request.Items) == 0 {
		respondError(c, http.StatusBadRequest, "invalid_return_input", invalidReturnMessage)
		return
	}

	input := store.NewReturnInput{
		OrderID:  request.OrderID,
		Reason:   request.Reason,
		Evidence: request.Evidence,
		Items:    make([]store.ItemInput, 0, len(request.Items)),
	}
	seenProducts := make(map[string]struct{}, len(request.Items))
	for _, item := range request.Items {
		if !nonEmpty(item.ProductID) || item.Quantity <= 0 {
			respondError(c, http.StatusBadRequest, "invalid_return_input", invalidReturnMessage)
			return
		}
		if _, duplicated := seenProducts[item.ProductID]; duplicated {
			respondError(c, http.StatusBadRequest, "invalid_return_input", invalidReturnMessage)
			return
		}
		seenProducts[item.ProductID] = struct{}{}
		input.Items = append(input.Items, store.ItemInput{ProductID: item.ProductID, Quantity: item.Quantity})
	}

	record, err := h.store.CreateReturn(c.Request.Context(), input)
	if err != nil {
		respondStoreError(c, "create return", err)
		return
	}
	c.JSON(http.StatusCreated, returnResponse(record))
}

func (h *handlers) getReturn(c *gin.Context) {
	record, err := h.store.GetReturn(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondStoreError(c, "get return", err)
		return
	}
	c.JSON(http.StatusOK, returnResponse(record))
}

type approveReturnRequest struct {
	QualityConclusion string `json:"quality_conclusion"`
	ReceivedAt        string `json:"received_at"`
	RefundableAmount  int64  `json:"refundable_amount"`
}

func (h *handlers) approveReturn(c *gin.Context) {
	var request approveReturnRequest
	if err := decodeJSON(c, &request); err != nil {
		respondError(c, http.StatusBadRequest, "invalid_return_input", invalidReturnMessage)
		return
	}
	if !nonEmpty(request.QualityConclusion) || !nonEmpty(request.ReceivedAt) || request.RefundableAmount < 0 {
		respondError(c, http.StatusBadRequest, "invalid_return_input", invalidReturnMessage)
		return
	}
	receivedAt, err := time.Parse(time.RFC3339, request.ReceivedAt)
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_return_input", invalidReturnMessage)
		return
	}

	record, err := h.store.ApproveReturn(
		c.Request.Context(),
		c.Param("id"),
		request.QualityConclusion,
		receivedAt.Format(time.RFC3339),
		request.RefundableAmount,
	)
	if err != nil {
		respondStoreError(c, "approve return", err)
		return
	}
	c.JSON(http.StatusOK, returnResponse(record))
}

type rejectReturnRequest struct {
	Reason string `json:"reason"`
}

func (h *handlers) rejectReturn(c *gin.Context) {
	var request rejectReturnRequest
	if err := decodeJSON(c, &request); err != nil {
		respondError(c, http.StatusBadRequest, "invalid_return_input", invalidReturnMessage)
		return
	}
	if !nonEmpty(request.Reason) {
		respondError(c, http.StatusBadRequest, "invalid_return_input", invalidReturnMessage)
		return
	}
	record, err := h.store.RejectReturn(c.Request.Context(), c.Param("id"), request.Reason)
	if err != nil {
		respondStoreError(c, "approve return", err)
		return
	}
	c.JSON(http.StatusOK, returnResponse(record))
}

type returnItemResponse struct {
	ProductID string `json:"product_id"`
	Quantity  int64  `json:"quantity"`
}

func returnResponse(record *store.Return) gin.H {
	items := make([]returnItemResponse, 0, len(record.Items))
	for _, item := range record.Items {
		items = append(items, returnItemResponse{ProductID: item.ProductID, Quantity: item.Quantity})
	}
	var receivedAt any
	if record.ReceivedAt != "" {
		receivedAt = record.ReceivedAt
	}
	return gin.H{
		"id":                 record.ID,
		"order_id":           record.OrderID,
		"status":             record.Status,
		"reason":             record.Reason,
		"evidence":           record.Evidence,
		"quality_conclusion": record.QualityConclusion,
		"received_at":        receivedAt,
		"refundable_amount":  record.RefundableAmount,
		"reject_reason":      record.RejectReason,
		"items":              items,
		"created_at":         record.CreatedAt,
		"updated_at":         record.UpdatedAt,
	}
}
