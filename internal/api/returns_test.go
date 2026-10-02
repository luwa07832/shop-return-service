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

type testServer struct {
	router http.Handler
	store  *store.Store
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return &testServer{router: NewRouter(st), store: st}
}

func (s *testServer) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != "" {
		reader = bytes.NewReader([]byte(body))
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	return rec
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return out
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func assertError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body %s", rec.Code, status, rec.Body.String())
	}
	got := decodeBody[errorBody](t, rec)
	if got.Error.Code != code {
		t.Fatalf("code = %q, want %q; body %s", got.Error.Code, code, rec.Body.String())
	}
	if got.Error.Message == "" {
		t.Fatalf("error message is empty: %s", rec.Body.String())
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &top); err != nil || len(top) != 1 {
		t.Fatalf("error response must have one top-level key: %s", rec.Body.String())
	}
}

type returnResp struct {
	ID       string   `json:"id"`
	OrderID  string   `json:"order_id"`
	Status   string   `json:"status"`
	Reason   string   `json:"reason"`
	Evidence []string `json:"evidence"`
	Items    []struct {
		ProductID string `json:"product_id"`
		Quantity  int64  `json:"quantity"`
	} `json:"items"`
	RefundableAmount  int64   `json:"refundable_amount"`
	RefundedAmount    int64   `json:"refunded_amount"`
	QualityConclusion string  `json:"quality_conclusion"`
	ReceivedAt        *string `json:"received_at"`
	RejectReason      string  `json:"reject_reason"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}

type refundResp struct {
	RefundID       string `json:"refund_id"`
	Amount         int64  `json:"amount"`
	Status         string `json:"status"`
	FailureReason  string `json:"failure_reason"`
	IdempotencyKey string `json:"idempotency_key"`
	CreatedAt      string `json:"created_at"`
}

func createReturnRecord(t *testing.T, s *testServer, body string) returnResp {
	t.Helper()
	rec := s.do(t, http.MethodPost, "/v1/returns", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", rec.Code, rec.Body.String())
	}
	return decodeBody[returnResp](t, rec)
}

const validCreateBody = `{"order_id":"ord_1","reason":"broken","evidence":["ev1"],
"items":[{"product_id":"p1","quantity":2},{"product_id":"p2","quantity":1}]}`
