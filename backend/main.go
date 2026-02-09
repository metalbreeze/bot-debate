package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

var (
	db            *Database
	debateManager *DebateManager
	config        *Config
	chatgptClient *ChatGPTClient
)

func main() {
	// Load configuration
	var err error
	config, err = LoadConfig("config.yml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	log.Printf("Configuration loaded successfully")

	// Initialize database
	db, err = NewDatabase(config.Database.Path)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Initialize ChatGPT client
	if config.ChatGPT.Judge.Enabled {
		chatgptClient = NewChatGPTClient(
			config.ChatGPT.APIKey,
			config.ChatGPT.APIURL,
			config.ChatGPT.Model,
			config.ChatGPT.Timeout,
			config.ChatGPT.Judge.MaxTokens,
			config.ChatGPT.Judge.Temperature,
		)
		if config.ChatGPT.APIKey != "" && config.ChatGPT.APIKey != "your-api-key-here" {
			log.Printf("ChatGPT judge enabled (model: %s)", config.ChatGPT.Model)
		} else {
			log.Printf("ChatGPT judge disabled (API key not configured)")
		}
	}

	// Initialize debate manager
	debateManager = NewDebateManager(db)

	// Setup routes
	http.HandleFunc("/debate", handleBotWebSocket)
	http.HandleFunc("/frontend", handleFrontendWebSocket)
	http.HandleFunc("/api/debates", handleDebatesAPI)
	http.HandleFunc("/api/debate/create", handleCreateDebate)
	http.HandleFunc("/api/debate/", handleGetDebate)

	// Serve static frontend files
	frontendPath := "../frontend"
	if _, err := os.Stat(frontendPath); !os.IsNotExist(err) {
		fs := http.FileServer(http.Dir(frontendPath))
		http.Handle("/", fs)
	}

	// Start server
	addr := fmt.Sprintf("%s:%d", config.Server.Host, config.Server.Port)
	log.Printf("Server starting on %s", addr)
	log.Printf("Bot WebSocket: ws://%s/debate", addr)
	log.Printf("Frontend WebSocket: ws://%s/frontend", addr)
	log.Printf("Frontend UI: http://%s", addr)

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

// handleBotWebSocket handles WebSocket connections from bots
func handleBotWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("Bot connected from %s", conn.RemoteAddr())

	// Wait for login message
	var msg Message
	if err := conn.ReadJSON(&msg); err != nil {
		log.Printf("Error reading login message: %v", err)
		return
	}

	if msg.Type != "bot_login" {
		sendError(conn, "INVALID_MESSAGE_TYPE", "Expected bot_login message", "", false)
		return
	}

	// Parse login request
	loginData, err := json.Marshal(msg.Data)
	if err != nil {
		sendError(conn, "INVALID_MESSAGE_FORMAT", "Failed to parse login data", "", false)
		return
	}

	var loginReq LoginRequest
	if err := json.Unmarshal(loginData, &loginReq); err != nil {
		sendError(conn, "INVALID_MESSAGE_FORMAT", "Invalid login request format", "", false)
		return
	}

	// Process login
	confirmed, rejected := debateManager.BotLogin(&loginReq, conn)
	if rejected != nil {
		conn.WriteJSON(createMessage("login_rejected", rejected))
		return
	}

	conn.WriteJSON(createMessage("login_confirmed", confirmed))
	log.Printf("Bot %s logged in to debate %s", confirmed.BotIdentifier, loginReq.DebateID)

	// Start heartbeat monitoring for this bot
	quitHeartbeat := make(chan bool)
	missedPings := 0

	// Start goroutine to send ping every 30 seconds
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// Check if we missed too many pongs (3 strikes)
				if missedPings >= 3 {
					log.Printf("Bot %s missed 3 pings, disconnecting", confirmed.BotIdentifier)
					// Handle heartbeat timeout
					debateManager.HandleBotDisconnect(loginReq.DebateID, confirmed.BotIdentifier, "heartbeat_timeout")
					conn.Close()
					return
				}

				// Send ping
				if err := conn.WriteJSON(createMessage("ping", map[string]string{
					"server_time": getNow(),
				})); err != nil {
					log.Printf("Failed to send ping to bot %s: %v", confirmed.BotIdentifier, err)
					return
				}

				// Increment missed pings (will be reset when pong is received)
				missedPings++
				log.Printf("Sent ping to bot %s (missed: %d)", confirmed.BotIdentifier, missedPings)

			case <-quitHeartbeat:
				return
			}
		}
	}()

	// Handle subsequent messages
	for {
		var msg Message
		if err := conn.ReadJSON(&msg); err != nil {
			log.Printf("Bot disconnected: %v", err)
			// Handle bot disconnection
			debateManager.HandleBotDisconnect(loginReq.DebateID, confirmed.BotIdentifier, "connection_lost")
			break
		}

		switch msg.Type {
		case "debate_speech":
			handleBotSpeech(conn, msg)
		case "pong":
			// Reset missed pings counter when pong is received
			missedPings = 0
			log.Printf("Received pong from bot %s", confirmed.BotIdentifier)
		default:
			log.Printf("Unknown message type from bot: %s", msg.Type)
		}
	}

	// Cleanup: stop heartbeat goroutine
	close(quitHeartbeat)
}

// handleBotSpeech processes a speech from a bot
func handleBotSpeech(conn *websocket.Conn, msg Message) {
	speechData, err := json.Marshal(msg.Data)
	if err != nil {
		sendError(conn, "INVALID_MESSAGE_FORMAT", "Failed to parse speech data", "", true)
		return
	}

	var speech DebateSpeech
	if err := json.Unmarshal(speechData, &speech); err != nil {
		sendError(conn, "INVALID_MESSAGE_FORMAT", "Invalid speech format", "", true)
		return
	}

	// Process speech
	if errMsg := debateManager.HandleSpeech(&speech, conn); errMsg != nil {
		conn.WriteJSON(createMessage("error", errMsg))
	}
}

