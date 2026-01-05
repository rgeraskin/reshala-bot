package bot

import (
	"fmt"
	"strings"
)

type ResponseFormatter struct {
	maxLength int
}

func NewResponseFormatter(maxLength int) *ResponseFormatter {
	return &ResponseFormatter{
		maxLength: maxLength,
	}
}

func (rf *ResponseFormatter) Format(text string) string {
	text = strings.TrimSpace(text)

	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	lines := strings.Split(text, "\n")
	var cleaned []string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" || len(cleaned) > 0 {
			cleaned = append(cleaned, line)
		}
	}

	for len(cleaned) > 0 && strings.TrimSpace(cleaned[len(cleaned)-1]) == "" {
		cleaned = cleaned[:len(cleaned)-1]
	}

	return strings.Join(cleaned, "\n")
}

func (rf *ResponseFormatter) AddPrefix(text, prefix string) string {
	if prefix == "" {
		return text
	}
	return fmt.Sprintf("%s %s", prefix, text)
}

func (rf *ResponseFormatter) FormatError(err error) string {
	return fmt.Sprintf("❌ Error: %v", err)
}

func (rf *ResponseFormatter) FormatSuccess(message string) string {
	return fmt.Sprintf("✅ %s", message)
}

func (rf *ResponseFormatter) FormatWarning(message string) string {
	return fmt.Sprintf("⚠️ %s", message)
}

func (rf *ResponseFormatter) FormatInfo(message string) string {
	return fmt.Sprintf("ℹ️ %s", message)
}

func (rf *ResponseFormatter) Truncate(text string) string {
	if len(text) <= rf.maxLength {
		return text
	}
	return text[:rf.maxLength-3] + "..."
}

func (rf *ResponseFormatter) FormatCodeBlock(code, language string) string {
	if language == "" {
		return fmt.Sprintf("```\n%s\n```", code)
	}
	return fmt.Sprintf("```%s\n%s\n```", language, code)
}

func (rf *ResponseFormatter) FormatList(items []string) string {
	var sb strings.Builder
	for i, item := range items {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, item))
	}
	return sb.String()
}

func (rf *ResponseFormatter) FormatKeyValue(key, value string) string {
	return fmt.Sprintf("**%s:** %s", key, value)
}
