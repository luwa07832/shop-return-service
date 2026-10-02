package api

import (
	"net/http"
	"testing"
)

func TestRefundAfterFullyRefundedExceeds(t *testing.T) {
	s := newTestServer(t)
	r := createReturnRecord(t, s, validCreateBody)
	approveReturnRecord(t, s, r.ID, 100)

	first := s.do(t, http.MethodPost, "/v1/returns/"+r.ID+"/refunds", refundBody(100, "k1"))
	if first.Code != http.StatusOK {
		t.Fatalf("first = %d, body %s", first.Code, first.Body.String())
	}
	over := s.do(t, http.MethodPost, "/v1/returns/"+r.ID+"/refunds", refundBody(1, "k2"))
	assertError(t, over, http.StatusConflict, "refund_exceeds_returnable")

	dup := s.do(t, http.MethodPost, "/v1/returns/"+r.ID+"/refunds", refundBody(1, "k1"))
	assertError(t, dup, http.StatusConflict, "duplicate_refund")

	r2 := decodeBody[returnResp](t, s.do(t, http.MethodGet, "/v1/returns/"+r.ID, ""))
	if r2.Status != "refunded" || r2.RefundedAmount != 100 {
		t.Fatalf("history changed: %+v", r2)
	}
}
