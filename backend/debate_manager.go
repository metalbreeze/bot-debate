package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// DebateManager manages active debates and bot connections
type DebateManager struct {
	debates   map[string]*ActiveDebate
	mutex     sync.RWMutex
	db        *Database
	broadcast chan BroadcastMessage
}

// ActiveDebate represents a debate in progress
type ActiveDebate struct {
	Debate              *Debate
	BotA                *ConnectedBot
	BotB                *ConnectedBot
	SupportingBot       *ConnectedBot
	OpposingBot         *ConnectedBot
	DebateLog           []DebateLogEntry
	FrontendConns       map[*websocket.Conn]bool
	LastSpeaker         string
	WaitingTimer        *time.Timer // Timer for waiting state timeout
	TimeoutTimer        *time.Timer
	InactivityTimer     *time.Timer
	MaxDurationTimer    *time.Timer
	StartTime           time.Time
	LastActivityTime    time.Time
	mutex               sync.RWMutex
}

// ConnectedBot represents a connected bot
type ConnectedBot struct {
	Bot              *Bot
	Conn             *websocket.Conn
	LastPongTime     time.Time
	MissedPings      int
	PingTicker       *time.Ticker
	HeartbeatQuitCh  chan bool
}

// BroadcastMessage for sending to frontend
type BroadcastMessage struct {
	DebateID string
	Message  Message
}

// NewDebateManager creates a new debate manager
func NewDebateManager(db *Database) *DebateManager {
	dm := &DebateManager{
		debates:   make(map[string]*ActiveDebate),
		db:        db,
		broadcast: make(chan BroadcastMessage, 100),
	}
	go dm.handleBroadcasts()
	return dm
}

// handleBroadcasts processes broadcast messages to frontend
func (dm *DebateManager) handleBroadcasts() {
	for msg := range dm.broadcast {
		dm.mutex.RLock()
		debate, exists := dm.debates[msg.DebateID]
		dm.mutex.RUnlock()

		if !exists {
			continue
		}

		debate.mutex.RLock()
		for conn := range debate.FrontendConns {
			err := conn.WriteJSON(msg.Message)
			if err != nil {
				log.Printf("Error broadcasting to frontend: %v", err)
			}
		}
		debate.mutex.RUnlock()
	}
}

