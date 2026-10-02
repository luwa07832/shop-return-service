// Package api wires the public HTTP surface and keeps every error response in
// the single top-level "error" shape published in README.md.
package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/luwa07832/shop-return-service/internal/store"
)

// NewRouter builds the HTTP router.
func NewRouter(st *store.Store) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())

	router.GET("/healthz", healthz(st))

	v1 := router.Group("/v1")
	{
		v1.POST("/returns", createReturn(st))
		v1.GET("/returns/:id", getReturn(st))
		v1.POST("/returns/:id/approve", approveReturn(st))
		v1.POST("/returns/:id/reject", rejectReturn(st))
		v1.POST("/returns/:id/refunds", createRefund(st))
		v1.GET("/returns/:id/refunds", listRefunds(st))
	}

	router.NoRoute(func(c *gin.Context) {
		writeError(c, errRouteNotFound)
	})
	return router
}

func healthz(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := st.Ping(); err != nil {
			writeError(c, errStorageUnavailable)
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "database": "ok"})
	}
}

func createReturn(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in createReturnInput
		if err := decodeJSONBody(c.Request.Body, &in); err != nil {
			writeError(c, asAPIError(err))
			return
		}
		valid, err := validateCreateReturn(&in)
		if err != nil {
			writeError(c, asAPIError(err))
			return
		}
		created, err := st.CreateReturn(c.Request.Context(), valid)
		if err != nil {
			writeStoreError(c, err)
			return
		}
		c.JSON(http.StatusCreated, created)
	}
}

func getReturn(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		r, err := st.GetReturn(c.Request.Context(), c.Param("id"))
		if err != nil {
			writeStoreError(c, err)
			return
		}
		c.JSON(http.StatusOK, r)
	}
}

func approveReturn(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in approveReturnInput
		if err := decodeJSONBody(c.Request.Body, &in); err != nil {
			writeError(c, asAPIError(err))
			return
		}
		conclusion, receivedAt, refundableAmount, err := validateApprove(&in)
		if err != nil {
			writeError(c, asAPIError(err))
			return
		}
		r, err := st.ApproveReturn(c.Request.Context(), c.Param("id"), conclusion, receivedAt, refundableAmount)
		if err != nil {
			writeStoreError(c, err)
			return
		}
		c.JSON(http.StatusOK, r)
	}
}

func rejectReturn(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in rejectReturnInput
		if err := decodeJSONBody(c.Request.Body, &in); err != nil {
			writeError(c, asAPIError(err))
			return
		}
		reason, err := validateReject(&in)
		if err != nil {
			writeError(c, asAPIError(err))
			return
		}
		r, err := st.RejectReturn(c.Request.Context(), c.Param("id"), reason)
		if err != nil {
			writeStoreError(c, err)
			return
		}
		c.JSON(http.StatusOK, r)
	}
}

func createRefund(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in createRefundInput
		if err := decodeJSONBody(c.Request.Body, &in); err != nil {
			writeError(c, asAPIError(err))
			return
		}
		valid, err := validateCreateRefund(&in)
		if err != nil {
			writeError(c, asAPIError(err))
			return
		}
		_, refund, storeErr := st.CreateRefund(c.Request.Context(), c.Param("id"), valid)
		if storeErr != nil {
			writeStoreError(c, storeErr)
			return
		}
		c.JSON(http.StatusOK, refund)
	}
}

func listRefunds(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		returnID := c.Param("id")
		if refundID := c.Query("refund_id"); refundID != "" {
			refund, err := st.GetRefund(c.Request.Context(), returnID, refundID)
			if err != nil {
				writeStoreError(c, err)
				return
			}
			c.JSON(http.StatusOK, refund)
			return
		}
		refunds, err := st.ListRefunds(c.Request.Context(), returnID)
		if err != nil {
			writeStoreError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"refunds": refunds})
	}
}

func writeError(c *gin.Context, e *apiError) {
	c.JSON(e.status, gin.H{"error": gin.H{"code": e.code, "message": e.message}})
}

func asAPIError(err error) *apiError {
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return errInternal
}

func writeStoreError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrReturnNotFound):
		writeError(c, errReturnNotFound)
	case errors.Is(err, store.ErrReturnAlreadyProcessed):
		writeError(c, errReturnProcessed)
	case errors.Is(err, store.ErrReturnNotApproved):
		writeError(c, errReturnNotApproved)
	case errors.Is(err, store.ErrRefundExceedsBalance):
		writeError(c, errRefundExceeds)
	case errors.Is(err, store.ErrDuplicateRefund):
		writeError(c, errDuplicateRefund)
	case errors.Is(err, store.ErrRefundNotFound):
		writeError(c, errRefundNotFound)
	default:
		writeError(c, errInternal)
	}
}
