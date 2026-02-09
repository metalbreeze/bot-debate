package main

import (
	"time"
)

// Debate represents a debate session
type Debate struct {
	ID           string    `json:"debate_id"`
	Topic        string    `json:"topic"`
	TotalRounds  int       `json:"total_rounds"`
	CurrentRound int       `json:"current_round"`
	Status       string    `json:"status"` // waiting, active, completed, timeout, error
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Bot represents a bot participant
type Bot struct {
	BotName       string    `json:"bot_name"`
	BotUUID       string    `json:"bot_uuid"`
	BotIdentifier string    `json:"bot_identifier"` // name+uuid (first 8 chars)
	DebateID      string    `json:"debate_id"`
	DebateKey     string    `json:"debate_key"`
	Side          string    `json:"side"` // supporting, opposing, or empty
	ConnectedAt   time.Time `json:"connected_at"`
}

// Message represents a base WebSocket message
type Message struct {
	Type      string      `json:"type"`
	Timestamp string      `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// LoginRequest from bot
type LoginRequest struct {
	BotName  string `json:"bot_name"`
	BotUUID  string `json:"bot_uuid"`
	DebateID string `json:"debate_id"`
	Version  string `json:"version,omitempty"`
}

// LoginConfirmed response
type LoginConfirmed struct {
	Status        string   `json:"status"`
	Message       string   `json:"message"`
	DebateID      string   `json:"debate_id"`
	DebateKey     string   `json:"debate_key"`
	BotIdentifier string   `json:"bot_identifier"`
	Topic         string   `json:"topic"`
	JoinedBots    []string `json:"joined_bots"` // List of bot identifiers that have already joined
}

// LoginRejected response
type LoginRejected struct {
	Status     string `json:"status"`
	Reason     string `json:"reason"`
	Message    string `json:"message"`
	DebateID   string `json:"debate_id"`
	RetryAfter int    `json:"retry_after,omitempty"`
}

// DebateStart notification
type DebateStart struct {
	DebateID         string `json:"debate_id"`
	Topic            string `json:"topic"`
	SupportingSide   string `json:"supporting_side"`
	OpposingSide     string `json:"opposing_side"`
	TotalRounds      int    `json:"total_rounds"`
	CurrentRound     int    `json:"current_round"`
	YourSide         string `json:"your_side"`
	YourIdentifier   string `json:"your_identifier"`
	NextSpeaker      string `json:"next_speaker"`
	TimeoutSeconds   int    `json:"timeout_seconds"`
	MinContentLength int    `json:"min_content_length"`
	MaxContentLength int    `json:"max_content_length"`
}

// SpeechMessage content
type SpeechMessage struct {
	Format  string `json:"format"`
	Content string `json:"content"`
}

// DebateSpeech from bot
type DebateSpeech struct {
	DebateID  string        `json:"debate_id"`
	DebateKey string        `json:"debate_key"`
	Speaker   string        `json:"speaker"`
	Message   SpeechMessage `json:"message"`
}

// DebateLogEntry in history
type DebateLogEntry struct {
	Round     int           `json:"round"`
	Speaker   string        `json:"speaker"`
	Side      string        `json:"side"`
	Timestamp string        `json:"timestamp"`
	Message   SpeechMessage `json:"message"`
}

// DebateUpdate to bots
type DebateUpdate struct {
	DebateID         string           `json:"debate_id"`
	Topic            string           `json:"topic"`
	SupportingSide   string           `json:"supporting_side"`
	OpposingSide     string           `json:"opposing_side"`
	TotalRounds      int              `json:"total_rounds"`
	CurrentRound     int              `json:"current_round"`
	YourSide         string           `json:"your_side"`
	YourIdentifier   string           `json:"your_identifier"`
	NextSpeaker      string           `json:"next_speaker"`
	TimeoutSeconds   int              `json:"timeout_seconds"`
	MinContentLength int              `json:"min_content_length"`
	MaxContentLength int              `json:"max_content_length"`
	DebateLog        []DebateLogEntry `json:"debate_log"`
}

// DebateResult summary
type DebateResult struct {
	Winner          string        `json:"winner"`
	SupportingScore int           `json:"supporting_score"`
	OpposingScore   int           `json:"opposing_score"`
	Summary         SpeechMessage `json:"summary"`
	Reason          string        `json:"reason,omitempty"` // Reason for debate end (e.g., "completed", "bot_disconnected", "heartbeat_timeout", "max_duration_timeout")
}

// DebateEnd notification
type DebateEnd struct {
	DebateID       string           `json:"debate_id"`
	Topic          string           `json:"topic"`
	SupportingSide string           `json:"supporting_side"`
	OpposingSide   string           `json:"opposing_side"`
	TotalRounds    int              `json:"total_rounds"`
	Status         string           `json:"status"`
	DebateLog      []DebateLogEntry `json:"debate_log"`
	DebateResult   DebateResult     `json:"debate_result"`
}

// DebateWaiting notification (waiting for bots to join)
type DebateWaiting struct {
	DebateID    string   `json:"debate_id"`
	Topic       string   `json:"topic"`
	TotalRounds int      `json:"total_rounds"`
	Status      string   `json:"status"`
	JoinedBots  []string `json:"joined_bots"` // List of bot identifiers that have joined
}

// ErrorMessage to bot
type ErrorMessage struct {
	ErrorCode   string `json:"error_code"`
	Message     string `json:"message"`
	DebateID    string `json:"debate_id,omitempty"`
	Details     string `json:"details,omitempty"`
	Recoverable bool   `json:"recoverable"`
}

// CreateDebateRequest from frontend
type CreateDebateRequest struct {
	Topic       string `json:"topic"`
	TotalRounds int    `json:"total_rounds"`
	CreatedBy   string `json:"created_by,omitempty"`
}

// DebateCreated response
type DebateCreated struct {
	DebateID    string `json:"debate_id"`
	Topic       string `json:"topic"`
	TotalRounds int    `json:"total_rounds"`
	Status      string `json:"status"`
}

// SubscribeDebate from frontend
type SubscribeDebate struct {
	DebateID string `json:"debate_id"`
}