// CreateDebate creates a new debate
func (dm *DebateManager) CreateDebate(topic string, totalRounds int) (*Debate, error) {
	debate := &Debate{
		ID:           "debate-" + uuid.New().String(),
		Topic:        topic,
		TotalRounds:  totalRounds,
		CurrentRound: 1,
		Status:       "waiting",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := dm.db.CreateDebate(debate); err != nil {
		return nil, err
	}

	dm.mutex.Lock()
	dm.debates[debate.ID] = &ActiveDebate{
		Debate:        debate,
		DebateLog:     make([]DebateLogEntry, 0),
		FrontendConns: make(map[*websocket.Conn]bool),
	}
	dm.mutex.Unlock()

	// Start waiting timeout timer (30 minutes)
	dm.startWaitingTimer(debate.ID)

	return debate, nil
}

// BotLogin handles bot login
func (dm *DebateManager) BotLogin(loginReq *LoginRequest, conn *websocket.Conn) (*LoginConfirmed, *LoginRejected) {
	dm.mutex.Lock()
	defer dm.mutex.Unlock()

	// If no debate_id provided, auto-assign an available debate
	if loginReq.DebateID == "" {
		availableDebate, err := dm.db.GetAvailableDebate()
		if err != nil {
			log.Printf("Error finding available debate: %v", err)
			return nil, &LoginRejected{
				Status:  "rejected",
				Reason:  "no_available_debate",
				Message: "No available debates found. Please create a debate first or specify a debate_id.",
			}
		}
		if availableDebate == nil {
			return nil, &LoginRejected{
				Status:  "rejected",
				Reason:  "no_available_debate",
				Message: "No available debates found. Please create a debate first or specify a debate_id.",
			}
		}
		loginReq.DebateID = availableDebate.ID
		log.Printf("Auto-assigned bot %s to debate %s", loginReq.BotName, availableDebate.ID)
	}

	activeDebate, exists := dm.debates[loginReq.DebateID]
	if !exists {
		// Try to load from database
		debate, err := dm.db.GetDebate(loginReq.DebateID)
		if err != nil {
			return nil, &LoginRejected{
				Status:   "rejected",
				Reason:   "debate_not_found",
				Message:  "Debate not found",
				DebateID: loginReq.DebateID,
			}
		}

		if debate.Status != "waiting" {
			return nil, &LoginRejected{
				Status:     "rejected",
				Reason:     "debate_not_ready",
				Message:    "Debate not ready yet, try later",
				DebateID:   loginReq.DebateID,
				RetryAfter: 5,
			}
		}

		activeDebate = &ActiveDebate{
			Debate:        debate,
			DebateLog:     make([]DebateLogEntry, 0),
			FrontendConns: make(map[*websocket.Conn]bool),
		}
		dm.debates[loginReq.DebateID] = activeDebate
	}

	// Check if debate is full
	if activeDebate.BotA != nil && activeDebate.BotB != nil {
		return nil, &LoginRejected{
			Status:   "rejected",
			Reason:   "debate_full",
			Message:  "Debate already has two bots",
			DebateID: loginReq.DebateID,
		}
	}

	// Generate bot identifier and debate key
	botIdentifier := fmt.Sprintf("%s-%s", loginReq.BotName, loginReq.BotUUID[:8])
	debateKey := generateDebateKey()

	bot := &Bot{
		BotName:       loginReq.BotName,
		BotUUID:       loginReq.BotUUID,
		BotIdentifier: botIdentifier,
		DebateID:      loginReq.DebateID,
		DebateKey:     debateKey,
		ConnectedAt:   time.Now(),
	}

	// Add bot to database
	if err := dm.db.AddBot(bot); err != nil {
		log.Printf("Error adding bot to database: %v", err)
		return nil, &LoginRejected{
			Status:   "rejected",
			Reason:   "internal_error",
			Message:  "Failed to register bot",
			DebateID: loginReq.DebateID,
		}
	}

	connectedBot := &ConnectedBot{
		Bot:  bot,
		Conn: conn,
	}

	// Assign bot slot
	if activeDebate.BotA == nil {
		activeDebate.BotA = connectedBot
	} else {
		activeDebate.BotB = connectedBot
	}

	// Build list of already joined bots (excluding the current bot)
	joinedBots := []string{}
	if activeDebate.BotA != nil && activeDebate.BotA.Bot.BotIdentifier != botIdentifier {
		joinedBots = append(joinedBots, activeDebate.BotA.Bot.BotIdentifier)
	}
	if activeDebate.BotB != nil && activeDebate.BotB.Bot.BotIdentifier != botIdentifier {
		joinedBots = append(joinedBots, activeDebate.BotB.Bot.BotIdentifier)
	}

	confirmed := &LoginConfirmed{
		Status:        "confirmed",
		Message:       "Wait for other bot",
		DebateID:      loginReq.DebateID,
		DebateKey:     debateKey,
		BotIdentifier: botIdentifier,
		Topic:         activeDebate.Debate.Topic,
		JoinedBots:    joinedBots,
	}

	// Broadcast waiting status to frontend
	allJoinedBots := []string{}
	if activeDebate.BotA != nil {
		allJoinedBots = append(allJoinedBots, activeDebate.BotA.Bot.BotIdentifier)
	}
	if activeDebate.BotB != nil {
		allJoinedBots = append(allJoinedBots, activeDebate.BotB.Bot.BotIdentifier)
	}
	dm.broadcast <- BroadcastMessage{
		DebateID: loginReq.DebateID,
		Message: createMessage("debate_waiting", DebateWaiting{
			DebateID:    loginReq.DebateID,
			Topic:       activeDebate.Debate.Topic,
			TotalRounds: activeDebate.Debate.TotalRounds,
			Status:      "waiting",
			JoinedBots:  allJoinedBots,
		}),
	}

	// If both bots are connected, start debate
	if activeDebate.BotA != nil && activeDebate.BotB != nil {
		go dm.startDebate(loginReq.DebateID)
	}

	return confirmed, nil
}

// startDebate initiates the debate
func (dm *DebateManager) startDebate(debateID string) {
	time.Sleep(1 * time.Second) // Small delay to ensure both bots are ready

	dm.mutex.Lock()
	activeDebate, exists := dm.debates[debateID]
	dm.mutex.Unlock()

	if !exists {
		return
	}

	// Cancel waiting timer since both bots are connected
	if activeDebate.WaitingTimer != nil {
		activeDebate.WaitingTimer.Stop()
		activeDebate.WaitingTimer = nil
	}

	// Randomly assign sides
	if randomBool() {
		activeDebate.SupportingBot = activeDebate.BotA
		activeDebate.OpposingBot = activeDebate.BotB
	} else {
		activeDebate.SupportingBot = activeDebate.BotB
		activeDebate.OpposingBot = activeDebate.BotA
	}

	// Update sides in database
	dm.db.UpdateBotSide(debateID, activeDebate.SupportingBot.Bot.BotIdentifier, "supporting")
	dm.db.UpdateBotSide(debateID, activeDebate.OpposingBot.Bot.BotIdentifier, "opposing")

	activeDebate.SupportingBot.Bot.Side = "supporting"
	activeDebate.OpposingBot.Bot.Side = "opposing"

	// Update debate status
	dm.db.UpdateDebateStatus(debateID, "active")
	activeDebate.Debate.Status = "active"

	// Send debate start to both bots
	startMsgA := createMessage("debate_start", DebateStart{
		DebateID:         debateID,
		Topic:            activeDebate.Debate.Topic,
		SupportingSide:   activeDebate.SupportingBot.Bot.BotIdentifier,
		OpposingSide:     activeDebate.OpposingBot.Bot.BotIdentifier,
		TotalRounds:      activeDebate.Debate.TotalRounds,
		CurrentRound:     1,
		YourSide:         activeDebate.SupportingBot.Bot.Side,
		YourIdentifier:   activeDebate.SupportingBot.Bot.BotIdentifier,
		NextSpeaker:      activeDebate.SupportingBot.Bot.BotIdentifier,
		TimeoutSeconds:   120,
		MinContentLength: config.Debate.MinContentLength,
		MaxContentLength: config.Debate.MaxContentLength,
	})

	startMsgB := createMessage("debate_start", DebateStart{
		DebateID:         debateID,
		Topic:            activeDebate.Debate.Topic,
		SupportingSide:   activeDebate.SupportingBot.Bot.BotIdentifier,
		OpposingSide:     activeDebate.OpposingBot.Bot.BotIdentifier,
		TotalRounds:      activeDebate.Debate.TotalRounds,
		CurrentRound:     1,
		YourSide:         activeDebate.OpposingBot.Bot.Side,
		YourIdentifier:   activeDebate.OpposingBot.Bot.BotIdentifier,
		NextSpeaker:      activeDebate.SupportingBot.Bot.BotIdentifier,
		TimeoutSeconds:   120,
		MinContentLength: config.Debate.MinContentLength,
		MaxContentLength: config.Debate.MaxContentLength,
	})

	activeDebate.SupportingBot.Conn.WriteJSON(startMsgA)
	activeDebate.OpposingBot.Conn.WriteJSON(startMsgB)

	// Broadcast to frontend
	dm.broadcast <- BroadcastMessage{
		DebateID: debateID,
		Message:  startMsgA,
	}

	// Set timing
	activeDebate.StartTime = time.Now()
	activeDebate.LastActivityTime = time.Now()
	activeDebate.LastSpeaker = ""

	// Start timers
	dm.startTimeout(debateID, activeDebate.SupportingBot.Bot.BotIdentifier)
	dm.startInactivityTimer(debateID)
	dm.startMaxDurationTimer(debateID)

	log.Printf("Debate %s started: %s (supporting) vs %s (opposing)",
		debateID, activeDebate.SupportingBot.Bot.BotIdentifier, activeDebate.OpposingBot.Bot.BotIdentifier)
}

// HandleSpeech processes a bot's speech
func (dm *DebateManager) HandleSpeech(speech *DebateSpeech, senderConn *websocket.Conn) *ErrorMessage {
	dm.mutex.RLock()
	activeDebate, exists := dm.debates[speech.DebateID]
	dm.mutex.RUnlock()

	if !exists {
		return &ErrorMessage{
			ErrorCode:   "DEBATE_NOT_FOUND",
			Message:     "Debate not found",
			DebateID:    speech.DebateID,
			Recoverable: false,
		}
	}

	// Verify debate key
	var speakerBot *ConnectedBot
	if activeDebate.SupportingBot != nil && activeDebate.SupportingBot.Bot.BotIdentifier == speech.Speaker {
		speakerBot = activeDebate.SupportingBot
	} else if activeDebate.OpposingBot != nil && activeDebate.OpposingBot.Bot.BotIdentifier == speech.Speaker {
		speakerBot = activeDebate.OpposingBot
	}

	if speakerBot == nil || speakerBot.Bot.DebateKey != speech.DebateKey {
		return &ErrorMessage{
			ErrorCode:   "INVALID_DEBATE_KEY",
			Message:     "Invalid debate key",
			DebateID:    speech.DebateID,
			Recoverable: false,
		}
	}

	// Check turn
	expectedSpeaker := dm.getNextSpeaker(activeDebate)
	if speech.Speaker != expectedSpeaker {
		return &ErrorMessage{
			ErrorCode:   "NOT_YOUR_TURN",
			Message:     "It's not your turn to speak",
			DebateID:    speech.DebateID,
			Recoverable: true,
		}
	}

	// Cancel timeout
	if activeDebate.TimeoutTimer != nil {
		activeDebate.TimeoutTimer.Stop()
	}

	// Update last activity time and reset inactivity timer
	activeDebate.LastActivityTime = time.Now()
	dm.resetInactivityTimer(speech.DebateID)

	// Validate content length
	contentLen := len(strings.TrimSpace(speech.Message.Content))
	if contentLen < config.Debate.MinContentLength {
		return &ErrorMessage{
			ErrorCode:   "CONTENT_TOO_SHORT",
			Message:     fmt.Sprintf("Speech content too short (minimum %d characters)", config.Debate.MinContentLength),
			DebateID:    speech.DebateID,
			Recoverable: true,
		}
	}
	if contentLen > config.Debate.MaxContentLength {
		return &ErrorMessage{
			ErrorCode:   "CONTENT_TOO_LONG",
			Message:     fmt.Sprintf("Speech content too long (maximum %d characters)", config.Debate.MaxContentLength),
			DebateID:    speech.DebateID,
			Recoverable: true,
		}
	}

	// Add to debate log
	logEntry := DebateLogEntry{
		Round:     activeDebate.Debate.CurrentRound,
		Speaker:   speech.Speaker,
		Side:      speakerBot.Bot.Side,
		Timestamp: time.Now().Format(time.RFC3339),
		Message:   speech.Message,
	}

	activeDebate.mutex.Lock()
	activeDebate.DebateLog = append(activeDebate.DebateLog, logEntry)
	activeDebate.LastSpeaker = speech.Speaker
	activeDebate.mutex.Unlock()

	// Save to database
	dm.db.AddDebateLog(&logEntry, speech.DebateID)

	// Determine next speaker and update round
	var nextSpeaker string

	if speech.Speaker == activeDebate.SupportingBot.Bot.BotIdentifier {
		// Supporting spoke, opposing is next
		nextSpeaker = activeDebate.OpposingBot.Bot.BotIdentifier
	} else {
		// Opposing spoke, round complete, supporting starts next round
		activeDebate.Debate.CurrentRound++
		dm.db.UpdateDebateRound(speech.DebateID, activeDebate.Debate.CurrentRound)

		// Check if debate is complete
		if activeDebate.Debate.CurrentRound > activeDebate.Debate.TotalRounds {
			dm.endDebate(speech.DebateID, "completed", "completed")
			return nil
		}

		nextSpeaker = activeDebate.SupportingBot.Bot.BotIdentifier
	}

	// Send update to both bots
	dm.sendDebateUpdate(activeDebate, nextSpeaker)

	// Start timeout for next speaker
	dm.startTimeout(speech.DebateID, nextSpeaker)

	return nil
}

// sendDebateUpdate sends current debate state to both bots
func (dm *DebateManager) sendDebateUpdate(activeDebate *ActiveDebate, nextSpeaker string) {
	activeDebate.mutex.RLock()
	defer activeDebate.mutex.RUnlock()

	// Send to supporting bot
	updateMsgA := createMessage("debate_update", DebateUpdate{
		DebateID:         activeDebate.Debate.ID,
		Topic:            activeDebate.Debate.Topic,
		SupportingSide:   activeDebate.SupportingBot.Bot.BotIdentifier,
		OpposingSide:     activeDebate.OpposingBot.Bot.BotIdentifier,
		TotalRounds:      activeDebate.Debate.TotalRounds,
		CurrentRound:     activeDebate.Debate.CurrentRound,
		YourSide:         "supporting",
		YourIdentifier:   activeDebate.SupportingBot.Bot.BotIdentifier,
		NextSpeaker:      nextSpeaker,
		TimeoutSeconds:   120,
		MinContentLength: config.Debate.MinContentLength,
		MaxContentLength: config.Debate.MaxContentLength,
		DebateLog:        activeDebate.DebateLog,
	})

	// Send to opposing bot
	updateMsgB := createMessage("debate_update", DebateUpdate{
		DebateID:         activeDebate.Debate.ID,
		Topic:            activeDebate.Debate.Topic,
		SupportingSide:   activeDebate.SupportingBot.Bot.BotIdentifier,
		OpposingSide:     activeDebate.OpposingBot.Bot.BotIdentifier,
		TotalRounds:      activeDebate.Debate.TotalRounds,
		CurrentRound:     activeDebate.Debate.CurrentRound,
		YourSide:         "opposing",
		YourIdentifier:   activeDebate.OpposingBot.Bot.BotIdentifier,
		NextSpeaker:      nextSpeaker,
		TimeoutSeconds:   120,
		MinContentLength: config.Debate.MinContentLength,
		MaxContentLength: config.Debate.MaxContentLength,
		DebateLog:        activeDebate.DebateLog,
	})

	activeDebate.SupportingBot.Conn.WriteJSON(updateMsgA)
	activeDebate.OpposingBot.Conn.WriteJSON(updateMsgB)

	// Broadcast to frontend
	dm.broadcast <- BroadcastMessage{
		DebateID: activeDebate.Debate.ID,
		Message:  updateMsgA,
	}
}

// getNextSpeaker determines who should speak next
func (dm *DebateManager) getNextSpeaker(activeDebate *ActiveDebate) string {
	if activeDebate.LastSpeaker == "" {
		return activeDebate.SupportingBot.Bot.BotIdentifier
	}
	if activeDebate.LastSpeaker == activeDebate.SupportingBot.Bot.BotIdentifier {
		return activeDebate.OpposingBot.Bot.BotIdentifier
	}
	return activeDebate.SupportingBot.Bot.BotIdentifier
}

// startTimeout starts a timeout timer for a speaker
func (dm *DebateManager) startTimeout(debateID, speaker string) {
	dm.mutex.RLock()
	activeDebate, exists := dm.debates[debateID]
	dm.mutex.RUnlock()

	if !exists {
		return
	}

	activeDebate.TimeoutTimer = time.AfterFunc(
		time.Duration(config.Debate.SpeechTimeout)*time.Second,
		func() {
			log.Printf("%d Timeout for %s in debate %s ",
				config.Debate.SpeechTimeout,
				speaker,
				debateID,
			)
			dm.endDebate(debateID, "timeout", "speech_timeout")
		},
	)
}

// endDebate ends a debate and generates summary
// reason: specific reason for ending (e.g., "completed", "speech_timeout", "inactivity_timeout", "max_duration_timeout", "bot_disconnected", "heartbeat_timeout")
func (dm *DebateManager) endDebate(debateID, status, reason string) {
	dm.mutex.RLock()
	activeDebate, exists := dm.debates[debateID]
	dm.mutex.RUnlock()

	if !exists {
		return
	}

	// Cancel any pending timers
	if activeDebate.WaitingTimer != nil {
		activeDebate.WaitingTimer.Stop()
	}
	if activeDebate.TimeoutTimer != nil {
		activeDebate.TimeoutTimer.Stop()
	}
	if activeDebate.InactivityTimer != nil {
		activeDebate.InactivityTimer.Stop()
	}
	if activeDebate.MaxDurationTimer != nil {
		activeDebate.MaxDurationTimer.Stop()
	}

	// Update status
	dm.db.UpdateDebateStatus(debateID, status)
	activeDebate.Debate.Status = status

	// Generate summary (simplified - in production, use AI)
	result := dm.generateDebateResult(activeDebate, status, reason)

	// Save result
	dm.db.SaveDebateResult(debateID, result)

	// Get bot identifiers safely
	supportingSide := "未连接"
	opposingSide := "未连接"
	if activeDebate.SupportingBot != nil {
		supportingSide = activeDebate.SupportingBot.Bot.BotIdentifier
	}
	if activeDebate.OpposingBot != nil {
		opposingSide = activeDebate.OpposingBot.Bot.BotIdentifier
	}

	// Send end message to both bots
	endMsg := createMessage("debate_end", DebateEnd{
		DebateID:       debateID,
		Topic:          activeDebate.Debate.Topic,
		SupportingSide: supportingSide,
		OpposingSide:   opposingSide,
		TotalRounds:    activeDebate.Debate.TotalRounds,
		Status:         status,
		DebateLog:      activeDebate.DebateLog,
		DebateResult:   *result,
	})

	if activeDebate.SupportingBot != nil && activeDebate.SupportingBot.Conn != nil {
		activeDebate.SupportingBot.Conn.WriteJSON(endMsg)
	}
	if activeDebate.OpposingBot != nil && activeDebate.OpposingBot.Conn != nil {
		activeDebate.OpposingBot.Conn.WriteJSON(endMsg)
	}

	// Broadcast to frontend
	dm.broadcast <- BroadcastMessage{
		DebateID: debateID,
		Message:  endMsg,
	}

	log.Printf("Debate %s ended with status: %s", debateID, status)
}

// generateDebateResult creates a debate result (simplified)
// reason: specific reason for ending (e.g., "completed", "speech_timeout", "inactivity_timeout", "max_duration_timeout", "bot_disconnected_{bot_id}", "heartbeat_timeout_{bot_id}")
func (dm *DebateManager) generateDebateResult(activeDebate *ActiveDebate, status, reason string) *DebateResult {
	// Count speeches from each side
	supportingCount := 0
	opposingCount := 0
	for _, entry := range activeDebate.DebateLog {
		if entry.Side == "supporting" {
			supportingCount++
		} else {
			opposingCount++
		}
	}

	// Check if we should use ChatGPT for judging
	// Only use ChatGPT if:
	// 1. ChatGPT is enabled
	// 2. Both bots are present
	// 3. Both sides have spoken (at least 1 speech each)
	shouldUseAI := chatgptClient != nil &&
		activeDebate.SupportingBot != nil &&
		activeDebate.OpposingBot != nil &&
		supportingCount > 0 &&
		opposingCount > 0

	if shouldUseAI {
		result, err := chatgptClient.JudgeDebate(
			activeDebate.Debate.Topic,
			activeDebate.DebateLog,
			activeDebate.SupportingBot.Bot.BotIdentifier,
			activeDebate.OpposingBot.Bot.BotIdentifier,
		)
		if err == nil {
			log.Printf("ChatGPT judge completed for debate %s: %s wins", activeDebate.Debate.ID, result.Winner)
			return result
		}
		log.Printf("ChatGPT judge failed, using fallback: %v", err)
	} else if status == "timeout" && (supportingCount == 0 || opposingCount == 0) {
		log.Printf("Skipping AI judge for debate %s: timeout with insufficient speeches (supporting: %d, opposing: %d)",
			activeDebate.Debate.ID, supportingCount, opposingCount)
	}

	// Fallback: simple scoring or timeout result

	supportingScore := 45 + (supportingCount * 2)
	opposingScore := 45 + (opposingCount * 2)

	if supportingScore > 50 {
		supportingScore = 50
	}
	if opposingScore > 50 {
		opposingScore = 50
	}

	// Normalize to 100
	total := supportingScore + opposingScore
	supportingScore = supportingScore * 100 / total
	opposingScore = 100 - supportingScore

	// Determine winner
	winner := "none"

	// Only determine winner if both sides have spoken
	if supportingCount > 0 && opposingCount > 0 {
		if supportingScore > opposingScore+5 {
			winner = "supporting"
		} else if opposingScore > supportingScore+5 {
			winner = "opposing"
		}
	} 

	// Get bot identifiers safely
	supportingID := "未连接"
	opposingID := "未连接"
	if activeDebate.SupportingBot != nil {
		supportingID = activeDebate.SupportingBot.Bot.BotIdentifier
	}
	if activeDebate.OpposingBot != nil {
		opposingID = activeDebate.OpposingBot.Bot.BotIdentifier
	}

	// Generate reason description
	reasonDesc := dm.getReasonDescription(reason, supportingID, opposingID)

	// Generate summary based on status
	var summary string
	if status == "timeout" && (supportingCount == 0 && opposingCount == 0) {
		summary = fmt.Sprintf(`## 辩论超时

**辩题**: %s

### 正方: %s
状态: 未发言

### 反方: %s
状态: 未发言

### 结果
辩论因超时而结束，双方均未发言。

**结束原因**: %s

**获胜方**: 无`, activeDebate.Debate.Topic, supportingID, opposingID, reasonDesc)
	} else if status == "timeout" && (supportingCount == 0 || opposingCount == 0) {
		summary = fmt.Sprintf(`## 辩论超时

**辩题**: %s

### 正方 (%s)
- 发言次数: %d

### 反方 (%s)
- 发言次数: %d

### 结果
辩论因超时而结束，仅有一方发言，无法进行完整评判。

**结束原因**: %s

**获胜方**: 无`, activeDebate.Debate.Topic,
			supportingID, supportingCount,
			opposingID, opposingCount,
			reasonDesc)
	} else {
		summary = fmt.Sprintf(`## 辩论总结

**辩题**: %s

### 正方 (%s)
- 发言次数: %d
- 得分: %d

### 反方 (%s)
- 发言次数: %d
- 得分: %d

### 结果
**获胜方**: %s

注: 使用简单计分规则，ChatGPT评判不可用。

感谢两位选手的精彩辩论！`, activeDebate.Debate.Topic,
			supportingID, supportingCount, supportingScore,
			opposingID, opposingCount, opposingScore,
			winner)
	}

	return &DebateResult{
		Winner:          winner,
		SupportingScore: supportingScore,
		OpposingScore:   opposingScore,
		Summary: SpeechMessage{
			Format:  "markdown",
			Content: summary,
		},
		Reason: reason,
	}
}

// AddFrontendConnection adds a frontend WebSocket connection
func (dm *DebateManager) AddFrontendConnection(debateID string, conn *websocket.Conn) error {
	dm.mutex.Lock()
	defer dm.mutex.Unlock()

	activeDebate, exists := dm.debates[debateID]
	if !exists {
		return fmt.Errorf("debate not found")
	}

	activeDebate.mutex.Lock()
	activeDebate.FrontendConns[conn] = true
	activeDebate.mutex.Unlock()

	return nil
}

// RemoveFrontendConnection removes a frontend connection
func (dm *DebateManager) RemoveFrontendConnection(debateID string, conn *websocket.Conn) {
	dm.mutex.Lock()
	defer dm.mutex.Unlock()

	activeDebate, exists := dm.debates[debateID]
	if !exists {
		return
	}

	activeDebate.mutex.Lock()
	delete(activeDebate.FrontendConns, conn)
	activeDebate.mutex.Unlock()
}

// Helper functions

func generateDebateKey() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return "key-" + hex.EncodeToString(bytes)
}

