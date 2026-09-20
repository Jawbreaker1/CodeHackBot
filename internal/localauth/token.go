// Package localauth handles the separate credential used by local bridge clients.
package localauth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strings"
)

func LoopbackHost(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Read accepts a private file containing a generated 256-bit bearer token.
func Read(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open bridge token file: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 128 {
		return "", fmt.Errorf("bridge token must be a private regular file (chmod 600)")
	}
	buf := make([]byte, 128)
	n, err := f.Read(buf)
	if err != nil {
		return "", fmt.Errorf("read bridge token file: %w", err)
	}
	token := strings.TrimSpace(string(buf[:n]))
	decoded, err := hex.DecodeString(token)
	if err != nil || len(decoded) != 32 {
		return "", fmt.Errorf("invalid bridge token file")
	}
	return token, nil
}

// Create never overwrites an existing credential.
func Create(path string) error {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.WriteString(hex.EncodeToString(buf) + "\n")
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
