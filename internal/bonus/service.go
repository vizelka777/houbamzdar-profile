package bonus

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

const SystemAccountUserID int64 = 0

var (
	ErrInvalidAmount      = errors.New("amount must be positive")
	ErrInsufficientFunds  = errors.New("insufficient funds")
	ErrInvalidReasonCode  = errors.New("invalid reason code for operation")
	ErrMissingIdempotency = errors.New("idempotency key is required")
)

type EntryType string

const (
	EntryDebit  EntryType = "debit"
	EntryCredit EntryType = "credit"
)

type LedgerEntry struct {
	UserID int64
	Type   EntryType
	Amount int64
}

type ApplyRequest struct {
	Operation      string
	ReasonCode     string
	IdempotencyKey string
	ProviderRef    string
	Metadata       map[string]string
	Entries        []LedgerEntry
}

type Service struct {
	db *sql.DB
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

func (s *Service) GrantBonus(reasonCode string, userID int64, idempotencyKey string, metadata map[string]string) error {
	if idempotencyKey == "" {
		return ErrMissingIdempotency
	}
	amount, err := amountFromMetadata(metadata)
	if err != nil {
		return err
	}
	_, err = s.ApplyTransactionTx(ApplyRequest{
		Operation:      "grant_bonus",
		ReasonCode:     reasonCode,
		IdempotencyKey: idempotencyKey,
		Metadata:       metadata,
		Entries: []LedgerEntry{
			{UserID: SystemAccountUserID, Type: EntryDebit, Amount: amount},
			{UserID: userID, Type: EntryCredit, Amount: amount},
		},
	})
	return err
}

func amountFromMetadata(metadata map[string]string) (int64, error) {
	if metadata == nil || metadata["amount"] == "" {
		return 1, nil
	}
	var amount int64
	if _, err := fmt.Sscanf(metadata["amount"], "%d", &amount); err != nil {
		return 0, ErrInvalidAmount
	}
	if amount <= 0 {
		return 0, ErrInvalidAmount
	}
	return amount, nil
}

func (s *Service) Transfer(fromUser, toUser, amount int64, reasonCode string) error {
	_, err := s.ApplyTransactionTx(ApplyRequest{
		Operation:  "transfer",
		ReasonCode: reasonCode,
		Entries: []LedgerEntry{
			{UserID: fromUser, Type: EntryDebit, Amount: amount},
			{UserID: toUser, Type: EntryCredit, Amount: amount},
		},
	})
	return err
}

func (s *Service) Purchase(userID, amount int64, providerRef string) error {
	if providerRef == "" {
		return ErrMissingIdempotency
	}
	_, err := s.ApplyTransactionTx(ApplyRequest{
		Operation:      "purchase",
		ReasonCode:     "purchase.provider_charge",
		IdempotencyKey: providerRef,
		ProviderRef:    providerRef,
		Entries: []LedgerEntry{
			{UserID: userID, Type: EntryDebit, Amount: amount},
			{UserID: SystemAccountUserID, Type: EntryCredit, Amount: amount},
		},
	})
	return err
}

func (s *Service) ApplyTransactionTx(req ApplyRequest) (bool, error) {
	if len(req.Entries) < 2 {
		return false, errors.New("at least two entries are required")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	if req.IdempotencyKey != "" {
		var existing string
		err = tx.QueryRow(
			`SELECT id FROM transaction_journal WHERE idempotency_key = ? AND operation = ?`,
			req.IdempotencyKey,
			req.Operation,
		).Scan(&existing)
		if err == nil {
			if commitErr := tx.Commit(); commitErr != nil {
				return false, commitErr
			}
			return false, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
	}

	if err := validateReasonTx(tx, req.Operation, req.ReasonCode); err != nil {
		return false, err
	}

	if err := validateEntries(req.Entries); err != nil {
		return false, err
	}

	for _, entry := range req.Entries {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO wallet_accounts (user_id, balance) VALUES (?, 0)`, entry.UserID); err != nil {
			return false, err
		}
	}

	balances := map[int64]int64{}
	for _, entry := range req.Entries {
		delta := entry.Amount
		if entry.Type == EntryDebit {
			delta = -delta
		}
		balances[entry.UserID] += delta
	}

	for userID, delta := range balances {
		if delta >= 0 || userID == SystemAccountUserID {
			continue
		}
		var balance int64
		if err := tx.QueryRow(`SELECT balance FROM wallet_accounts WHERE user_id = ?`, userID).Scan(&balance); err != nil {
			return false, err
		}
		if balance+delta < 0 {
			return false, ErrInsufficientFunds
		}
	}

	for userID, delta := range balances {
		if _, err := tx.Exec(`UPDATE wallet_accounts SET balance = balance + ? WHERE user_id = ?`, delta, userID); err != nil {
			return false, err
		}
	}

	metadataJSON := "{}"
	if len(req.Metadata) > 0 {
		bytes, err := json.Marshal(req.Metadata)
		if err != nil {
			return false, err
		}
		metadataJSON = string(bytes)
	}

	journalID := uuid.NewString()
	if _, err := tx.Exec(`
		INSERT INTO transaction_journal (id, operation, reason_code, idempotency_key, provider_ref, metadata)
		VALUES (?, ?, ?, ?, ?, ?)
	`, journalID, req.Operation, req.ReasonCode, nullIfEmpty(req.IdempotencyKey), nullIfEmpty(req.ProviderRef), metadataJSON); err != nil {
		return false, err
	}

	for _, entry := range req.Entries {
		if _, err := tx.Exec(`
			INSERT INTO transaction_entries (journal_id, user_id, entry_type, amount)
			VALUES (?, ?, ?, ?)
		`, journalID, entry.UserID, string(entry.Type), entry.Amount); err != nil {
			return false, err
		}
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func validateReasonTx(tx *sql.Tx, operation, reasonCode string) error {
	var count int
	if err := tx.QueryRow(
		`SELECT COUNT(*) FROM reason_codes WHERE code = ? AND operation = ?`,
		reasonCode,
		operation,
	).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("%w: %s/%s", ErrInvalidReasonCode, operation, reasonCode)
	}
	return nil
}

func validateEntries(entries []LedgerEntry) error {
	var totalDebit int64
	var totalCredit int64
	for _, entry := range entries {
		if entry.Amount <= 0 {
			return ErrInvalidAmount
		}
		switch entry.Type {
		case EntryDebit:
			totalDebit += entry.Amount
		case EntryCredit:
			totalCredit += entry.Amount
		default:
			return errors.New("entry type must be debit or credit")
		}
	}
	if totalDebit != totalCredit {
		return errors.New("debit and credit totals must match")
	}
	return nil
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