func randomBool() bool {
	n, _ := rand.Int(rand.Reader, big.NewInt(2))
	return n.Int64() == 1
}

func createMessage(msgType string, data interface{}) Message {
	return Message{
		Type:      msgType,
		Timestamp: time.Now().Format(time.RFC3339),
		Data:      data,
	}
}

// startInactivityTimer starts the inactivity timeout timer
func (dm *DebateManager) startInactivityTimer(debateID string) {
	dm.mutex.RLock()
	activeDebate, exists := dm.debates[debateID]
	dm.mutex.RUnlock()

	if !exists {
		return
	}

	inactivityTimeout := time.Duration(config.Debate.InactivityTimeout) * time.Second

	activeDebate.InactivityTimer = time.AfterFunc(inactivityTimeout, func() {
		elapsed := time.Since(activeDebate.LastActivityTime)
		log.Printf("Inactivity timeout for debate %s (no activity for %v)", debateID, elapsed)
		dm.endDebate(debateID, "timeout", "inactivity_timeout")
	})
}

// resetInactivityTimer resets the inactivity timeout timer
func (dm *DebateManager) resetInactivityTimer(debateID string) {
	dm.mutex.RLock()
	activeDebate, exists := dm.debates[debateID]
	dm.mutex.RUnlock()

	if !exists {
		return
	}

	if activeDebate.InactivityTimer != nil {
		activeDebate.InactivityTimer.Stop()
	}

	dm.startInactivityTimer(debateID)
}

