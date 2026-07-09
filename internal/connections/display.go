package connections

import "fmt"

// DisplaySummary returns a masked summary of host, user, and database for list UIs.
// The password is never included.
func DisplaySummary(c Connection) string {
	return fmt.Sprintf("%s / %s / %s",
		maskField(c.Host),
		maskField(c.User),
		maskField(c.Database),
	)
}

func maskField(s string) string {
	if s == "" {
		return "***"
	}
	runes := []rune(s)
	if len(runes) <= 2 {
		return "***"
	}
	if len(runes) <= 4 {
		return string(runes[:1]) + "***"
	}
	return string(runes[:2]) + "***" + string(runes[len(runes)-1:])
}
