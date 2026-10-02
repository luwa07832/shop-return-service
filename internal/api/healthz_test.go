package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/luwa07832/shop-return-service/internal/store"
)

func TestHealthzReportsStorageUnavailableWhenClosed(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	NewRouter(st).ServeHTTP(recorder, request)

	assertError(t, recorder, http.StatusServiceUnavailable, "storage_unavailable")
}
