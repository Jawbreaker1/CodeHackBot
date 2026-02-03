package session

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

func NewID() string {
	ts := time.Now().UTC().Format("20060102-150405")
	buf := make([]byte, 2)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s-0000", ts)
	}
	return fmt.Sprintf("%s-%s", ts, hex.EncodeToString(buf))
}
