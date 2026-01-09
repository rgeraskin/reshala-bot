package claude

import (
	"strings"
)

type ToolExecution struct {
	ToolName string `json:"tool_name"`
	Status   string `json:"status"`
	Input    string `json:"input,omitempty"`
	Output   string `json:"output,omitempty"`
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
