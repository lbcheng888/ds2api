package openai

import textclean "ds2api/internal/textclean"

func sanitizeLeakedOutput(text string) string {
	return textclean.SanitizeLeakedOutput(text)
}
