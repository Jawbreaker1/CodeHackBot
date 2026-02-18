package orchestrator

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

func NewEventID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("evt-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("evt-%d-%s", time.Now().UnixNano(), hex.EncodeToString(b[:]))
}
