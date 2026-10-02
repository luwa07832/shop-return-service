package api

import (
	"net/http"
	"strconv"
	"testing"
)

func approveReturnRecord(t *testing.T, s *testServer, id string, amount int64) {
	t.Helper()
	body := `{"quality_conclusion":"resellable","received_at":"2026-09-30T10:00:00Z","refundable_amount":` +
		strconv.FormatInt(amount, 10) + `}`
	rec := s.do(t, http.MethodPost, "/v1/returns/"+id+"/approve", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve = %d, body %s", rec.Code, rec.Body.String())
	}
}

func refundBody(amount int64, key string) string {
	return `{"amount":` + strconv.FormatInt(amount, 10) +
		`,"reason":"partial","evidence":["proof"],"idempotency_key":"` + key + `"}`
}

func TestRefundHappyPath(t *testing.T) {
	s := newTestServer(t)
	r := createReturnRecord(t, s, validCreateBody)
	approveReturnRecord(t, s, r.ID, 1000)

	rec := s.do(t, http.MethodPost, "/v1/returns/"+r.ID+"/refunds", refundBody(400, "k1"))
	if rec.Code != http.StatusOK {
		t.Fatalf("refund status = %d, body %s", rec.Code, rec.Body.String())
	}
	first := decodeBody[refundResp](t, rec)
	if first.RefundID == "" || first.Amount != 400 || first.Status != "succeeded" ||
		first.FailureReason != "" || first.IdempotencyKey != "k1" || first.CreatedAt == "" {
		t.Fatalf("refund mismatch: %+v", first)
	}
	r2 := decodeBody[returnResp](t, s.do(t, http.MethodGet, "/v1/returns/"+r.ID, ""))
	if r2.Status != "refunding" || r2.RefundedAmount != 400 {
		t.Fatalf("return mismatch: %+v", r2)
	}

	second := s.do(t, http.MethodPost, "/v1/returns/"+r.ID+"/refunds", refundBody(600, "k2"))
	if second.Code != http.StatusOK {
		t.Fatalf("second refund = %d, body %s", second.Code, second.Body.String())
	}
	r3 := decodeBody[returnResp](t, s.do(t, http.MethodGet, "/v1/returns/"+r.ID, ""))
	if r3.Status != "refunded" || r3.RefundedAmount != 1000 {
		t.Fatalf("refunded mismatch: %+v", r3)
	}

	listRec := s.do(t, http.MethodGet, "/v1/returns/"+r.ID+"/refunds", "")
	if listRec.Code != http.StatusOK {
		t.Fatalf("list = %d", listRec.Code)
	}
	var list struct {
		Refunds []refundResp `json:"refunds"`
	}
	list = decodeBody[struct {
		Refunds []refundResp `json:"refunds"`
	}](t, listRec)
	if len(list.Refunds) != 2 || list.Refunds[0].Amount != 400 || list.Refunds[1].Amount != 600 {
		t.Fatalf("list order/content mismatch: %+v", list)
	}

	one := s.do(t, http.MethodGet, "/v1/returns/"+r.ID+"/refunds?refund_id="+first.RefundID, "")
	if one.Code != http.StatusOK {
		t.Fatalf("get one = %d, body %s", one.Code, one.Body.String())
	}
	single := decodeBody[refundResp](t, one)
	if single.RefundID != first.RefundID {
		t.Fatalf("single mismatch: %+v", single)
	}

	missing := s.do(t, http.MethodGet, "/v1/returns/"+r.ID+"/refunds?refund_id=ref_nope", "")
	assertError(t, missing, http.StatusNotFound, "refund_not_found")
}

func TestRefundNotApproved(t *testing.T) {
	s := newTestServer(t)
	r := createReturnRecord(t, s, validCreateBody)

	rec := s.do(t, http.MethodPost, "/v1/returns/"+r.ID+"/refunds", refundBody(1, "k1"))
	assertError(t, rec, http.StatusConflict, "return_not_approved")

	rec = s.do(t, http.MethodPost, "/v1/returns/ret_missing/refunds", refundBody(1, "k1"))
	assertError(t, rec, http.StatusNotFound, "return_not_found")

	rejected := createReturnRecord(t, s, `{"order_id":"ord_2","items":[{"product_id":"p1","quantity":1}]}`)
	s.do(t, http.MethodPost, "/v1/returns/"+rejected.ID+"/reject", `{"reason":"x"}`)
	rec = s.do(t, http.MethodPost, "/v1/returns/"+rejected.ID+"/refunds", refundBody(1, "k1"))
	assertError(t, rec, http.StatusConflict, "return_not_approved")
}