// startMaxDurationTimer starts the maximum duration timer
func (dm *DebateManager) startMaxDurationTimer(debateID string) {
	dm.mutex.RLock()
	activeDebate, exists := dm.debates[debateID]
	dm.mutex.RUnlock()

	if !exists {
		return
	}

	maxDuration := time.Duration(config.Debate.MaxDuration) * time.Second

	activeDebate.MaxDurationTimer = time.AfterFunc(maxDuration, func() {
		elapsed := time.Since(activeDebate.StartTime)
		log.Printf("Max duration timeout for debate %s (running for %v)", debateID, elapsed)
		dm.endDebate(debateID, "timeout", "max_duration_timeout")
	})
}

// startWaitingTimer starts a timer for debates in waiting state
// If both bots don't connect within the timeout, the debate is marked as timeout
func (dm *DebateManager) startWaitingTimer(debateID string) {
	dm.mutex.RLock()
	activeDebate, exists := dm.debates[debateID]
	dm.mutex.RUnlock()

	if !exists {
		return
	}

	waitingTimeout := time.Duration(config.Debate.WaitingTimeout) * time.Second

	activeDebate.WaitingTimer = time.AfterFunc(waitingTimeout, func() {
		dm.mutex.RLock()
		debate, exists := dm.debates[debateID]
		dm.mutex.RUnlock()

		if !exists {
			return
		}

		// Check if debate is still in waiting state
		if debate.Debate.Status == "waiting" {
			log.Printf("Waiting timeout for debate %s (no bots connected or only 1 bot)", debateID)

			// Update status to timeout
			dm.db.UpdateDebateStatus(debateID, "timeout")
			debate.Debate.Status = "timeout"

			// Clean up from active debates map
			dm.mutex.Lock()
			delete(dm.debates, debateID)
			dm.mutex.Unlock()
		}
	})

	log.Printf("Waiting timer started for debate %s (timeout: %v)", debateID, waitingTimeout)
}

