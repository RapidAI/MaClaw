package main

import (
	"encoding/base64"
	"fmt"
	"time"
)

// ImageOutputSizeLimit is the maximum decoded image size for SDK output images (desktop → mobile).
const ImageOutputSizeLimit = 10 * 1024 * 1024 // 10 MB

// ImageUploadSizeLimit is the maximum decoded image size for user-uploaded images (mobile → desktop).
const ImageUploadSizeLimit = 5 * 1024 * 1024 // 5 MB

// validImageMediaTypes is the set of accepted image MIME types.
var validImageMediaTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

// IsValidImageMediaType returns true if mediaType is a supported image MIME type.
func IsValidImageMediaType(mediaType string) bool {
	return validImageMediaTypes[mediaType]
}

// ValidateImageTransferMessage checks that msg has a non-empty session_id,
// a valid media_type, non-empty data, and that the base64-decoded data does
// not exceed sizeLimit. Use ImageOutputSizeLimit for desktop→mobile images
// and ImageUploadSizeLimit for mobile→desktop uploads.
func ValidateImageTransferMessage(msg ImageTransferMessage, sizeLimit int) error {
	if msg.SessionID == "" {
		return fmt.Errorf("image transfer: session_id is required")
	}
	if !IsValidImageMediaType(msg.MediaType) {
		return fmt.Errorf("image transfer: unsupported media_type %q", msg.MediaType)
	}
	if msg.Data == "" {
		return fmt.Errorf("image transfer: data is required")
	}
	decoded, err := base64.StdEncoding.DecodeString(msg.Data)
	if err != nil {
		return fmt.Errorf("image transfer: invalid base64 data: %w", err)
	}
	if len(decoded) > sizeLimit {
		return fmt.Errorf("image transfer: decoded size %d exceeds limit %d", len(decoded), sizeLimit)
	}
	return nil
}

// NewImageTransferMessage constructs an ImageTransferMessage with an
// auto-generated image_id (format: img_{UnixNano}) and current timestamp.
func NewImageTransferMessage(sessionID, mediaType, data string) ImageTransferMessage {
	now := time.Now()
	return ImageTransferMessage{
		ImageID:   fmt.Sprintf("img_%d", now.UnixNano()),
		SessionID: sessionID,
		MediaType: mediaType,
		Data:      data,
		Timestamp: now.Unix(),
	}
}

// ExceedsImageSizeLimit checks whether the base64-encoded data would exceed
// sizeLimit when decoded, using a length estimate instead of a full decode.
// This avoids allocating a multi-MB buffer just to check the size.
func ExceedsImageSizeLimit(base64Data string, sizeLimit int) bool {
	// Standard base64: every 4 chars encode 3 bytes.
	n := len(base64Data)
	if n == 0 {
		return false
	}
	pad := 0
	if base64Data[n-1] == '=' {
		pad++
		if n > 1 && base64Data[n-2] == '=' {
			pad++
		}
	}
	decodedSize := (n/4)*3 - pad
	return decodedSize > sizeLimit
}
