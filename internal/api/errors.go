package api

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/luwa07832/shop-return-service/internal/store"
)

// invalidInput is a local sentinel for validation failures; every public
// validation error shares the invalid_return_input / invalid_refund_input code.
var errInvalidInput = errors.New("invalid input")

func respondError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}

// respondStoreError maps storage errors to the published contract. Unexpected
// errors become a generic 500; their text never reaches the client.
func respondStoreError(c *gin.Context, action string, err error) {
	switch {
	case errors.Is(err, store.ErrReturnNotFound):
		respondError(c, http.StatusNotFound, "return_not_found", "the return does not exist")
	case errors.Is(err, store.ErrRefundNotFound):
		respondError(c, http.StatusNotFound, "refund_not_found", "the refund does not exist")
	case errors.Is(err, store.ErrAlreadyProcessed):
		respondError(c, http.StatusConflict, "return_already_processed", "the return has already been processed")
	case errors.Is(err, store.ErrReturnNotApproved):
		respondError(c, http.StatusConflict, "return_not_approved", "the return has not been approved for refund")
	case errors.Is(err, store.ErrRefundExceedsBalance):
		respondError(c, http.StatusConflict, "refund_exceeds_returnable", "the refund exceeds the remaining refundable amount")
	case errors.Is(err, store.ErrDuplicateRefund):
		respondError(c, http.StatusConflict, "duplicate_refund", "a successful refund with this idempotency key already exists")
	default:
		log.Printf("%s: %v", action, err)
		respondError(c, http.StatusInternalServerError, "internal_error", "an unexpected error occurred")
	}
}
