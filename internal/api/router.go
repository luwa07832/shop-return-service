package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/luwa07832/shop-return-service/internal/store"
)

// NewRouter wires the public HTTP surface. Every entry keeps the error shape
// described in README.md: a single top-level error object with string code and
// message, never leaking SQL, stacks or file paths.
func NewRouter(st *store.Store) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())

	h := &handlers{store: st}

	router.GET("/healthz", h.healthz)

	router.POST("/v1/returns", h.createReturn)
	router.GET("/v1/returns/:id", h.getReturn)
	router.POST("/v1/returns/:id/approve", h.approveReturn)
	router.POST("/v1/returns/:id/reject", h.rejectReturn)
	router.POST("/v1/returns/:id/refunds", h.createRefund)
	router.GET("/v1/returns/:id/refunds", h.listRefunds)

	router.NoRoute(func(c *gin.Context) {
		respondError(c, http.StatusNotFound, "route_not_found", "no route matches this path")
	})
	return router
}

func (h *handlers) healthz(c *gin.Context) {
	if err := h.store.Ping(); err != nil {
		respondError(c, http.StatusServiceUnavailable, "storage_unavailable", "database is not available")
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "database": "ok"})
}
