package history

import (
	"strings"
	"testing"
	"time"

	"github.com/devaloi/ask/internal/util"
)

func TestNewStore(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Verify tables were created by checking if we can query them
	var count int
	err = store.db.QueryRow("SELECT COUNT(*) FROM conversations").Scan(&count)
	if err != nil {
		t.Errorf("conversations table not created: %v", err)
	}

	err = store.db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&count)
	if err != nil {
		t.Errorf("messages table not created: %v", err)
	}
}

func TestSaveConversation_NewConversation(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	conv := &Conversation{
		Title:    "Test Conversation",
		Model:    "gpt-4",
		Provider: "openai",
		Messages: []Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there!"},
		},
	}

	id, err := store.SaveConversation(conv)
	if err != nil {
		t.Fatalf("SaveConversation failed: %v", err)
	}

	if id <= 0 {
		t.Errorf("expected positive ID, got %d", id)
	}

	if conv.ID != id {
		t.Errorf("expected conv.ID to be set to %d, got %d", id, conv.ID)
	}
}

func TestSaveConversation_AutoGenerateTitle(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	tests := []struct {
		name          string
		messages      []Message
		expectedTitle string
	}{
		{
			name: "short user message becomes title",
			messages: []Message{
				{Role: "user", Content: "What is Go?"},
			},
			expectedTitle: "What is Go?",
		},
		{
			name: "long user message is truncated with ellipsis",
			messages: []Message{
				{Role: "user", Content: "This is a very long message that exceeds fifty characters and should be truncated"},
			},
			expectedTitle: "This is a very long message that exceeds fifty ...",
		},
		{
			name: "first user message is used even if assistant message comes first",
			messages: []Message{
				{Role: "assistant", Content: "How can I help?"},
				{Role: "user", Content: "Explain Rust"},
			},
			expectedTitle: "Explain Rust",
		},
		{
			name: "newlines are normalized",
			messages: []Message{
				{Role: "user", Content: "Line one\nLine two\nLine three"},
			},
			expectedTitle: "Line one Line two Line three",
		},
		{
			name: "extra whitespace is normalized",
			messages: []Message{
				{Role: "user", Content: "Multiple   spaces   are   normalized"},
			},
			expectedTitle: "Multiple spaces are normalized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conv := &Conversation{
				Model:    "claude-3",
				Provider: "anthropic",
				Messages: tt.messages,
			}

			id, err := store.SaveConversation(conv)
			if err != nil {
				t.Fatalf("SaveConversation failed: %v", err)
			}

			retrieved, err := store.GetConversation(id)
			if err != nil {
				t.Fatalf("GetConversation failed: %v", err)
			}

			if retrieved.Title != tt.expectedTitle {
				t.Errorf("expected title %q, got %q", tt.expectedTitle, retrieved.Title)
			}
		})
	}
}

func TestSaveConversation_AppendMessages(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Create initial conversation
	conv := &Conversation{
		Title:    "Append Test",
		Model:    "gpt-4",
		Provider: "openai",
		Messages: []Message{
			{Role: "user", Content: "First message"},
			{Role: "assistant", Content: "First response"},
		},
	}

	id, err := store.SaveConversation(conv)
	if err != nil {
		t.Fatalf("SaveConversation failed: %v", err)
	}

	// Append new messages
	conv.Messages = []Message{
		{Role: "user", Content: "Second message"},
		{Role: "assistant", Content: "Second response"},
	}

	id2, err := store.SaveConversation(conv)
	if err != nil {
		t.Fatalf("SaveConversation (append) failed: %v", err)
	}

	if id != id2 {
		t.Errorf("expected same ID %d after append, got %d", id, id2)
	}

	// Verify all messages are stored
	retrieved, err := store.GetConversation(id)
	if err != nil {
		t.Fatalf("GetConversation failed: %v", err)
	}

	if len(retrieved.Messages) != 4 {
		t.Errorf("expected 4 messages, got %d", len(retrieved.Messages))
	}

	expectedContents := []string{"First message", "First response", "Second message", "Second response"}
	for i, expected := range expectedContents {
		if retrieved.Messages[i].Content != expected {
			t.Errorf("message %d: expected %q, got %q", i, expected, retrieved.Messages[i].Content)
		}
	}
}

