package api

import (
	"net/http"
	"testing"
)

func TestCreateAndGetReturn(t *testing.T) {
	s := newTestServer(t)
	created := createReturnRecord(t, s, validCreateBody)

	if created.ID == "" {
		t.Fatal("return id is empty")
	}
	if created.Status != "requested" {
		t.Fatalf("status = %q", created.Status)
	}
	if created.OrderID != "ord_1" || created.Reason != "broken" {
		t.Fatalf("unexpected payload: %+v", created)
	}
	if len(created.Items) != 2 || created.Items[0].ProductID != "p1" || created.Items[0].Quantity != 2 {
		t.Fatalf("items mismatch: %+v", created.Items)
	}
	if len(created.Evidence) != 1 || created.Evidence[0] != "ev1" {
		t.Fatalf("evidence mismatch: %+v", created.Evidence)
	}

	rec := s.do(t, http.MethodGet, "/v1/returns/"+created.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d, body %s", rec.Code, rec.Body.String())
	}
	fetched := decodeBody[returnResp](t, rec)
	if fetched.ID != created.ID || len(fetched.Items) != 2 {
		t.Fatalf("fetched mismatch: %+v", fetched)
	}
}

func TestGetUnknownReturnIs404(t *testing.T) {
	s := newTestServer(t)
	rec := s.do(t, http.MethodGet, "/v1/returns/ret_missing", "")
	assertError(t, rec, http.StatusNotFound, "return_not_found")
}

func TestCreateReturnValidation(t *testing.T) {
	s := newTestServer(t)
	cases := map[string]string{
		"empty body":          ``,
		"not json":            `{"order_id":`,
		"missing order_id":    `{"items":[{"product_id":"p1","quantity":1}]}`,
		"blank order_id":      `{"order_id":"  ","items":[{"product_id":"p1","quantity":1}]}`,
		"missing items":       `{"order_id":"ord_1"}`,
		"empty items":         `{"order_id":"ord_1","items":[]}`,
		"missing product_id":  `{"order_id":"ord_1","items":[{"quantity":1}]}`,
		"blank product_id":    `{"order_id":"ord_1","items":[{"product_id":"","quantity":1}]}`,
		"missing quantity":    `{"order_id":"ord_1","items":[{"product_id":"p1"}]}`,
		"zero quantity":       `{"order_id":"ord_1","items":[{"product_id":"p1","quantity":0}]}`,
		"negative quantity":   `{"order_id":"ord_1","items":[{"product_id":"p1","quantity":-1}]}`,
		"fractional quantity": `{"order_id":"ord_1","items":[{"product_id":"p1","quantity":1.5}]}`,
		"duplicate product":   `{"order_id":"ord_1","items":[{"product_id":"p1","quantity":1},{"product_id":"p1","quantity":2}]}`,
		"wrong quantity type": `{"order_id":"ord_1","items":[{"product_id":"p1","quantity":"1"}]}`,
		"unknown field":       `{"order_id":"ord_1","items":[{"product_id":"p1","quantity":1}],"nope":1}`,
		"trailing token":      `{"order_id":"ord_1","items":[{"product_id":"p1","quantity":1}]}{}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := s.do(t, http.MethodPost, "/v1/returns", body)
			assertError(t, rec, http.StatusBadRequest, "invalid_return_input")
		})
	}
}

func TestApproveLifecycle(t *testing.T) {
	s := newTestServer(t)
	r := createReturnRecord(t, s, validCreateBody)

	body := `{"quality_conclusion":"resellable","received_at":"2026-09-30T10:00:00Z","refundable_amount":1000}`
	rec := s.do(t, http.MethodPost, "/v1/returns/"+r.ID+"/approve", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve status = %d, body %s", rec.Code, rec.Body.String())
	}
	approved := decodeBody[returnResp](t, rec)
	if approved.Status != "approved" || approved.RefundableAmount != 1000 {
		t.Fatalf("approved mismatch: %+v", approved)
	}
	if approved.ReceivedAt == nil || *approved.ReceivedAt != "2026-09-30T10:00:00Z" {
		t.Fatalf("received_at mismatch: %+v", approved.ReceivedAt)
	}

	again := s.do(t, http.MethodPost, "/v1/returns/"+r.ID+"/approve", body)
	assertError(t, again, http.StatusConflict, "return_already_processed")

	reject := s.do(t, http.MethodPost, "/v1/returns/"+r.ID+"/reject", `{"reason":"late"}`)
	assertError(t, reject, http.StatusConflict, "return_already_processed")

	get := s.do(t, http.MethodGet, "/v1/returns/"+r.ID, "")
	fetched := decodeBody[returnResp](t, get)
	if fetched.Status != "approved" {
		t.Fatalf("history was rewritten: status = %q", fetched.Status)
	}
}

func TestApproveValidationAndMissing(t *testing.T) {
	s := newTestServer(t)
	r := createReturnRecord(t, s, validCreateBody)
	cases := map[string]string{
		"missing conclusion":  `{"received_at":"2026-09-30T10:00:00Z","refundable_amount":10}`,
		"blank conclusion":    `{"quality_conclusion":" ","received_at":"2026-09-30T10:00:00Z","refundable_amount":10}`,
		"missing received_at": `{"quality_conclusion":"ok","refundable_amount":10}`,
		"bad received_at":     `{"quality_conclusion":"ok","received_at":"not-a-time","refundable_amount":10}`,
		"negative amount":     `{"quality_conclusion":"ok","received_at":"2026-09-30T10:00:00Z","refundable_amount":-1}`,
		"missing amount":      `{"quality_conclusion":"ok","received_at":"2026-09-30T10:00:00Z"}`,
		"fractional amount":   `{"quality_conclusion":"ok","received_at":"2026-09-30T10:00:00Z","refundable_amount":1.2}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := s.do(t, http.MethodPost, "/v1/returns/"+r.ID+"/approve", body)
			assertError(t, rec, http.StatusBadRequest, "invalid_return_input")
		})
	}

	missing := s.do(t, http.MethodPost, "/v1/returns/ret_nope/approve",
		`{"quality_conclusion":"ok","received_at":"2026-09-30T10:00:00Z","refundable_amount":10}`)
	assertError(t, missing, http.StatusNotFound, "return_not_found")
}

func TestRejectLifecycle(t *testing.T) {
	s := newTestServer(t)
	r := createReturnRecord(t, s, validCreateBody)

	rec := s.do(t, http.MethodPost, "/v1/returns/"+r.ID+"/reject", `{"reason":"not_ours"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("reject status = %d, body %s", rec.Code, rec.Body.String())
	}
	rejected := decodeBody[returnResp](t, rec)
	if rejected.Status != "rejected" || rejected.RejectReason != "not_ours" {
		t.Fatalf("rejected mismatch: %+v", rejected)
	}

	again := s.do(t, http.MethodPost, "/v1/returns/"+r.ID+"/reject", `{"reason":"x"}`)
	assertError(t, again, http.StatusConflict, "return_already_processed")
}

func TestRejectValidation(t *testing.T) {
	s := newTestServer(t)
	r := createReturnRecord(t, s, validCreateBody)
	for _, body := range []string{`{}`, `{"reason":""}`, `{"reason":"  "}`, `{"reason":5}`} {
		rec := s.do(t, http.MethodPost, "/v1/returns/"+r.ID+"/reject", body)
		assertError(t, rec, http.StatusBadRequest, "invalid_return_input")
	}
}
