package brand

import (
	_ "embed"
	"regexp"
	"strings"
)

//go:embed cows/sheep.cow
var sheepCowSource string

var cowHeredocRE = regexp.MustCompile(`(?s)\$the_cow\s*=\s*<<EOC\n(.*?)EOC`)

// RenderSheep renders the embedded cowsay sheep.cow with the given eye glyphs.
// When thoughts is empty, leading thought-pointer lines are trimmed.
func RenderSheep(eyes string) []string {
	if eyes == "" {
		eyes = "oo"
	}
	if len([]rune(eyes)) > 2 {
		eyes = string([]rune(eyes)[:2])
	}

	raw := parseCowHeredoc(sheepCowSource)
	raw = strings.ReplaceAll(raw, "${eyes}", eyes)
	raw = strings.ReplaceAll(raw, "$eyes", eyes)
	raw = strings.ReplaceAll(raw, "$thoughts", "")

	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, " ")
		if len(out) == 0 && strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

func parseCowHeredoc(source string) string {
	m := cowHeredocRE.FindStringSubmatch(source)
	if len(m) < 2 {
		return ""
	}
	body := m[1]
	body = strings.ReplaceAll(body, `\@`, `@`)
	body = strings.ReplaceAll(body, `\\`, `\`)
	return body
}
