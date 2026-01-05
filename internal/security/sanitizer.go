package security

import (
	"fmt"
	"log"
	"regexp"
	"strings"
)

type Sanitizer struct {
	patterns []*regexp.Regexp
}

func NewSanitizer(patterns []string) *Sanitizer {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			log.Printf("Warning: failed to compile pattern %q: %v", pattern, err)
			continue
		}
		compiled = append(compiled, re)
	}
	return &Sanitizer{
		patterns: compiled,
	}
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
		log.Printf("Security: Redacted sensitive information from output")
	}

	return result
}

func (s *Sanitizer) SanitizeMultiple(texts ...string) []string {
	result := make([]string, len(texts))
	for i, text := range texts {
		result[i] = s.Sanitize(text)
	}
	return result
}

func (s *Sanitizer) ContainsSensitiveData(text string) bool {
	for _, pattern := range s.patterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	return false
}

func (s *Sanitizer) Validate() error {
	if len(s.patterns) == 0 {
		return fmt.Errorf("no security patterns configured")
	}
	return nil
}

func (s *Sanitizer) String() string {
	return fmt.Sprintf("Sanitizer with %d patterns", len(s.patterns))
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

func SanitizeEnvVars(envVars []string, allowList []string) []string {
	allowed := make(map[string]bool)
	for _, key := range allowList {
		allowed[key] = true
	}

	result := make([]string, 0, len(envVars))
	for _, env := range envVars {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		if allowed[key] {
			result = append(result, env)
		}
	}
	return result
}