// getReasonDescription returns a human-readable description of the debate end reason
func (dm *DebateManager) getReasonDescription(reason, supportingBot, opposingBot string) string {
	switch {
	case reason == "completed":
		return "辩论正常完成"
	case reason == "speech_timeout":
		return fmt.Sprintf("发言超时（Bot 未在 %d 秒内发言）", config.Debate.SpeechTimeout)
	case reason == "inactivity_timeout":
		return fmt.Sprintf("长时间无活动（超过 %d 秒无新发言）", config.Debate.InactivityTimeout)
	case reason == "max_duration_timeout":
		return fmt.Sprintf("辩论时长超过限制（超过 %d 秒）", config.Debate.MaxDuration)
	case strings.HasPrefix(reason, "bot_disconnected_"):
		botID := strings.TrimPrefix(reason, "bot_disconnected_")
		return fmt.Sprintf("Bot %s 断开连接", botID)
	case strings.HasPrefix(reason, "heartbeat_timeout_"):
		botID := strings.TrimPrefix(reason, "heartbeat_timeout_")
		return fmt.Sprintf("Bot %s 心跳超时（连续 3 次未响应 pong）", botID)
	default:
		return reason
	}
}

// HandleBotDisconnect handles bot disconnection (including heartbeat timeout)
func (dm *DebateManager) HandleBotDisconnect(debateID, botIdentifier string, reason string) {
	dm.mutex.RLock()
	activeDebate, exists := dm.debates[debateID]
	dm.mutex.RUnlock()

	if !exists {
		log.Printf("Bot %s disconnected from non-existent debate %s", botIdentifier, debateID)
		return
	}

	log.Printf("Bot %s disconnected from debate %s (reason: %s, status: %s)",
		botIdentifier, debateID, reason, activeDebate.Debate.Status)

	// Only end debate if it's currently active
	if activeDebate.Debate.Status == "active" {
		log.Printf("Ending debate %s due to bot %s disconnection", debateID, botIdentifier)
		// Include bot identifier in the reason
		detailedReason := fmt.Sprintf("%s_%s", reason, botIdentifier)
		dm.endDebate(debateID, "timeout", detailedReason)
	} else if activeDebate.Debate.Status == "waiting" {
		// If still waiting for bots to join, just log it
		log.Printf("Bot %s disconnected while debate %s is still waiting", botIdentifier, debateID)
	}
}
