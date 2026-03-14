package bonus

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

func newTestService(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bonus.db")
	raw, err := sql.Open("libsql", "file:"+path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })

	queries := []string{
		`CREATE TABLE reason_codes (code TEXT PRIMARY KEY, operation TEXT NOT NULL, description TEXT NOT NULL);`,
		`INSERT INTO reason_codes (code, operation, description) VALUES
			('bonus.manual_grant', 'grant_bonus', 'Manual admin grant'),
			('transfer.user_to_user', 'transfer', 'User transfer'),
			('purchase.provider_charge', 'purchase', 'Provider purchase');`,
		`CREATE TABLE wallet_accounts (user_id INTEGER PRIMARY KEY, balance INTEGER NOT NULL DEFAULT 0);`,
		`CREATE TABLE transaction_journal (
			id TEXT PRIMARY KEY,
			operation TEXT NOT NULL,
			reason_code TEXT NOT NULL,
			idempotency_key TEXT,
			provider_ref TEXT,
			metadata TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);`,
		`CREATE UNIQUE INDEX idx_transaction_journal_idempotency ON transaction_journal(idempotency_key, operation) WHERE idempotency_key IS NOT NULL;`,
		`CREATE TABLE transaction_entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			journal_id TEXT NOT NULL,
			user_id INTEGER NOT NULL,
			entry_type TEXT NOT NULL,
			amount INTEGER NOT NULL
		);`,
	}
	for _, q := range queries {
		if _, err := raw.Exec(q); err != nil {
			t.Fatalf("setup schema: %v", err)
		}
	}

	if _, err := raw.Exec(`INSERT INTO wallet_accounts (user_id, balance) VALUES (?, ?)`, SystemAccountUserID, 1_000_000); err != nil {
		t.Fatalf("seed system wallet: %v", err)
	}
	return NewService(raw), raw
}

func balanceOf(t *testing.T, raw *sql.DB, userID int64) int64 {
	t.Helper()
	var balance int64
	if err := raw.QueryRow(`SELECT balance FROM wallet_accounts WHERE user_id = ?`, userID).Scan(&balance); err != nil {
		t.Fatalf("query balance: %v", err)
	}
	return balance
}

func TestGrantBonusIdempotency(t *testing.T) {
	t.Parallel()
	svc, raw := newTestService(t)

	metadata := map[string]string{"campaign": "welcome", "amount": "10"}
	if err := svc.GrantBonus("bonus.manual_grant", 42, "idem-key-1", metadata); err != nil {
		t.Fatalf("first grant: %v", err)
	}
	if err := svc.GrantBonus("bonus.manual_grant", 42, "idem-key-1", metadata); err != nil {
		t.Fatalf("second grant should be idempotent: %v", err)
	}

	if got := balanceOf(t, raw, 42); got != 10 {
		t.Fatalf("expected credited balance=10, got %d", got)
	}

	var txCount int
	if err := raw.QueryRow(`SELECT COUNT(*) FROM transaction_journal`).Scan(&txCount); err != nil {
		t.Fatalf("count journal: %v", err)
	}
	if txCount != 1 {
		t.Fatalf("expected 1 journal row for idempotent grant, got %d", txCount)
	}
}

func TestTransferPreventsNegativeBalance(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)

	err := svc.Transfer(100, 101, 50, "transfer.user_to_user")
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("expected insufficient funds error, got %v", err)
	}
}

func TestDoubleEntryDebitCreditInvariant(t *testing.T) {
	t.Parallel()
	svc, raw := newTestService(t)

	if err := svc.Transfer(SystemAccountUserID, 7, 30, "transfer.user_to_user"); err != nil {
		t.Fatalf("transfer: %v", err)
	}

	var debitTotal int64
	if err := raw.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM transaction_entries WHERE entry_type='debit'`).Scan(&debitTotal); err != nil {
		t.Fatalf("sum debit: %v", err)
	}
	var creditTotal int64
	if err := raw.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM transaction_entries WHERE entry_type='credit'`).Scan(&creditTotal); err != nil {
		t.Fatalf("sum credit: %v", err)
	}
	if debitTotal != creditTotal {
		t.Fatalf("expected debit total == credit total, got %d != %d", debitTotal, creditTotal)
	}
}
