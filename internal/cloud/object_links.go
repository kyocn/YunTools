package cloud

import (
	"context"
	"errors"
	"time"
)

const (
	DefaultObjectLinkExpiry = 300 * time.Second
	MaxObjectLinkExpiry     = 7 * 24 * time.Hour
)

// ObjectLink is an access URL, not an oss:// storage location. Signed URLs are
// sensitive bearer credentials and must not be persisted or written to logs.
type ObjectLink struct {
	URL       string
	Signed    bool
	ExpiresAt time.Time
	Reason    string // Concise explanation when public access is blocked or unknown.
}

// ObjectLinker is optional; object-storage providers need not support sharing.
// A zero expiry selects DefaultObjectLinkExpiry.
type ObjectLinker interface {
	ObjectLink(context.Context, string, string, time.Duration) (ObjectLink, error)
}

func ObjectLinkExpiry(expiry time.Duration) (time.Duration, error) {
	if expiry == 0 {
		return DefaultObjectLinkExpiry, nil
	}
	if expiry < time.Second || expiry > MaxObjectLinkExpiry || expiry%time.Second != 0 {
		return 0, errors.New("链接有效期必须为 1–604800 秒的整数")
	}
	return expiry, nil
}