func TestListConversations_OrderedByDate(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Create conversations with slight delays to ensure ordering
	titles := []string{"First", "Second", "Third"}
	for _, title := range titles {
		conv := &Conversation{
			Title:    title,
			Model:    "gpt-4",
			Provider: "openai",
		}
		_, err := store.SaveConversation(conv)
		if err != nil {
			t.Fatalf("SaveConversation failed: %v", err)
		}
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	}

	conversations, err := store.ListConversations(10, "")
	if err != nil {
		t.Fatalf("ListConversations failed: %v", err)
	}

	if len(conversations) != 3 {
		t.Fatalf("expected 3 conversations, got %d", len(conversations))
	}

	// Should be newest first
	expectedOrder := []string{"Third", "Second", "First"}
	for i, expected := range expectedOrder {
		if conversations[i].Title != expected {
			t.Errorf("position %d: expected %q, got %q", i, expected, conversations[i].Title)
		}
	}
}

func TestListConversations_Limit(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Create 5 conversations
	for i := 0; i < 5; i++ {
		conv := &Conversation{
			Title:    "Conversation",
			Model:    "gpt-4",
			Provider: "openai",
		}
		_, err := store.SaveConversation(conv)
		if err != nil {
			t.Fatalf("SaveConversation failed: %v", err)
		}
	}

	tests := []struct {
		limit    int
		expected int
	}{
		{limit: 1, expected: 1},
		{limit: 3, expected: 3},
		{limit: 5, expected: 5},
		{limit: 10, expected: 5}, // More than available
	}

	for _, tt := range tests {
		conversations, err := store.ListConversations(tt.limit, "")
		if err != nil {
			t.Fatalf("ListConversations failed: %v", err)
		}

		if len(conversations) != tt.expected {
			t.Errorf("limit %d: expected %d conversations, got %d", tt.limit, tt.expected, len(conversations))
		}
	}
}

func TestListConversations_SearchByTitle(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Create conversations with different titles
	convs := []struct {
		title   string
		model   string
		content string
	}{
		{title: "Go Programming", model: "gpt-4", content: "Tell me about Go"},
		{title: "Python Basics", model: "claude-3", content: "Python is great"},
		{title: "JavaScript Tips", model: "gpt-4", content: "Async await"},
	}

	for _, c := range convs {
		conv := &Conversation{
			Title:    c.title,
			Model:    c.model,
			Provider: "test",
			Messages: []Message{{Role: "user", Content: c.content}},
		}
		_, err := store.SaveConversation(conv)
		if err != nil {
			t.Fatalf("SaveConversation failed: %v", err)
		}
	}

	// Search by title
	conversations, err := store.ListConversations(10, "Go Programming")
	if err != nil {
		t.Fatalf("ListConversations failed: %v", err)
	}

	if len(conversations) != 1 {
		t.Errorf("expected 1 conversation, got %d", len(conversations))
	}

	if len(conversations) > 0 && conversations[0].Title != "Go Programming" {
		t.Errorf("expected title 'Go Programming', got %q", conversations[0].Title)
	}
}

func TestListConversations_SearchByContent(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Create conversations
	conv1 := &Conversation{
		Title:    "Chat 1",
		Model:    "gpt-4",
		Provider: "test",
		Messages: []Message{{Role: "user", Content: "Tell me about kubernetes"}},
	}
	conv2 := &Conversation{
		Title:    "Chat 2",
		Model:    "gpt-4",
		Provider: "test",
		Messages: []Message{{Role: "user", Content: "Explain Docker"}},
	}

	store.SaveConversation(conv1)
	store.SaveConversation(conv2)

	// Search by message content
	conversations, err := store.ListConversations(10, "kubernetes")
	if err != nil {
		t.Fatalf("ListConversations failed: %v", err)
	}

	if len(conversations) != 1 {
		t.Errorf("expected 1 conversation, got %d", len(conversations))
	}

	if len(conversations) > 0 && conversations[0].Title != "Chat 1" {
		t.Errorf("expected title 'Chat 1', got %q", conversations[0].Title)
	}
}

func TestListConversations_SearchNoMatches(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	conv := &Conversation{
		Title:    "Some Title",
		Model:    "gpt-4",
		Provider: "test",
		Messages: []Message{{Role: "user", Content: "Hello world"}},
	}
	store.SaveConversation(conv)

	conversations, err := store.ListConversations(10, "nonexistent-search-term-xyz")
	if err != nil {
		t.Fatalf("ListConversations failed: %v", err)
	}

	if len(conversations) != 0 {
		t.Errorf("expected 0 conversations, got %d", len(conversations))
	}
}

func TestListConversations_EmptyDatabase(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	conversations, err := store.ListConversations(10, "")
	if err != nil {
		t.Fatalf("ListConversations failed: %v", err)
	}

	if len(conversations) != 0 {
		t.Errorf("expected empty slice, got %d conversations", len(conversations))
	}
}

