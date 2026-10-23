package knowledge

import "database/sql"

// commitImportTx commits the current directory-import transaction and returns
// nil. Parse, OCR, image decode, and LLM card distillation run with no
// transaction checked out, so they do not occupy the store's only connection.
// A failed commit returns the original transaction so the caller can roll it
// back. The next SQL statement has to begin a new transaction.
func commitImportTx(tx *sql.Tx) (*sql.Tx, error) {
	if tx == nil {
		return nil, nil
	}
	if err := tx.Commit(); err != nil {
		return tx, err
	}
	return nil, nil
}
