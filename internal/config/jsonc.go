package config

// stripJSONC removes JSONC-style comments and trailing commas from src,
// returning valid JSON. It uses a 5-state machine:
//
//	Default      – normal JSON territory
//	lineComment  – inside a // … \n comment
//	blockComment – inside a /* … */ comment
//	inString     – inside a "…" string literal
//	escape       – the character immediately after a backslash inside a string
//
// Trailing commas before } or ] are also removed so that standard
// encoding/json can parse the result.
func stripJSONC(src []byte) []byte {
	const (
		stDefault      = iota
		stLineComment  // inside //...
		stBlockComment // inside /*...*/
		stString       // inside "..."
		stEscape       // \ inside "..."
	)

	out := make([]byte, 0, len(src))
	state := stDefault

	emit := func(b byte) { out = append(out, b) }

	for i := 0; i < len(src); i++ {
		b := src[i]

		switch state {
		case stString:
			emit(b)
			if b == '\\' {
				state = stEscape
			} else if b == '"' {
				state = stDefault
			}

		case stEscape:
			emit(b)
			state = stString

		case stLineComment:
			if b == '\n' {
				emit(b)
				state = stDefault
			}
			// skip all other bytes in line comment

		case stBlockComment:
			if b == '*' && i+1 < len(src) && src[i+1] == '/' {
				i++ // skip '/'
				state = stDefault
			}
			// skip comment body

		case stDefault:
			switch {
			case b == '"':
				emit(b)
				state = stString

			case b == '/' && i+1 < len(src) && src[i+1] == '/':
				i++ // skip second '/'
				state = stLineComment

			case b == '/' && i+1 < len(src) && src[i+1] == '*':
				i++ // skip '*'
				state = stBlockComment

			case b == '}' || b == ']':
				// Drop trailing comma (with optional whitespace) before this closer.
				out = dropTrailingComma(out)
				emit(b)

			default:
				emit(b)
			}
		}
	}
	return out
}

// dropTrailingComma scans backwards through buf past whitespace and removes
// a trailing ',' if found, returning the trimmed buffer.
func dropTrailingComma(buf []byte) []byte {
	i := len(buf) - 1
	for i >= 0 && isJSONWhitespace(buf[i]) {
		i--
	}
	if i >= 0 && buf[i] == ',' {
		// Remove the comma and any whitespace after it that was already emitted.
		return buf[:i]
	}
	return buf
}

func isJSONWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