func TestGetConversation_WithMessages(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	conv := &Conversation{
		Title:    "Full Conversation",
		Model:    "claude-3",
		Provider: "anthropic",
		Messages: []Message{
			{Role: "user", Content: "Question 1"},
			{Role: "assistant", Content: "Answer 1"},
			{Role: "user", Content: "Question 2"},
			{Role: "assistant", Content: "Answer 2"},
		},
	}

	id, err := store.SaveConversation(conv)
	if err != nil {
		t.Fatalf("SaveConversation failed: %v", err)
	}

	retrieved, err := store.GetConversation(id)
	if err != nil {
		t.Fatalf("GetConversation failed: %v", err)
	}

	// Check conversation fields
	if retrieved.ID != id {
		t.Errorf("expected ID %d, got %d", id, retrieved.ID)
	}
	if retrieved.Title != "Full Conversation" {
		t.Errorf("expected title 'Full Conversation', got %q", retrieved.Title)
	}
	if retrieved.Model != "claude-3" {
		t.Errorf("expected model 'claude-3', got %q", retrieved.Model)
	}
	if retrieved.Provider != "anthropic" {
		t.Errorf("expected provider 'anthropic', got %q", retrieved.Provider)
	}

	// Check messages
	if len(retrieved.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(retrieved.Messages))
	}

	expectedRoles := []string{"user", "assistant", "user", "assistant"}
	expectedContents := []string{"Question 1", "Answer 1", "Question 2", "Answer 2"}

	for i, msg := range retrieved.Messages {
		if msg.Role != expectedRoles[i] {
			t.Errorf("message %d: expected role %q, got %q", i, expectedRoles[i], msg.Role)
		}
		if msg.Content != expectedContents[i] {
			t.Errorf("message %d: expected content %q, got %q", i, expectedContents[i], msg.Content)
		}
		if msg.ConversationID != id {
			t.Errorf("message %d: expected conversation_id %d, got %d", i, id, msg.ConversationID)
		}
		if msg.ID == 0 {
			t.Errorf("message %d: expected non-zero ID", i)
		}
	}
}

func TestGetConversation_NotFound(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	_, err = store.GetConversation(999)
	if err == nil {
		t.Error("expected error for non-existent conversation, got nil")
	}

	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestGetConversation_NoMessages(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	conv := &Conversation{
		Title:    "Empty Conversation",
		Model:    "gpt-4",
		Provider: "openai",
	}

	id, err := store.SaveConversation(conv)
	if err != nil {
		t.Fatalf("SaveConversation failed: %v", err)
	}

	retrieved, err := store.GetConversation(id)
	if err != nil {
		t.Fatalf("GetConversation failed: %v", err)
	}

	if len(retrieved.Messages) != 0 && retrieved.Messages != nil {
		t.Errorf("expected empty messages, got %d", len(retrieved.Messages))
	}
}

func TestMessagesChronologicalOrder(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	conv := &Conversation{
		Title:    "Order Test",
		Model:    "gpt-4",
		Provider: "openai",
		Messages: []Message{
			{Role: "user", Content: "Message 1"},
		},
	}

	id, err := store.SaveConversation(conv)
	if err != nil {
		t.Fatalf("SaveConversation failed: %v", err)
	}

	// Add more messages over time
	time.Sleep(10 * time.Millisecond)
	conv.Messages = []Message{{Role: "assistant", Content: "Message 2"}}
	store.SaveConversation(conv)

	time.Sleep(10 * time.Millisecond)
	conv.Messages = []Message{{Role: "user", Content: "Message 3"}}
	store.SaveConversation(conv)

	time.Sleep(10 * time.Millisecond)
	conv.Messages = []Message{{Role: "assistant", Content: "Message 4"}}
	store.SaveConversation(conv)

	// Retrieve and verify order
	retrieved, err := store.GetConversation(id)
	if err != nil {
		t.Fatalf("GetConversation failed: %v", err)
	}

	if len(retrieved.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(retrieved.Messages))
	}

	// Verify chronological order
	for i := 1; i < len(retrieved.Messages); i++ {
		if retrieved.Messages[i].CreatedAt.Before(retrieved.Messages[i-1].CreatedAt) {
			t.Errorf("messages not in chronological order: message %d created before message %d", i, i-1)
		}
	}

	// Verify content order
	expectedOrder := []string{"Message 1", "Message 2", "Message 3", "Message 4"}
	for i, expected := range expectedOrder {
		if retrieved.Messages[i].Content != expected {
			t.Errorf("message %d: expected %q, got %q", i, expected, retrieved.Messages[i].Content)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "short string unchanged",
			input:    "Hello",
			maxLen:   50,
			expected: "Hello",
		},
		{
			name:     "exact length unchanged",
			input:    "12345678901234567890",
			maxLen:   20,
			expected: "12345678901234567890",
		},
		{
			name:     "long string truncated with ellipsis",
			input:    "This is a very long string that needs to be truncated",
			maxLen:   20,
			expected: "This is a very lo...",
		},
		{
			name:     "newlines normalized",
			input:    "Line1\nLine2\nLine3",
			maxLen:   50,
			expected: "Line1 Line2 Line3",
		},
		{
			name:     "multiple spaces normalized",
			input:    "Word   with   spaces",
			maxLen:   50,
			expected: "Word with spaces",
		},
		{
			name:     "tabs and newlines normalized",
			input:    "Tab\there\nand\nnewlines",
			maxLen:   50,
			expected: "Tab here and newlines",
		},
		{
			name:     "leading and trailing whitespace removed",
			input:    "  trimmed  ",
			maxLen:   50,
			expected: "trimmed",
		},
		{
			name:     "truncation with whitespace normalization",
			input:    "This\nis\na\nvery\nlong\ntext\nwith\nmany\nnewlines\nand\nmore",
			maxLen:   30,
			expected: "This is a very long text wi...",
		},
		{
			name:     "empty string",
			input:    "",
			maxLen:   50,
			expected: "",
		},
		{
			name:     "whitespace only",
			input:    "   \n\t  ",
			maxLen:   50,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := util.Truncate(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
			}
		})
	}
}

