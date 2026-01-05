package claude

import (
	"encoding/json"
	"strings"
)

type ClaudeResponse struct {
	Type    string `json:"type"`
	Content string `json:"content"`
	Done    bool   `json:"done"`
}

type ToolExecution struct {
	ToolName string `json:"tool_name"`
	Status   string `json:"status"`
	Input    string `json:"input,omitempty"`
	Output   string `json:"output,omitempty"`
}

func ParseResponse(raw string) (*ClaudeResponse, error) {
	var response ClaudeResponse
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		response = ClaudeResponse{
			Type:    "text",
			Content: raw,
			Done:    true,
		}
	}
	return &response, nil
}

func ExtractToolExecutions(raw string) []ToolExecution {
	var tools []ToolExecution

	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		if strings.Contains(line, "Tool:") {
			tool := parseToolLine(line)
			if tool != nil {
				tools = append(tools, *tool)
			}
		}
	}

	return tools
}

func parseToolLine(line string) *ToolExecution {
	parts := strings.SplitN(line, "Tool:", 2)
	if len(parts) != 2 {
		return nil
	}

	toolInfo := strings.TrimSpace(parts[1])
	return &ToolExecution{
		ToolName: toolInfo,
		Status:   "success",
	}
}

func FormatResponse(raw string) string {
	lines := strings.Split(raw, "\n")
	var formatted []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			formatted = append(formatted, trimmed)
		}
	}

	return strings.Join(formatted, "\n")
}

func SplitLongMessage(message string, maxLength int) []string {
	if len(message) <= maxLength {
		return []string{message}
	}

	var chunks []string
	lines := strings.Split(message, "\n")
	var currentChunk strings.Builder

	for _, line := range lines {
		if currentChunk.Len()+len(line)+1 > maxLength {
			if currentChunk.Len() > 0 {
				chunks = append(chunks, currentChunk.String())
				currentChunk.Reset()
			}

			if len(line) > maxLength {
				for i := 0; i < len(line); i += maxLength {
					end := i + maxLength
					if end > len(line) {
						end = len(line)
					}
					chunks = append(chunks, line[i:end])
				}
			} else {
				currentChunk.WriteString(line)
				currentChunk.WriteString("\n")
			}
		} else {
			if currentChunk.Len() > 0 {
				currentChunk.WriteString("\n")
			}
			currentChunk.WriteString(line)
		}
	}

	if currentChunk.Len() > 0 {
		chunks = append(chunks, currentChunk.String())
	}

	return chunks
}
