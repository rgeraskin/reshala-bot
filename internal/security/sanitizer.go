package security

import (
	"fmt"
	"log/slog"
	"regexp"
)

type Sanitizer struct {
	patterns []*regexp.Regexp
}

func NewSanitizer(patterns []string) (*Sanitizer, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid security pattern %q: %w", pattern, err)
		}
		compiled = append(compiled, re)
	}
	return &Sanitizer{
		patterns: compiled,
	}, nil
}

func (s *Sanitizer) Sanitize(text string) string {
	result := text
	redacted := false

	for _, pattern := range s.patterns {
		if pattern.MatchString(result) {
			result = pattern.ReplaceAllString(result, "***REDACTED***")
			redacted = true
		}
	}

	if redacted {
		slog.Info("Security: Redacted sensitive information from output")
	}

	return result
}

var DefaultPatterns = []string{
	`api[_-]?key[s]?\s*[:=]\s*["']?([^"'\s]+)`,
	`token[s]?\s*[:=]\s*["']?([^"'\s]+)`,
	`password[s]?\s*[:=]\s*["']?([^"'\s]+)`,
	`secret[s]?\s*[:=]\s*["']?([^"'\s]+)`,
	`[A-Za-z0-9+/]{40,}={0,2}`,
	`xox[pboa]-[0-9]{10,13}-[0-9]{10,13}-[0-9]{10,13}-[a-z0-9]{32}`,
	`eyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+`,
}