func TestStoreClose(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	err = store.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Verify database is closed by attempting to query
	var count int
	err = store.db.QueryRow("SELECT COUNT(*) FROM conversations").Scan(&count)
	if err == nil {
		t.Error("expected error after closing database, got nil")
	}
}

func TestSaveConversation_PreservesExplicitTitle(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	conv := &Conversation{
		Title:    "My Custom Title",
		Model:    "gpt-4",
		Provider: "openai",
		Messages: []Message{
			{Role: "user", Content: "This should not become the title"},
		},
	}

	id, err := store.SaveConversation(conv)
	if err != nil {
		t.Fatalf("SaveConversation failed: %v", err)
	}

	retrieved, err := store.GetConversation(id)
	if err != nil {
		t.Fatalf("GetConversation failed: %v", err)
	}

	if retrieved.Title != "My Custom Title" {
		t.Errorf("expected title 'My Custom Title', got %q", retrieved.Title)
	}
}

func TestSaveConversation_SkipsAlreadyPersistedMessages(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	conv := &Conversation{
		Title:    "Test",
		Model:    "gpt-4",
		Provider: "openai",
		Messages: []Message{
			{Role: "user", Content: "First"},
		},
	}

	id, err := store.SaveConversation(conv)
	if err != nil {
		t.Fatalf("SaveConversation failed: %v", err)
	}

	// Retrieve to get the message IDs
	retrieved, err := store.GetConversation(id)
	if err != nil {
		t.Fatalf("GetConversation failed: %v", err)
	}

	// Include already persisted message (with ID) and new message (without ID)
	conv.Messages = append(retrieved.Messages, Message{Role: "assistant", Content: "Second"})

	_, err = store.SaveConversation(conv)
	if err != nil {
		t.Fatalf("SaveConversation failed: %v", err)
	}

	// Verify only one new message was added
	final, err := store.GetConversation(id)
	if err != nil {
		t.Fatalf("GetConversation failed: %v", err)
	}

	if len(final.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(final.Messages))
	}
}

func TestListConversations_SearchCaseInsensitive(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	conv := &Conversation{
		Title:    "GoLang Programming",
		Model:    "gpt-4",
		Provider: "test",
		Messages: []Message{{Role: "user", Content: "Tell me about KUBERNETES"}},
	}
	store.SaveConversation(conv)

	tests := []struct {
		search   string
		expected int
	}{
		{"golang", 1},
		{"GOLANG", 1},
		{"GoLang", 1},
		{"kubernetes", 1},
		{"KUBERNETES", 1},
	}

	for _, tt := range tests {
		conversations, err := store.ListConversations(10, tt.search)
		if err != nil {
			t.Fatalf("ListConversations failed for %q: %v", tt.search, err)
		}

		if len(conversations) != tt.expected {
			t.Errorf("search %q: expected %d conversations, got %d", tt.search, tt.expected, len(conversations))
		}
	}
}

func TestListConversations_PartialMatch(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	conv := &Conversation{
		Title:    "Programming in Go",
		Model:    "gpt-4",
		Provider: "test",
	}
	store.SaveConversation(conv)

	// Search for partial match
	conversations, err := store.ListConversations(10, "gram")
	if err != nil {
		t.Fatalf("ListConversations failed: %v", err)
	}

	if len(conversations) != 1 {
		t.Errorf("expected 1 conversation for partial match, got %d", len(conversations))
	}
}
