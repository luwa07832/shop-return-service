package store

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func seedApproved(t *testing.T, st *Store, refundable int64) string {
	t.Helper()
	created, err := st.CreateReturn(context.Background(), NewReturnInput{
		OrderID:  "ORD-1",
		Reason:   "broken",
		Evidence: []string{"photo"},
		Items:    []ItemInput{{ProductID: "SKU-1", Quantity: 2}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := st.ApproveReturn(context.Background(), created.ID, "ok", "2026-09-30T10:00:00Z", int64(refundable)); err != nil {
		t.Fatalf("approve: %v", err)
	}
	return created.ID
}

func refundInput(amount int64, key string) NewRefundInput {
	return NewRefundInput{
		Amount:         amount,
		Reason:         "goodwill",
		Evidence:       []string{"note"},
		IdempotencyKey: key,
	}
}

func TestFailedRefundCanRetryWithSameKey(t *testing.T) {
	st := newTestStore(t)
	id := seedApproved(t, st, 100)

	failed, err := st.RecordFailedRefund(context.Background(), id, refundInput(40, "k1"), "gateway timeout")
	if err != nil {
		t.Fatalf("record failed: %v", err)
	}
	if failed.Status != RefundFailed || failed.FailureReason != "gateway timeout" {
		t.Fatalf("unexpected failed refund: %+v", failed)
	}

	// The failed attempt consumed no balance and did not change the status.
	record, err := st.GetReturn(context.Background(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if record.Status != StatusApproved {
		t.Fatalf("status = %s, want approved", record.Status)
	}

	succeeded, err := st.CreateRefund(context.Background(), id, refundInput(40, "k1"))
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if succeeded.Status != RefundSucceeded {
		t.Fatalf("status = %s, want succeeded", succeeded.Status)
	}

	// Once a success exists the key is locked even though a failed record precedes it.
	if _, err := st.CreateRefund(context.Background(), id, refundInput(10, "k1")); !errors.Is(err, ErrDuplicateRefund) {
		t.Fatalf("err = %v, want ErrDuplicateRefund", err)
	}

	refunds, err := st.ListRefunds(context.Background(), id)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(refunds) != 2 || refunds[0].Status != RefundFailed || refunds[1].Status != RefundSucceeded {
		t.Fatalf("append-only history broken: %+v", refunds)
	}
}

func TestFullRefundMarksReturnRefunded(t *testing.T) {
	st := newTestStore(t)
	id := seedApproved(t, st, 100)

	if _, err := st.CreateRefund(context.Background(), id, refundInput(60, "a")); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := st.CreateRefund(context.Background(), id, refundInput(40, "b")); err != nil {
		t.Fatalf("second: %v", err)
	}
	record, _ := st.GetReturn(context.Background(), id)
	if record.Status != StatusRefunded {
		t.Fatalf("status = %s, want refunded", record.Status)
	}
}

func TestConcurrentRefundsNeverOverdraw(t *testing.T) {
	st := newTestStore(t)
	id := seedApproved(t, st, 100)

	const attempts = 8
	var wg sync.WaitGroup
	errs := make([]error, attempts)
	wg.Add(attempts)
	for index := 0; index < attempts; index++ {
		go func(index int) {
			defer wg.Done()
			_, errs[index] = st.CreateRefund(context.Background(), id, refundInput(20, "concurrent-"+string(rune('a'+index))))
		}(index)
	}
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
			continue
		}
		if !errors.Is(err, ErrRefundExceedsBalance) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if succeeded != 5 {
		t.Fatalf("succeeded = %d, want 5", succeeded)
	}

	refunds, _ := st.ListRefunds(context.Background(), id)
	total := int64(0)
	for _, refund := range refunds {
		total += refund.Amount
	}
	if total != 100 {
		t.Fatalf("total refunded = %d, want 100", total)
	}
	record, _ := st.GetReturn(context.Background(), id)
	if record.Status != StatusRefunded {
		t.Fatalf("status = %s, want refunded", record.Status)
	}
}

func TestConcurrentApprovalOnlyOneWins(t *testing.T) {
	st := newTestStore(t)
	id := seedApproved(t, st, 100)
	_ = id

	created, err := st.CreateReturn(context.Background(), NewReturnInput{
		OrderID:  "ORD-2",
		Reason:   "other",
		Evidence: []string{"photo"},
		Items:    []ItemInput{{ProductID: "SKU-2", Quantity: 1}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = st.ApproveReturn(context.Background(), created.ID, "ok", "2026-09-30T10:00:00Z", 5)
	}()
	go func() { defer wg.Done(); _, errs[1] = st.RejectReturn(context.Background(), created.ID, "no") }()
	wg.Wait()

	processed := 0
	for _, err := range errs {
		if err == nil {
			processed++
			continue
		}
		if !errors.Is(err, ErrAlreadyProcessed) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if processed != 1 {
		t.Fatalf("processed = %d, want 1", processed)
	}
	record, _ := st.GetReturn(context.Background(), created.ID)
	if record.Status != StatusApproved && record.Status != StatusRejected {
		t.Fatalf("status = %s", record.Status)
	}
}
