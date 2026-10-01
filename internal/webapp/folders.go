package webapp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const folderMetadata = ".birdhackbot-folder.json"

type folderRecord struct {
	Title string `json:"title"`
}

// Folders are logical customer groups. Their title is separate from the
// filesystem-safe ID so operators can use ordinary names in the sidebar.
func (s *Server) createFolder(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > 80 {
		return "", fmt.Errorf("folder name must be between 1 and 80 characters")
	}
	id := fmt.Sprintf("group-%d-%06d", time.Now().UTC().UnixNano(), s.seq.Add(1))
	root := filepath.Join(s.config.SessionsRoot, id)
	if err := os.MkdirAll(s.config.SessionsRoot, 0700); err != nil {
		return "", err
	}
	if err := os.Mkdir(root, 0700); err != nil {
		return "", err
	}
	if err := atomicWriteJSON(filepath.Join(root, folderMetadata), folderRecord{Title: title}); err != nil {
		_ = os.Remove(root)
		return "", err
	}
	return id, nil
}

func (s *Server) folderTitles() map[string]string {
	titles := map[string]string{}
	entries, err := os.ReadDir(s.config.SessionsRoot)
	if err != nil {
		return titles
	}
	for _, entry := range entries {
		if !entry.IsDir() || !validCustomerID(entry.Name()) {
			continue
		}
		var record folderRecord
		data, err := os.ReadFile(filepath.Join(s.config.SessionsRoot, entry.Name(), folderMetadata))
		if err == nil && json.Unmarshal(data, &record) == nil && strings.TrimSpace(record.Title) != "" {
			titles[entry.Name()] = record.Title
		}
	}
	return titles
}

func (s *Server) assignRunCustomer(current *run, customer string) error {
	if !validCustomerID(customer) {
		return fmt.Errorf("invalid customer folder")
	}
	current.mu.Lock()
	if current.deleted {
		current.mu.Unlock()
		return fmt.Errorf("session has been deleted")
	}
	previous := current.customer
	current.customer = customer
	current.updatedAt = time.Now().UTC()
	current.mu.Unlock()
	if err := current.persist(); err != nil {
		current.mu.Lock()
		current.customer = previous
		current.mu.Unlock()
		return err
	}
	return nil
}