// handleFrontendWebSocket handles WebSocket connections from frontend
func handleFrontendWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade frontend connection: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("Frontend connected from %s", conn.RemoteAddr())

	var debateID string

	// Wait for subscribe message
	for {
		var msg Message
		if err := conn.ReadJSON(&msg); err != nil {
			log.Printf("Frontend disconnected: %v", err)
			break
		}

		switch msg.Type {
		case "subscribe_debate":
			data, _ := json.Marshal(msg.Data)
			var sub SubscribeDebate
			if err := json.Unmarshal(data, &sub); err != nil {
				continue
			}

			debateID = sub.DebateID
			if err := debateManager.AddFrontendConnection(debateID, conn); err != nil {
				log.Printf("Failed to subscribe: %v", err)
				continue
			}

			log.Printf("Frontend subscribed to debate %s", debateID)

			// Send current state
			sendCurrentDebateState(conn, debateID)

		case "ping":
			conn.WriteJSON(createMessage("pong", map[string]string{
				"server_time": getNow(),
			}))
		}
	}

	// Cleanup on disconnect
	if debateID != "" {
		debateManager.RemoveFrontendConnection(debateID, conn)
	}
}

// sendCurrentDebateState sends the current debate state to a newly connected frontend
func sendCurrentDebateState(conn *websocket.Conn, debateID string) {
	debate, err := db.GetDebate(debateID)
	if err != nil {
		return
	}

	bots, _ := db.GetBots(debateID)
	debateLog, _ := db.GetDebateLog(debateID)

	var supportingBot, opposingBot *Bot
	for _, bot := range bots {
		if bot.Side == "supporting" {
			supportingBot = bot
		} else if bot.Side == "opposing" {
			opposingBot = bot
		}
	}

	if debate.Status == "completed" || debate.Status == "timeout" {
		// Send debate end
		result, _ := db.GetDebateResult(debateID)
		if result != nil {
			endMsg := createMessage("debate_end", DebateEnd{
				DebateID:       debateID,
				Topic:          debate.Topic,
				SupportingSide: supportingBot.BotIdentifier,
				OpposingSide:   opposingBot.BotIdentifier,
				TotalRounds:    debate.TotalRounds,
				Status:         debate.Status,
				DebateLog:      debateLog,
				DebateResult:   *result,
			})
			conn.WriteJSON(endMsg)
		}
	} else if debate.Status == "active" && supportingBot != nil && opposingBot != nil {
		// Send debate update
		updateMsg := createMessage("debate_update", DebateUpdate{
			DebateID:         debateID,
			Topic:            debate.Topic,
			SupportingSide:   supportingBot.BotIdentifier,
			OpposingSide:     opposingBot.BotIdentifier,
			TotalRounds:      debate.TotalRounds,
			CurrentRound:     debate.CurrentRound,
			MinContentLength: config.Debate.MinContentLength,
			MaxContentLength: config.Debate.MaxContentLength,
			DebateLog:        debateLog,
		})
		conn.WriteJSON(updateMsg)
	} else if debate.Status == "waiting" {
		// Send debate waiting state with joined bots
		joinedBots := []string{}
		for _, bot := range bots {
			joinedBots = append(joinedBots, bot.BotIdentifier)
		}
		waitingMsg := createMessage("debate_waiting", DebateWaiting{
			DebateID:    debateID,
			Topic:       debate.Topic,
			TotalRounds: debate.TotalRounds,
			Status:      debate.Status,
			JoinedBots:  joinedBots,
		})
		conn.WriteJSON(waitingMsg)
	}
}

// handleCreateDebate handles debate creation from frontend
func handleCreateDebate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CreateDebateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.Topic == "" {
		http.Error(w, "Topic is required", http.StatusBadRequest)
		return
	}

	if req.TotalRounds <= 0 {
		req.TotalRounds = 5
	}

	debate, err := debateManager.CreateDebate(req.Topic, req.TotalRounds)
	if err != nil {
		http.Error(w, "Failed to create debate", http.StatusInternalServerError)
		return
	}

	response := DebateCreated{
		DebateID:    debate.ID,
		Topic:       debate.Topic,
		TotalRounds: debate.TotalRounds,
		Status:      debate.Status,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
	log.Printf("Debate created: %s - %s", debate.ID, debate.Topic)
}

// handleDebatesAPI returns list of all debates
func handleDebatesAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := r.URL.Query().Get("status")
	debates, err := db.GetAllDebates(status)
	if err != nil {
		http.Error(w, "Failed to fetch debates", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(debates)
}

// handleGetDebate returns a specific debate
func handleGetDebate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	debateID := filepath.Base(r.URL.Path)
	debate, err := db.GetDebate(debateID)
	if err != nil {
		http.Error(w, "Debate not found", http.StatusNotFound)
		return
	}

	bots, _ := db.GetBots(debateID)
	debateLog, _ := db.GetDebateLog(debateID)
	result, _ := db.GetDebateResult(debateID)

	response := map[string]interface{}{
		"debate":     debate,
		"bots":       bots,
		"debate_log": debateLog,
		"result":     result,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Helper functions

func sendError(conn *websocket.Conn, errorCode, message, debateID string, recoverable bool) {
	errMsg := createMessage("error", ErrorMessage{
		ErrorCode:   errorCode,
		Message:     message,
		DebateID:    debateID,
		Recoverable: recoverable,
	})
	conn.WriteJSON(errMsg)
}

func getNow() string {
	return createMessage("", nil).Timestamp
}
