package chat

// ReadReceiptService handles batch read-position updates.
type ReadReceiptService struct {
	store *Store
}

// NewReadReceiptService creates a ReadReceiptService.
func NewReadReceiptService(store *Store) *ReadReceiptService {
	return &ReadReceiptService{store: store}
}

// BatchUpdate processes a list of read receipts for a user.
func (s *ReadReceiptService) BatchUpdate(userID string, receipts []ReadReceipt) error {
	for _, r := range receipts {
		if err := s.store.UpdateReadSeq(r.ChannelID, userID, r.Seq); err != nil {
			return err
		}
	}
	return nil
}
