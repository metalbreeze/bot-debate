package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the application configuration
type Config struct {
	Server struct {
		Host string `yaml:"host"`
		Port int    `yaml:"port"`
	} `yaml:"server"`

	Database struct {
		Path string `yaml:"path"`
	} `yaml:"database"`

	Debate struct {
		SpeechTimeout      int `yaml:"speech_timeout"`
		InactivityTimeout  int `yaml:"inactivity_timeout"`
		MaxDuration        int `yaml:"max_duration"`
		MinContentLength   int `yaml:"min_content_length"`
		MaxContentLength   int `yaml:"max_content_length"`
	} `yaml:"debate"`

	ChatGPT struct {
		APIKey  string `yaml:"api_key"`
		APIURL  string `yaml:"api_url"`
		Model   string `yaml:"model"`
		Timeout int    `yaml:"timeout"`

		Judge struct {
			Enabled     bool    `yaml:"enabled"`
			MaxTokens   int     `yaml:"max_tokens"`
			Temperature float64 `yaml:"temperature"`
		} `yaml:"judge"`
	} `yaml:"chatgpt"`
}

// LoadConfig loads configuration from config.yml
func LoadConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Set defaults
	if config.Server.Host == "" {
		config.Server.Host = "0.0.0.0"
	}
	if config.Server.Port == 0 {
		config.Server.Port = 8081
	}
	if config.Database.Path == "" {
		config.Database.Path = "./debate.db"
	}
	if config.ChatGPT.APIURL == "" {
		config.ChatGPT.APIURL = "https://api.openai.com/v1/chat/completions"
	}
	if config.ChatGPT.Model == "" {
		config.ChatGPT.Model = "gpt-4"
	}
	if config.ChatGPT.Timeout == 0 {
		config.ChatGPT.Timeout = 30
	}
	if config.ChatGPT.Judge.MaxTokens == 0 {
		config.ChatGPT.Judge.MaxTokens = 1000
	}
	if config.ChatGPT.Judge.Temperature == 0 {
		config.ChatGPT.Judge.Temperature = 0.7
	}
	if config.Debate.SpeechTimeout == 0 {
		config.Debate.SpeechTimeout = 120
	}
	if config.Debate.InactivityTimeout == 0 {
		config.Debate.InactivityTimeout = 1800 // 30 minutes
	}
	if config.Debate.MaxDuration == 0 {
		config.Debate.MaxDuration = 3600 // 1 hour
	}
	if config.Debate.MinContentLength == 0 {
		config.Debate.MinContentLength = 50
	}
	if config.Debate.MaxContentLength == 0 {
		config.Debate.MaxContentLength = 2000
	}

	// Override API key from environment variables if present
	// Priority: OPENAI_API_KEY > CHATGPT_API_KEY > config file
	if envKey := os.Getenv("OPENAI_API_KEY"); envKey != "" {
		config.ChatGPT.APIKey = envKey
		log.Printf("Using ChatGPT API key from OPENAI_API_KEY environment variable")
	} else if envKey := os.Getenv("CHATGPT_API_KEY"); envKey != "" {
		config.ChatGPT.APIKey = envKey
		log.Printf("Using ChatGPT API key from CHATGPT_API_KEY environment variable")
	}

	return &config, nil
}
