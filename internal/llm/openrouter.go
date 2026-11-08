package llm

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"stratest/internal/config"
	"stratest/internal/strategy"
)

//go:embed prompt.md
var promptTemplate string

var Endpoint = "https://openrouter.ai/api/v1/chat/completions"

type Response struct {
	Timeframes []string `json:"timeframes"`
	Base       string   `json:"base"`
	WarmupDays int      `json:"warmup_days"`
	Code       string   `json:"code"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func PromptHash(description, model string) string {
	return strategy.Hash(description, promptTemplate, model)
}

func prompt(description string) string {
	var out strings.Builder
	template.Must(template.New("prompt").Parse(promptTemplate)).Execute(&out, map[string]string{"Description": description})
	return out.String()
}

func complete(model string, messages []message) (string, error) {
	if config.OpenRouterKey() == "" {
		return "", errors.New("OPENROUTER_API_KEY is not set (copy .env.example to .env)")
	}
	body, _ := json.Marshal(map[string]any{"model": model, "temperature": 0, "messages": messages})
	request, _ := http.NewRequest("POST", Endpoint, bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+config.OpenRouterKey())
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	var parsed struct {
		Choices []struct{ Message message }
		Error   *struct{ Message string }
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openrouter: %s: %.300s", response.Status, raw)
	}
	if parsed.Error != nil || len(parsed.Choices) == 0 {
		return "", fmt.Errorf("openrouter: no completion: %.300s", raw)
	}
	return parsed.Choices[0].Message.Content, nil
}

// parse tolerates prose or code fences around the JSON object.
func parse(text string) (Response, error) {
	var r Response
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return r, errors.New("response contains no JSON object")
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &r); err != nil {
		return r, fmt.Errorf("response is not valid JSON: %w", err)
	}
	if strings.TrimSpace(r.Code) == "" {
		return r, errors.New("response has no code")
	}
	return r, nil
}

// Generate turns a description into validated strategy code, retrying once with the validation
// error fed back. Only validated responses are cached, keyed by description, template and model.
func Generate(description, model string, useCache bool) (r Response, cached bool, err error) {
	path := filepath.Join(config.LLMDir, PromptHash(description, model)+".json")
	if raw, err := os.ReadFile(path); useCache && err == nil && json.Unmarshal(raw, &r) == nil {
		return r, true, nil
	}
	messages := []message{{"user", prompt(description)}}
	for attempt := 1; ; attempt++ {
		text, err := complete(model, messages)
		if err != nil {
			return r, false, err
		}
		r, err = parse(text)
		if err == nil {
			err = strategy.Validate(r.Code, r.Timeframes, r.Base)
		}
		if err == nil {
			break
		}
		if attempt == 2 {
			return r, false, fmt.Errorf("generated code failed validation twice: %w", err)
		}
		messages = append(messages, message{"assistant", text}, message{"user", "That failed validation: " + err.Error() + "\nFix it and reply with the corrected JSON only."})
	}
	if err := os.MkdirAll(config.LLMDir, 0o755); err != nil {
		return r, false, err
	}
	raw, _ := json.MarshalIndent(r, "", "  ")
	return r, false, os.WriteFile(path, raw, 0o644)
}
