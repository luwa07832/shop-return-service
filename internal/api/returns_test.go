package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/luwa07832/shop-return-service/internal/store"
)

func newTestRouter(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return NewRouter(st), st
}

func doJSON(t *testing.T, router http.Handler, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	switch value := body.(type) {
	case nil:
		reader = bytes.NewReader(nil)
	case string:
		reader = bytes.NewReader([]byte(value))
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, target, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func decodeBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
	return decoded
}

func assertErrorShape(t *testing.T, recorder *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if recorder.Code != wantStatus {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, wantStatus, recorder.Body.String())
	}
	decoded := decodeBody(t, recorder)
	errorValue, ok := decoded["error"].(map[string]any)
	if !ok {
		t.Fatalf("error object missing in %s", recorder.Body.String())
	}
	if len(decoded) != 1 {
		t.Fatalf("response has extra top-level fields: %s", recorder.Body.String())
	}
	code, _ := errorValue["code"].(string)
	message, _ := errorValue["message"].(string)
	if code != wantCode {
		t.Fatalf("code = %q, want %q", code, wantCode)
	}
	if message == "" {
		t.Fatalf("message must be a non-empty string: %s", recorder.Body.String())
	}
}

func validReturnBody() map[string]any {
	return map[string]any{
		"order_id": "ORD-1",
		"items": []map[string]any{
			{"product_id": "SKU-1", "quantity": 1},
			{"product_id": "SKU-2", "quantity": 3},
		},
		"reason":   "broken on arrival",
		"evidence": []string{"receipt-1", "photo-1"},
	}
}

func createReturn(t *testing.T, router http.Handler, body any) string {
	t.Helper()
	recorder := doJSON(t, router, http.MethodPost, "/v1/returns", body)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body = %s", recorder.Code, recorder.Body.String())
	}
	return decodeBody(t, recorder)["id"].(string)
}

func TestCreateReturnAndReadBack(t *testing.T) {
	router, _ := newTestRouter(t)

	id := createReturn(t, router, validReturnBody())

	recorder := doJSON(t, router, http.MethodGet, "/v1/returns/"+id, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	record := decodeBody(t, recorder)
	if record["id"] != id {
		t.Fatalf("id = %v", record["id"])
	}
	if record["status"] != "requested" {
		t.Fatalf("status = %v, want requested", record["status"])
	}
	if record["order_id"] != "ORD-1" || record["reason"] != "broken on arrival" {
		t.Fatalf("unexpected record: %v", record)
	}
	items := record["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items = %v", items)
	}
	first := items[0].(map[string]any)
	if first["product_id"] != "SKU-1" || first["quantity"].(float64) != 1 {
		t.Fatalf("first item = %v", first)
	}
	if record["refundable_amount"].(float64) != 0 || record["quality_conclusion"] != "" {
		t.Fatalf("unexpected default fields: %v", record)
	}
	if record["received_at"] != nil {
		t.Fatalf("received_at = %v, want null", record["received_at"])
	}
}

func TestCreateReturnRejectsInvalidInput(t *testing.T) {
	router, _ := newTestRouter(t)

	cases := []any{
		nil,
		"not-json",
		map[string]any{"items": []map[string]any{{"product_id": "SKU-1", "quantity": 1}}},
		map[string]any{"order_id": "ORD-1", "items": []map[string]any{{"product_id": "SKU-1", "quantity": 1}}},
		map[string]any{"order_id": "ORD-1", "reason": "x", "evidence": []string{"e"}, "items": []any{}},
		map[string]any{"order_id": "ORD-1", "reason": "x", "evidence": []string{"e"}, "items": []map[string]any{{"product_id": "SKU-1", "quantity": 0}}},
		map[string]any{"order_id": "ORD-1", "reason": "x", "evidence": []string{"e"}, "items": []map[string]any{{"product_id": "SKU-1", "quantity": 1.5}}},
		map[string]any{"order_id": "ORD-1", "reason": "x", "evidence": []string{"e"}, "items": []map[string]any{{"product_id": "SKU-1", "quantity": -1}}},
		map[string]any{"order_id": "ORD-1", "reason": "x", "evidence": []string{"e"}, "items": []map[string]any{{"product_id": "SKU-1", "quantity": 1}, {"product_id": "SKU-1", "quantity": 2}}},
		map[string]any{"order_id": "ORD-1", "reason": "x", "evidence": []string{"e"}, "items": []map[string]any{{"product_id": "", "quantity": 1}}},
		map[string]any{"order_id": "ORD-1", "reason": "x", "evidence": []string{}, "items": []map[string]any{{"product_id": "SKU-1", "quantity": 1}}},
	}
	for index, body := range cases {
		recorder := doJSON(t, router, http.MethodPost, "/v1/returns", body)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("case %d: status = %d, body = %s", index, recorder.Code, recorder.Body.String())
		}
		assertErrorShape(t, recorder, http.StatusBadRequest, "invalid_return_input")
	}
}

