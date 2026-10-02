package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/luwa07832/shop-return-service/internal/store"
)

type createRefundRequest struct {
	Amount         int64    `json:"amount"`
	Reason         string   `json:"reason"`
	Evidence       []string `json:"evidence"`
	IdempotencyKey string   `json:"idempotency_key"`
}

func (h *handlers) createRefund(c *gin.Context) {
	var request createRefundRequest
	if err := decodeJSON(c, &request); err != nil {
		respondError(c, http.StatusBadRequest, "invalid_refund_input", invalidRefundMessage)
		return
	}
	if request.Amount <= 0 || !nonEmpty(request.Reason) || !validEvidence(request.Evidence) || !nonEmpty(request.IdempotencyKey) {
		respondError(c, http.StatusBadRequest, "invalid_refund_input", invalidRefundMessage)
		return
	}

	refund, err := h.store.CreateRefund(c.Request.Context(), c.Param("id"), store.NewRefundInput{
		Amount:         request.Amount,
		Reason:         request.Reason,
		Evidence:       request.Evidence,
		IdempotencyKey: request.IdempotencyKey,
	})
	if err != nil {
		respondStoreError(c, "create refund", err)
		return
	}
	c.JSON(http.StatusCreated, refundResponse(refund))
}

func (h *handlers) listRefunds(c *gin.Context) {
	returnID := c.Param("id")
	if refundID := c.Query("refund_id"); refundID != "" {
		refund, err := h.store.GetRefund(c.Request.Context(), returnID, refundID)
		if err != nil {
			respondStoreError(c, "get refund", err)
			return
		}
		c.JSON(http.StatusOK, refundResponse(refund))
		return
	}

	refunds, err := h.store.ListRefunds(c.Request.Context(), returnID)
	if err != nil {
		respondStoreError(c, "list refunds", err)
		return
	}
	items := make([]gin.H, 0, len(refunds))
	for index := range refunds {
		items = append(items, refundResponse(&refunds[index]))
	}
	c.JSON(http.StatusOK, gin.H{"return_id": returnID, "refunds": items})
}

func refundResponse(refund *store.Refund) gin.H {
	return gin.H{
		"refund_id":       refund.ID,
		"return_id":       refund.ReturnID,
		"amount":          refund.Amount,
		"reason":          refund.Reason,
		"evidence":        refund.Evidence,
		"status":          refund.Status,
		"failure_reason":  refund.FailureReason,
		"idempotency_key": refund.IdempotencyKey,
		"created_at":      refund.CreatedAt,
	}
}
