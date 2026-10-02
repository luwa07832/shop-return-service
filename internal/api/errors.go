package api

import "net/http"

// Published error codes and messages. Messages must never expose SQL, stack
// traces or filesystem paths.
const (
	codeInvalidInput       = "invalid_return_input"
	codeReturnNotFound     = "return_not_found"
	codeReturnProcessed    = "return_already_processed"
	codeReturnNotApproved  = "return_not_approved"
	codeRefundExceeds      = "refund_exceeds_returnable"
	codeDuplicateRefund    = "duplicate_refund"
	codeRefundNotFound     = "refund_not_found"
	codeStorageUnavailable = "storage_unavailable"
	codeInternal           = "internal_error"
	codeRouteNotFound      = "route_not_found"
)

type apiError struct {
	status  int
	code    string
	message string
}

func (e *apiError) Error() string { return e.code }

func newAPIError(status int, code, message string) *apiError {
	return &apiError{status: status, code: code, message: message}
}

var (
	errInvalidInput       = newAPIError(http.StatusBadRequest, codeInvalidInput, "the request input is invalid")
	errReturnNotFound     = newAPIError(http.StatusNotFound, codeReturnNotFound, "the return was not found")
	errReturnProcessed    = newAPIError(http.StatusConflict, codeReturnProcessed, "the return has already been processed")
	errReturnNotApproved  = newAPIError(http.StatusConflict, codeReturnNotApproved, "the return is not approved")
	errRefundExceeds      = newAPIError(http.StatusConflict, codeRefundExceeds, "the refund exceeds the returnable amount")
	errDuplicateRefund    = newAPIError(http.StatusConflict, codeDuplicateRefund, "a successful refund with this idempotency key already exists")
	errRefundNotFound     = newAPIError(http.StatusNotFound, codeRefundNotFound, "the refund was not found")
	errStorageUnavailable = newAPIError(http.StatusServiceUnavailable, codeStorageUnavailable, "database is not available")
	errInternal           = newAPIError(http.StatusInternalServerError, codeInternal, "an internal error occurred")
	errRouteNotFound      = newAPIError(http.StatusNotFound, codeRouteNotFound, "no route matches this path")
)
