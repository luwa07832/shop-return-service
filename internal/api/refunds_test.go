package api

import (
	"net/http"
	"testing"
)

func refundBody(amount int64, key string) map[string]any {
	return map[string]any{
		"amount":          amount,
		"reason":          "partial goodwill refund",
		"evidence":        []string{"approval-note"},
		"idempotency_key": key,
	}
}

func approvedReturn(t *testing.T, router http.Handler, refundable int64) string {
	t.Helper()
	id := createReturn(t, router, validReturnBody())
	body := approveBody()
	body["refundable_amount"] = refundable
	recorder := doJSON(t, router, http.MethodPost, "/v1/returns/"+id+"/approve", body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("approve status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	return id
}

func TestCreateRefundKeepsRefundingUntilFullyRefunded(t *testing.T) {
	router, _ := newTestRouter(t)
	id := approvedReturn(t, router, 100)

	first := doJSON(t, router, http.MethodPost, "/v1/returns/"+id+"/refunds", refundBody(40, "k1"))
	if first.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", first.Code, first.Body.String())
	}
	firstRefund := decodeBody(t, first)
	if firstRefund["status"] != "succeeded" || firstRefund["amount"].(float64) != 40 {
		t.Fatalf("unexpected refund: %v", firstRefund)
	}
	if firstRefund["idempotency_key"] != "k1" || firstRefund["return_id"] != id {
		t.Fatalf("unexpected refund: %v", firstRefund)
	}

	record := decodeBody(t, doJSON(t, router, http.MethodGet, "/v1/returns/"+id, nil))
	if record["status"] != "refunding" {
		t.Fatalf("return status = %v, want refunding", record["status"])
	}

	second := doJSON(t, router, http.MethodPost, "/v1/returns/"+id+"/refunds", refundBody(60, "k2"))
	if second.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", second.Code, second.Body.String())
	}
	record = decodeBody(t, doJSON(t, router, http.MethodGet, "/v1/returns/"+id, nil))
	if record["status"] != "refunded" {
		t.Fatalf("return status = %v, want refunded", record["status"])
	}

	overdraw := doJSON(t, router, http.MethodPost, "/v1/returns/"+id+"/refunds", refundBody(1, "k3"))
	assertErrorShape(t, overdraw, http.StatusConflict, "refund_exceeds_returnable")
}

func TestRefundExceedsReturnable(t *testing.T) {
	router, _ := newTestRouter(t)
	id := approvedReturn(t, router, 10)
	assertErrorShape(t,
		doJSON(t, router, http.MethodPost, "/v1/returns/"+id+"/refunds", refundBody(11, "k1")),
		http.StatusConflict, "refund_exceeds_returnable")

	// The failed attempt must not move the return out of approved or consume balance.
	record := decodeBody(t, doJSON(t, router, http.MethodGet, "/v1/returns/"+id, nil))
	if record["status"] != "approved" {
		t.Fatalf("status = %v, want approved", record["status"])
	}
	retry := doJSON(t, router, http.MethodPost, "/v1/returns/"+id+"/refunds", refundBody(10, "k2"))
	if retry.Code != http.StatusCreated {
		t.Fatalf("retry status = %d, body = %s", retry.Code, retry.Body.String())
	}
}

func TestDuplicateRefundKey(t *testing.T) {
	router, _ := newTestRouter(t)
	id := approvedReturn(t, router, 100)

	doJSON(t, router, http.MethodPost, "/v1/returns/"+id+"/refunds", refundBody(30, "dup"))
	duplicate := doJSON(t, router, http.MethodPost, "/v1/returns/"+id+"/refunds", refundBody(30, "dup"))
	assertErrorShape(t, duplicate, http.StatusConflict, "duplicate_refund")

	// Duplicate rejection must not move money twice.
	record := decodeBody(t, doJSON(t, router, http.MethodGet, "/v1/returns/"+id, nil))
	if record["status"] != "refunding" {
		t.Fatalf("status = %v, want refunding", record["status"])
	}
	list := decodeBody(t, doJSON(t, router, http.MethodGet, "/v1/returns/"+id+"/refunds", nil))
	if len(list["refunds"].([]any)) != 1 {
		t.Fatalf("refunds = %v", list["refunds"])
	}
}

func TestRefundRequiresApproval(t *testing.T) {
	router, _ := newTestRouter(t)
	id := createReturn(t, router, validReturnBody())
	assertErrorShape(t,
		doJSON(t, router, http.MethodPost, "/v1/returns/"+id+"/refunds", refundBody(1, "k1")),
		http.StatusConflict, "return_not_approved")
}

func TestRefundInvalidInput(t *testing.T) {
	router, _ := newTestRouter(t)
	id := approvedReturn(t, router, 100)

	cases := []any{
		nil,
		map[string]any{"reason": "x", "evidence": []string{"e"}, "idempotency_key": "k"},
		map[string]any{"amount": 0, "reason": "x", "evidence": []string{"e"}, "idempotency_key": "k"},
		map[string]any{"amount": -5, "reason": "x", "evidence": []string{"e"}, "idempotency_key": "k"},
		map[string]any{"amount": 5, "evidence": []string{"e"}, "idempotency_key": "k"},
		map[string]any{"amount": 5, "reason": "x", "idempotency_key": "k"},
		map[string]any{"amount": 5, "reason": "x", "evidence": []string{"e"}},
	}
	for index, body := range cases {
		recorder := doJSON(t, router, http.MethodPost, "/v1/returns/"+id+"/refunds", body)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("case %d: status = %d, body = %s", index, recorder.Code, recorder.Body.String())
		}
		assertErrorShape(t, recorder, http.StatusBadRequest, "invalid_refund_input")
	}
}

func TestListAndGetRefunds(t *testing.T) {
	router, _ := newTestRouter(t)
	id := approvedReturn(t, router, 100)

	first := doJSON(t, router, http.MethodPost, "/v1/returns/"+id+"/refunds", refundBody(20, "k1"))
	firstID := decodeBody(t, first)["refund_id"].(string)
	doJSON(t, router, http.MethodPost, "/v1/returns/"+id+"/refunds", refundBody(30, "k2"))

	listRecorder := doJSON(t, router, http.MethodGet, "/v1/returns/"+id+"/refunds", nil)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("status = %d", listRecorder.Code)
	}
	list := decodeBody(t, listRecorder)
	if list["return_id"] != id {
		t.Fatalf("return_id = %v", list["return_id"])
	}
	refunds := list["refunds"].([]any)
	if len(refunds) != 2 {
		t.Fatalf("refunds = %v", refunds)
	}
	// Ascending creation order.
	if refunds[0].(map[string]any)["amount"].(float64) != 20 {
		t.Fatalf("first refund = %v", refunds[0])
	}
	if refunds[1].(map[string]any)["amount"].(float64) != 30 {
		t.Fatalf("second refund = %v", refunds[1])
	}

	singleRecorder := doJSON(t, router, http.MethodGet, "/v1/returns/"+id+"/refunds?refund_id="+firstID, nil)
	if singleRecorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", singleRecorder.Code, singleRecorder.Body.String())
	}
	single := decodeBody(t, singleRecorder)
	if single["refund_id"] != firstID || single["amount"].(float64) != 20 {
		t.Fatalf("single refund = %v", single)
	}

	assertErrorShape(t,
		doJSON(t, router, http.MethodGet, "/v1/returns/"+id+"/refunds?refund_id=missing", nil),
		http.StatusNotFound, "refund_not_found")
	assertErrorShape(t,
		doJSON(t, router, http.MethodGet, "/v1/returns/missing/refunds", nil),
		http.StatusNotFound, "return_not_found")
}