func TestReturnNotFound(t *testing.T) {
	router, _ := newTestRouter(t)
	assertErrorShape(t, doJSON(t, router, http.MethodGet, "/v1/returns/missing", nil), http.StatusNotFound, "return_not_found")
}

func approveBody() map[string]any {
	return map[string]any{
		"quality_conclusion": "refurbishable",
		"received_at":        "2026-09-30T10:00:00Z",
		"refundable_amount":  100,
	}
}

func TestApproveLifecycle(t *testing.T) {
	router, _ := newTestRouter(t)
	id := createReturn(t, router, validReturnBody())

	recorder := doJSON(t, router, http.MethodPost, "/v1/returns/"+id+"/approve", approveBody())
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	record := decodeBody(t, recorder)
	if record["status"] != "approved" {
		t.Fatalf("status = %v", record["status"])
	}
	if record["quality_conclusion"] != "refurbishable" || record["refundable_amount"].(float64) != 100 {
		t.Fatalf("unexpected record: %v", record)
	}
	if record["received_at"] != "2026-09-30T10:00:00Z" {
		t.Fatalf("received_at = %v", record["received_at"])
	}

	assertErrorShape(t,
		doJSON(t, router, http.MethodPost, "/v1/returns/"+id+"/approve", approveBody()),
		http.StatusConflict, "return_already_processed")
	assertErrorShape(t,
		doJSON(t, router, http.MethodPost, "/v1/returns/"+id+"/reject", map[string]any{"reason": "late"}),
		http.StatusConflict, "return_already_processed")
}

func TestApproveValidationAndMissingReturn(t *testing.T) {
	router, _ := newTestRouter(t)
	id := createReturn(t, router, validReturnBody())

	badBodies := []any{
		nil,
		map[string]any{"received_at": "2026-09-30T10:00:00Z", "refundable_amount": 100},
		map[string]any{"quality_conclusion": "x", "received_at": "2026-09-30T10:00:00Z", "refundable_amount": -1},
		map[string]any{"quality_conclusion": "x", "received_at": "not-a-time", "refundable_amount": 1},
		map[string]any{"quality_conclusion": "x", "refundable_amount": 1},
	}
	for index, body := range badBodies {
		assertErrorShape(t,
			doJSON(t, router, http.MethodPost, "/v1/returns/"+id+"/approve", body),
			http.StatusBadRequest, "invalid_return_input")
		_ = index
	}
	assertErrorShape(t,
		doJSON(t, router, http.MethodPost, "/v1/returns/missing/approve", approveBody()),
		http.StatusNotFound, "return_not_found")
}

func TestRejectLifecycle(t *testing.T) {
	router, _ := newTestRouter(t)
	id := createReturn(t, router, validReturnBody())

	recorder := doJSON(t, router, http.MethodPost, "/v1/returns/"+id+"/reject", map[string]any{"reason": "customer fault"})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if status := decodeBody(t, recorder)["status"]; status != "rejected" {
		t.Fatalf("status = %v", status)
	}

	assertErrorShape(t,
		doJSON(t, router, http.MethodPost, "/v1/returns/"+id+"/reject", map[string]any{"reason": "again"}),
		http.StatusConflict, "return_already_processed")
	assertErrorShape(t,
		doJSON(t, router, http.MethodPost, "/v1/returns/"+id+"/refunds", map[string]any{
			"amount": 1, "reason": "x", "evidence": []string{"e"}, "idempotency_key": "k1",
		}),
		http.StatusConflict, "return_not_approved")
}

func TestRejectValidation(t *testing.T) {
	router, _ := newTestRouter(t)
	id := createReturn(t, router, validReturnBody())
	assertErrorShape(t,
		doJSON(t, router, http.MethodPost, "/v1/returns/"+id+"/reject", map[string]any{"reason": ""}),
		http.StatusBadRequest, "invalid_return_input")
}
