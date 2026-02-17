// Package history provides conversation storage using SQLite.
package history

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Message represents a single message in a conversation.
type Message struct {
	ID             int64
	ConversationID int64
	Role           string
	Content        string
	CreatedAt      time.Time
}

// Conversation represents a conversation with an LLM.
type Conversation struct {
	ID        int64
	Title     string
	Model     string
	Provider  string
	CreatedAt time.Time
	Messages  []Message
}

// Store handles SQLite conversation storage.
type Store struct {
	db *sql.DB
}

// NewStore creates a new SQLite store at the given path.
// It creates the database and runs migrations if needed.
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store := &Store{db: db}

	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return store, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// SaveConversation saves a new conversation with its messages.
// If the conversation has an ID, it appends the new messages.
// Returns the conversation ID.
func (s *Store) SaveConversation(conv *Conversation) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if conv.ID == 0 {
		// New conversation
		title := conv.Title
		if title == "" && len(conv.Messages) > 0 {
			// Auto-generate title from first user message
			for _, msg := range conv.Messages {
				if msg.Role == "user" {
					title = truncateTitle(msg.Content, 50)
					break
				}
			}
		}

		result, err := tx.Exec(
			`INSERT INTO conversations (title, model, provider, created_at) VALUES (?, ?, ?, ?)`,
			title, conv.Model, conv.Provider, time.Now(),
		)
		if err != nil {
			return 0, fmt.Errorf("failed to insert conversation: %w", err)
		}

		conv.ID, err = result.LastInsertId()
		if err != nil {
			return 0, fmt.Errorf("failed to get conversation ID: %w", err)
		}
	}

	// Insert messages
	for _, msg := range conv.Messages {
		if msg.ID == 0 {
			_, err := tx.Exec(
				`INSERT INTO messages (conversation_id, role, content, created_at) VALUES (?, ?, ?, ?)`,
				conv.ID, msg.Role, msg.Content, time.Now(),
			)
			if err != nil {
				return 0, fmt.Errorf("failed to insert message: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return conv.ID, nil
}

// ListConversations returns recent conversations, optionally filtered by search.
func (s *Store) ListConversations(limit int, search string) ([]Conversation, error) {
	var rows *sql.Rows
	var err error

	if search != "" {
		// Search in titles and message content
		rows, err = s.db.Query(`
			SELECT DISTINCT c.id, c.title, c.model, c.provider, c.created_at
			FROM conversations c
			LEFT JOIN messages m ON c.id = m.conversation_id
			WHERE c.title LIKE ? OR m.content LIKE ?
			ORDER BY c.created_at DESC
			LIMIT ?
		`, "%"+search+"%", "%"+search+"%", limit)
	} else {
		rows, err = s.db.Query(`
			SELECT id, title, model, provider, created_at
			FROM conversations
			ORDER BY created_at DESC
			LIMIT ?
		`, limit)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to list conversations: %w", err)
	}
	defer rows.Close()

	var conversations []Conversation
	for rows.Next() {
		var conv Conversation
		if err := rows.Scan(&conv.ID, &conv.Title, &conv.Model, &conv.Provider, &conv.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan conversation: %w", err)
		}
		conversations = append(conversations, conv)
	}

	return conversations, rows.Err()
}

// GetConversation returns a conversation with all its messages.
func (s *Store) GetConversation(id int64) (*Conversation, error) {
	conv := &Conversation{}

	err := s.db.QueryRow(`
		SELECT id, title, model, provider, created_at
		FROM conversations
		WHERE id = ?
	`, id).Scan(&conv.ID, &conv.Title, &conv.Model, &conv.Provider, &conv.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("conversation %d not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get conversation: %w", err)
	}

	rows, err := s.db.Query(`
		SELECT id, role, content, created_at
		FROM messages
		WHERE conversation_id = ?
		ORDER BY created_at ASC
	`, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get messages: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var msg Message
		if err := rows.Scan(&msg.ID, &msg.Role, &msg.Content, &msg.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}
		msg.ConversationID = id
		conv.Messages = append(conv.Messages, msg)
	}

	return conv, rows.Err()
}

// truncateTitle truncates a string to maxLen, adding "..." if truncated.
func truncateTitle(s string, maxLen int) string {
	// Remove newlines and extra whitespace
	s = strings.Join(strings.Fields(s), " ")

	if len(s) <= maxLen {
		return s
	}

	return s[:maxLen-3] + "..."
}
