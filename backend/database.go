package main

import (
	"database/sql"
	"encoding/json"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Database handles all database operations
type Database struct {
	db *sql.DB
}

// NewDatabase creates a new database connection
func NewDatabase(dbPath string) (*Database, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	database := &Database{db: db}
	if err := database.createTables(); err != nil {
		return nil, err
	}

	return database, nil
}

// createTables initializes database schema
func (d *Database) createTables() error {
	schema := `
	CREATE TABLE IF NOT EXISTS debates (
		id TEXT PRIMARY KEY,
		topic TEXT NOT NULL,
		total_rounds INTEGER NOT NULL,
		current_round INTEGER DEFAULT 1,
		status TEXT DEFAULT 'waiting',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS bots (
		bot_name TEXT NOT NULL,
		bot_uuid TEXT NOT NULL,
		bot_identifier TEXT NOT NULL,
		debate_id TEXT NOT NULL,
		debate_key TEXT NOT NULL,
		side TEXT,
		connected_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (debate_id, bot_uuid),
		FOREIGN KEY (debate_id) REFERENCES debates(id)
	);

	CREATE TABLE IF NOT EXISTS debate_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		debate_id TEXT NOT NULL,
		round INTEGER NOT NULL,
		speaker TEXT NOT NULL,
		side TEXT NOT NULL,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		message_format TEXT NOT NULL,
		message_content TEXT NOT NULL,
		FOREIGN KEY (debate_id) REFERENCES debates(id)
	);

	CREATE TABLE IF NOT EXISTS debate_results (
		debate_id TEXT PRIMARY KEY,
		winner TEXT NOT NULL,
		supporting_score INTEGER NOT NULL,
		opposing_score INTEGER NOT NULL,
		summary_format TEXT NOT NULL,
		summary_content TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (debate_id) REFERENCES debates(id)
	);

	CREATE INDEX IF NOT EXISTS idx_debates_status ON debates(status);
	CREATE INDEX IF NOT EXISTS idx_bots_debate ON bots(debate_id);
	CREATE INDEX IF NOT EXISTS idx_debate_log_debate ON debate_log(debate_id);
	`

	_, err := d.db.Exec(schema)
	return err
}

// CreateDebate creates a new debate session
func (d *Database) CreateDebate(debate *Debate) error {
	query := `INSERT INTO debates (id, topic, total_rounds, current_round, status, created_at, updated_at)
	          VALUES (?, ?, ?, ?, ?, ?, ?)`
	_, err := d.db.Exec(query, debate.ID, debate.Topic, debate.TotalRounds, debate.CurrentRound,
		debate.Status, debate.CreatedAt, debate.UpdatedAt)
	return err
}

// GetDebate retrieves a debate by ID
func (d *Database) GetDebate(debateID string) (*Debate, error) {
	query := `SELECT id, topic, total_rounds, current_round, status, created_at, updated_at
	          FROM debates WHERE id = ?`

	debate := &Debate{}
	err := d.db.QueryRow(query, debateID).Scan(
		&debate.ID, &debate.Topic, &debate.TotalRounds, &debate.CurrentRound,
		&debate.Status, &debate.CreatedAt, &debate.UpdatedAt)

	if err != nil {
		return nil, err
	}
	return debate, nil
}

// UpdateDebateStatus updates debate status
func (d *Database) UpdateDebateStatus(debateID, status string) error {
	query := `UPDATE debates SET status = ?, updated_at = ? WHERE id = ?`
	_, err := d.db.Exec(query, status, time.Now(), debateID)
	return err
}

// UpdateDebateRound updates current round
func (d *Database) UpdateDebateRound(debateID string, round int) error {
	query := `UPDATE debates SET current_round = ?, updated_at = ? WHERE id = ?`
	_, err := d.db.Exec(query, round, time.Now(), debateID)
	return err
}

// AddBot registers a bot to a debate
func (d *Database) AddBot(bot *Bot) error {
	query := `INSERT INTO bots (bot_name, bot_uuid, bot_identifier, debate_id, debate_key, side, connected_at)
	          VALUES (?, ?, ?, ?, ?, ?, ?)`
	_, err := d.db.Exec(query, bot.BotName, bot.BotUUID, bot.BotIdentifier, bot.DebateID,
		bot.DebateKey, bot.Side, bot.ConnectedAt)
	return err
}

// GetBots retrieves all bots for a debate
func (d *Database) GetBots(debateID string) ([]*Bot, error) {
	query := `SELECT bot_name, bot_uuid, bot_identifier, debate_id, debate_key, side, connected_at
	          FROM bots WHERE debate_id = ?`

	rows, err := d.db.Query(query, debateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bots []*Bot
	for rows.Next() {
		bot := &Bot{}
		err := rows.Scan(&bot.BotName, &bot.BotUUID, &bot.BotIdentifier, &bot.DebateID,
			&bot.DebateKey, &bot.Side, &bot.ConnectedAt)
		if err != nil {
			return nil, err
		}
		bots = append(bots, bot)
	}
	return bots, nil
}

// GetBotByIdentifier retrieves a specific bot
func (d *Database) GetBotByIdentifier(debateID, botIdentifier string) (*Bot, error) {
	query := `SELECT bot_name, bot_uuid, bot_identifier, debate_id, debate_key, side, connected_at
	          FROM bots WHERE debate_id = ? AND bot_identifier = ?`

	bot := &Bot{}
	err := d.db.QueryRow(query, debateID, botIdentifier).Scan(
		&bot.BotName, &bot.BotUUID, &bot.BotIdentifier, &bot.DebateID,
		&bot.DebateKey, &bot.Side, &bot.ConnectedAt)

	if err != nil {
		return nil, err
	}
	return bot, nil
}

// UpdateBotSide assigns a side to a bot
func (d *Database) UpdateBotSide(debateID, botIdentifier, side string) error {
	query := `UPDATE bots SET side = ? WHERE debate_id = ? AND bot_identifier = ?`
	_, err := d.db.Exec(query, side, debateID, botIdentifier)
	return err
}

// AddDebateLog adds a speech to the debate log
func (d *Database) AddDebateLog(entry *DebateLogEntry, debateID string) error {
	query := `INSERT INTO debate_log (debate_id, round, speaker, side, timestamp, message_format, message_content)
	          VALUES (?, ?, ?, ?, ?, ?, ?)`
	_, err := d.db.Exec(query, debateID, entry.Round, entry.Speaker, entry.Side,
		entry.Timestamp, entry.Message.Format, entry.Message.Content)
	return err
}

// GetDebateLog retrieves all speeches for a debate
func (d *Database) GetDebateLog(debateID string) ([]DebateLogEntry, error) {
	query := `SELECT round, speaker, side, timestamp, message_format, message_content
	          FROM debate_log WHERE debate_id = ? ORDER BY id ASC`

	rows, err := d.db.Query(query, debateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var log []DebateLogEntry
	for rows.Next() {
		var entry DebateLogEntry
		var format, content string
		err := rows.Scan(&entry.Round, &entry.Speaker, &entry.Side, &entry.Timestamp, &format, &content)
		if err != nil {
			return nil, err
		}
		entry.Message = SpeechMessage{Format: format, Content: content}
		log = append(log, entry)
	}
	return log, nil
}

// SaveDebateResult saves the final result
func (d *Database) SaveDebateResult(debateID string, result *DebateResult) error {
	query := `INSERT INTO debate_results (debate_id, winner, supporting_score, opposing_score, summary_format, summary_content)
	          VALUES (?, ?, ?, ?, ?, ?)`
	_, err := d.db.Exec(query, debateID, result.Winner, result.SupportingScore, result.OpposingScore,
		result.Summary.Format, result.Summary.Content)
	return err
}

// GetDebateResult retrieves the debate result
func (d *Database) GetDebateResult(debateID string) (*DebateResult, error) {
	query := `SELECT winner, supporting_score, opposing_score, summary_format, summary_content
	          FROM debate_results WHERE debate_id = ?`

	result := &DebateResult{}
	var format, content string
	err := d.db.QueryRow(query, debateID).Scan(
		&result.Winner, &result.SupportingScore, &result.OpposingScore, &format, &content)

	if err != nil {
		return nil, err
	}
	result.Summary = SpeechMessage{Format: format, Content: content}
	return result, nil
}

// GetAvailableDebate finds a waiting debate with less than 2 bots
func (d *Database) GetAvailableDebate() (*Debate, error) {
	query := `
		SELECT d.id, d.topic, d.total_rounds, d.current_round, d.status, d.created_at, d.updated_at
		FROM debates d
		LEFT JOIN (
			SELECT debate_id, COUNT(*) as bot_count
			FROM bots
			GROUP BY debate_id
		) b ON d.id = b.debate_id
		WHERE d.status = 'waiting' AND (b.bot_count IS NULL OR b.bot_count < 2)
		ORDER BY d.created_at ASC
		LIMIT 1`

	debate := &Debate{}
	err := d.db.QueryRow(query).Scan(
		&debate.ID, &debate.Topic, &debate.TotalRounds, &debate.CurrentRound,
		&debate.Status, &debate.CreatedAt, &debate.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil // No available debate
	}
	if err != nil {
		return nil, err
	}
	return debate, nil
}

// GetAllDebates retrieves all debates with optional status filter
func (d *Database) GetAllDebates(status string) ([]*Debate, error) {
	var query string
	var rows *sql.Rows
	var err error

	if status != "" {
		query = `SELECT id, topic, total_rounds, current_round, status, created_at, updated_at
		         FROM debates WHERE status = ? ORDER BY created_at DESC`
		rows, err = d.db.Query(query, status)
	} else {
		query = `SELECT id, topic, total_rounds, current_round, status, created_at, updated_at
		         FROM debates ORDER BY created_at DESC`
		rows, err = d.db.Query(query)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var debates []*Debate
	for rows.Next() {
		debate := &Debate{}
		err := rows.Scan(&debate.ID, &debate.Topic, &debate.TotalRounds, &debate.CurrentRound,
			&debate.Status, &debate.CreatedAt, &debate.UpdatedAt)
		if err != nil {
			return nil, err
		}
		debates = append(debates, debate)
	}
	return debates, nil
}

// Close closes the database connection
func (d *Database) Close() error {
	return d.db.Close()
}

// Helper function to convert struct to JSON string
func toJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
