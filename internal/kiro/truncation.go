package kiro

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// ToolTruncationInfo stores metadata about a truncated tool call.
type ToolTruncationInfo struct {
	ToolCallID     string                 `json:"tool_call_id"`
	ToolName       string                 `json:"tool_name"`
	TruncationInfo map[string]interface{} `json:"truncation_info"`
	Timestamp      time.Time              `json:"timestamp"`
}

// ContentTruncationInfo stores metadata about truncated assistant content.
type ContentTruncationInfo struct {
	MessageHash    string    `json:"message_hash"`
	ContentPreview string    `json:"content_preview"`
	Timestamp      time.Time `json:"timestamp"`
}

var (
	toolTruncationCache    = make(map[string]ToolTruncationInfo)
	contentTruncationCache = make(map[string]ContentTruncationInfo)
	truncationMu           sync.Mutex
)

// SaveToolTruncation stores truncation information for a specific tool call.
func SaveToolTruncation(toolCallID, toolName string, truncationInfo map[string]interface{}) {
	truncationMu.Lock()
	defer truncationMu.Unlock()
	toolTruncationCache[toolCallID] = ToolTruncationInfo{
		ToolCallID:     toolCallID,
		ToolName:       toolName,
		TruncationInfo: truncationInfo,
		Timestamp:      time.Now(),
	}
}

// GetToolTruncation retrieves and removes truncation information for a specific tool call.
func GetToolTruncation(toolCallID string) (ToolTruncationInfo, bool) {
	truncationMu.Lock()
	defer truncationMu.Unlock()
	info, ok := toolTruncationCache[toolCallID]
	if ok {
		delete(toolTruncationCache, toolCallID)
	}
	return info, ok
}

// SaveContentTruncation hashes the truncated content and stores its preview in the cache.
func SaveContentTruncation(content string) string {
	limit := 500
	if len(content) < limit {
		limit = len(content)
	}
	contentForHash := content[:limit]
	hashBytes := sha256.Sum256([]byte(contentForHash))
	messageHash := hex.EncodeToString(hashBytes[:])[:16]

	previewLimit := 200
	if len(content) < previewLimit {
		previewLimit = len(content)
	}

	truncationMu.Lock()
	defer truncationMu.Unlock()
	contentTruncationCache[messageHash] = ContentTruncationInfo{
		MessageHash:    messageHash,
		ContentPreview: content[:previewLimit],
		Timestamp:      time.Now(),
	}
	return messageHash
}

// GetContentTruncation checks if the assistant message was previously truncated, retrieving and removing it if so.
func GetContentTruncation(content string) (ContentTruncationInfo, bool) {
	limit := 500
	if len(content) < limit {
		limit = len(content)
	}
	contentForHash := content[:limit]
	hashBytes := sha256.Sum256([]byte(contentForHash))
	messageHash := hex.EncodeToString(hashBytes[:])[:16]

	truncationMu.Lock()
	defer truncationMu.Unlock()
	info, ok := contentTruncationCache[messageHash]
	if ok {
		delete(contentTruncationCache, messageHash)
	}
	return info, ok
}

// ClearTruncationCaches clears the in-memory truncation caches.
func ClearTruncationCaches() {
	truncationMu.Lock()
	defer truncationMu.Unlock()
	toolTruncationCache = make(map[string]ToolTruncationInfo)
	contentTruncationCache = make(map[string]ContentTruncationInfo)
}
