package tui

import "strings"

type SectionEntryMode int

const (
	SectionEntryOverview SectionEntryMode = iota
	SectionEntryInside
)

func ParseSectionEntry(raw string) SectionEntryMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "inside":
		return SectionEntryInside
	default:
		return SectionEntryOverview
	}
}

func (m SectionEntryMode) String() string {
	if m == SectionEntryInside {
		return "inside"
	}
	return "overview"
}
