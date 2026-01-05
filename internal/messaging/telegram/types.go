package telegram

const (
	MaxMessageLength = 4096
)

func SplitMessage(text string, maxLength int) []string {
	if len(text) <= maxLength {
		return []string{text}
	}

	var chunks []string
	remaining := text

	for len(remaining) > 0 {
		if len(remaining) <= maxLength {
			chunks = append(chunks, remaining)
			break
		}

		splitIndex := maxLength
		for i := maxLength - 1; i >= maxLength-200 && i > 0; i-- {
			if remaining[i] == '\n' {
				splitIndex = i
				break
			}
		}

		chunks = append(chunks, remaining[:splitIndex])
		remaining = remaining[splitIndex:]
	}

	return chunks
}