func TestRefundExceedsBalanceRecordsFailure(t *testing.T) {
	s := newTestServer(t)
	r := createReturnRecord(t, s, validCreateBody)
	approveReturnRecord(t, s, r.ID, 100)

	over := s.do(t, http.MethodPost, "/v1/returns/"+r.ID+"/refunds", refundBody(150, "k1"))
	assertError(t, over, http.StatusConflict, "refund_exceeds_returnable")

	r2 := decodeBody[returnResp](t, s.do(t, http.MethodGet, "/v1/returns/"+r.ID, ""))
	if r2.Status != "approved" || r2.RefundedAmount != 0 {
		t.Fatalf("failed attempt rewrote return: %+v", r2)
	}

	list := decodeBody[struct {
		Refunds []refundResp `json:"refunds"`
	}](t, s.do(t, http.MethodGet, "/v1/returns/"+r.ID+"/refunds", ""))
	if len(list.Refunds) != 1 || list.Refunds[0].Status != "failed" ||
		list.Refunds[0].FailureReason == "" || list.Refunds[0].Amount != 150 {
		t.Fatalf("failed record mismatch: %+v", list)
	}

	retry := s.do(t, http.MethodPost, "/v1/returns/"+r.ID+"/refunds", refundBody(60, "k1"))
	if retry.Code != http.StatusOK {
		t.Fatalf("retry with same key = %d, body %s", retry.Code, retry.Body.String())
	}
	r3 := decodeBody[returnResp](t, s.do(t, http.MethodGet, "/v1/returns/"+r.ID, ""))
	if r3.Status != "refunding" || r3.RefundedAmount != 60 {
		t.Fatalf("retry mismatch: %+v", r3)
	}

	all := decodeBody[struct {
		Refunds []refundResp `json:"refunds"`
	}](t, s.do(t, http.MethodGet, "/v1/returns/"+r.ID+"/refunds", ""))
	if len(all.Refunds) != 2 || all.Refunds[0].Status != "failed" || all.Refunds[1].Status != "succeeded" {
		t.Fatalf("append-only history mismatch: %+v", all)
	}
}

func TestDuplicateSucceededKey(t *testing.T) {
	s := newTestServer(t)
	r := createReturnRecord(t, s, validCreateBody)
	approveReturnRecord(t, s, r.ID, 1000)

	first := s.do(t, http.MethodPost, "/v1/returns/"+r.ID+"/refunds", refundBody(100, "dup"))
	if first.Code != http.StatusOK {
		t.Fatalf("first = %d", first.Code)
	}
	dup := s.do(t, http.MethodPost, "/v1/returns/"+r.ID+"/refunds", refundBody(100, "dup"))
	assertError(t, dup, http.StatusConflict, "duplicate_refund")

	r2 := decodeBody[returnResp](t, s.do(t, http.MethodGet, "/v1/returns/"+r.ID, ""))
	if r2.RefundedAmount != 100 {
		t.Fatalf("duplicate changed balance: %d", r2.RefundedAmount)
	}
}

func TestRefundValidation(t *testing.T) {
	s := newTestServer(t)
	r := createReturnRecord(t, s, validCreateBody)
	approveReturnRecord(t, s, r.ID, 1000)
	cases := map[string]string{
		"zero amount":      `{"amount":0,"reason":"x","evidence":["e"],"idempotency_key":"k"}`,
		"negative amount":  `{"amount":-5,"reason":"x","evidence":["e"],"idempotency_key":"k"}`,
		"missing reason":   `{"amount":1,"evidence":["e"],"idempotency_key":"k"}`,
		"blank reason":     `{"amount":1,"reason":"  ","evidence":["e"],"idempotency_key":"k"}`,
		"missing evidence": `{"amount":1,"reason":"x","idempotency_key":"k"}`,
		"empty evidence":   `{"amount":1,"reason":"x","evidence":[],"idempotency_key":"k"}`,
		"blank evidence":   `{"amount":1,"reason":"x","evidence":["  "],"idempotency_key":"k"}`,
		"missing key":      `{"amount":1,"reason":"x","evidence":["e"]}`,
		"blank key":        `{"amount":1,"reason":"x","evidence":["e"],"idempotency_key":""}`,
		"fraction amount":  `{"amount":1.1,"reason":"x","evidence":["e"],"idempotency_key":"k"}`,
		"not json":         `{`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := s.do(t, http.MethodPost, "/v1/returns/"+r.ID+"/refunds", body)
			assertError(t, rec, http.StatusBadRequest, "invalid_return_input")
		})
	}
}

func TestRefundListUnknownReturn(t *testing.T) {
	s := newTestServer(t)
	rec := s.do(t, http.MethodGet, "/v1/returns/ret_missing/refunds", "")
	assertError(t, rec, http.StatusNotFound, "return_not_found")
}
