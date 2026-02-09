package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ChatGPTClient handles interactions with ChatGPT API
type ChatGPTClient struct {
	APIKey     string
	APIURL     string
	Model      string
	Timeout    time.Duration
	MaxTokens  int
	Temperature float64
}

// ChatGPTMessage represents a message in the conversation
type ChatGPTMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatGPTRequest represents the request to ChatGPT API
type ChatGPTRequest struct {
	Model       string           `json:"model"`
	Messages    []ChatGPTMessage `json:"messages"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Temperature float64          `json:"temperature,omitempty"`
}

// ChatGPTResponse represents the response from ChatGPT API
type ChatGPTResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// NewChatGPTClient creates a new ChatGPT client
func NewChatGPTClient(apiKey, apiURL, model string, timeout int, maxTokens int, temperature float64) *ChatGPTClient {
	return &ChatGPTClient{
		APIKey:      apiKey,
		APIURL:      apiURL,
		Model:       model,
		Timeout:     time.Duration(timeout) * time.Second,
		MaxTokens:   maxTokens,
		Temperature: temperature,
	}
}

// SendMessage sends a message to ChatGPT and returns the response
func (c *ChatGPTClient) SendMessage(messages []ChatGPTMessage) (string, error) {
	if c.APIKey == "" || c.APIKey == "your-api-key-here" {
		return "", fmt.Errorf("ChatGPT API key not configured")
	}

	reqBody := ChatGPTRequest{
		Model:       c.Model,
		Messages:    messages,
		MaxTokens:   c.MaxTokens,
		Temperature: c.Temperature,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", c.APIURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	client := &http.Client{
		Timeout: c.Timeout,
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var chatResp ChatGPTResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("no response from ChatGPT")
	}

	return chatResp.Choices[0].Message.Content, nil
}

// JudgeDebate analyzes a debate and determines the winner
func (c *ChatGPTClient) JudgeDebate(topic string, debateLog []DebateLogEntry, supportingBot, opposingBot string) (*DebateResult, error) {
	// Build debate transcript
	var transcript strings.Builder
	transcript.WriteString(fmt.Sprintf("辩题: %s\n\n", topic))
	transcript.WriteString(fmt.Sprintf("正方 (支持): %s\n", supportingBot))
	transcript.WriteString(fmt.Sprintf("反方 (反对): %s\n\n", opposingBot))
	transcript.WriteString("辩论过程:\n\n")

	for _, entry := range debateLog {
		sideName := "正方"
		if entry.Side == "opposing" {
			sideName = "反方"
		}
		transcript.WriteString(fmt.Sprintf("【第%d轮 - %s】\n%s\n\n", entry.Round, sideName, entry.Message.Content))
	}

	// Create judge prompt
	systemPrompt := `你是一位专业的辩论评委。请根据以下标准评判辩论：

评分标准 (总分100分):
1. 论点质量 (30分): 论点是否清晰、有力、有逻辑性
2. 论据支持 (25分): 是否提供充分的事实、数据、案例支持
3. 反驳能力 (20分): 是否有效反驳对方观点
4. 表达能力 (15分): 语言是否流畅、有说服力
5. 整体逻辑 (10分): 论证结构是否完整、严谨

请按以下JSON格式返回评判结果:
{
  "winner": "supporting" 或 "opposing" 或 "draw",
  "supporting_score": 0-100,
  "opposing_score": 0-100,
  "summary": "详细的评判总结，包括双方优缺点分析"
}`

	userPrompt := fmt.Sprintf("请评判以下辩论:\n\n%s", transcript.String())

	messages := []ChatGPTMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	response, err := c.SendMessage(messages)
	if err != nil {
		return nil, fmt.Errorf("failed to get judge response: %w", err)
	}

	// Parse response
	result, err := c.parseJudgeResponse(response)
	if err != nil {
		// If parsing fails, create a fallback result
		return &DebateResult{
			Winner:          "draw",
			SupportingScore: 50,
			OpposingScore:   50,
			Summary: SpeechMessage{
				Format:  "markdown",
				Content: fmt.Sprintf("## AI评判结果\n\n%s\n\n注意: 自动解析失败，以原始回复为准。", response),
			},
		}, nil
	}

	return result, nil
}

// parseJudgeResponse parses the ChatGPT judge response
func (c *ChatGPTClient) parseJudgeResponse(response string) (*DebateResult, error) {
	// Try to extract JSON from response
	startIdx := strings.Index(response, "{")
	endIdx := strings.LastIndex(response, "}")
	
	if startIdx == -1 || endIdx == -1 {
		return nil, fmt.Errorf("no JSON found in response")
	}

	jsonStr := response[startIdx : endIdx+1]

	var judgeData struct {
		Winner          string `json:"winner"`
		SupportingScore int    `json:"supporting_score"`
		OpposingScore   int    `json:"opposing_score"`
		Summary         string `json:"summary"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &judgeData); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Validate scores
	if judgeData.SupportingScore < 0 || judgeData.SupportingScore > 100 {
		judgeData.SupportingScore = 50
	}
	if judgeData.OpposingScore < 0 || judgeData.OpposingScore > 100 {
		judgeData.OpposingScore = 50
	}

	// Validate winner
	if judgeData.Winner != "supporting" && judgeData.Winner != "opposing" && judgeData.Winner != "draw" {
		judgeData.Winner = "draw"
	}

	return &DebateResult{
		Winner:          judgeData.Winner,
		SupportingScore: judgeData.SupportingScore,
		OpposingScore:   judgeData.OpposingScore,
		Summary: SpeechMessage{
			Format:  "markdown",
			Content: judgeData.Summary,
		},
	}, nil
}
