package tui

type SectionNavLevel int

const (
	SectionNavOverview SectionNavLevel = iota
	SectionNavInside
)

type SectionNav struct {
	Level      SectionNavLevel
	Section    int
	SectionCnt int
}

func NewSectionNav(sectionCount int) SectionNav {
	if sectionCount < 1 {
		sectionCount = 1
	}
	return SectionNav{SectionCnt: sectionCount}
}

func (n *SectionNav) MoveSection(delta int) {
	if n.SectionCnt <= 0 {
		return
	}
	n.Section += delta
	if n.Section < 0 {
		n.Section = n.SectionCnt - 1
	}
	if n.Section >= n.SectionCnt {
		n.Section = 0
	}
}

func (n *SectionNav) Enter() {
	if n.Level == SectionNavOverview {
		n.Level = SectionNavInside
	}
}

func (n *SectionNav) Exit() {
	n.Level = SectionNavOverview
}

func (n *SectionNav) EnterInside(section int) {
	if section < 0 || section >= n.SectionCnt {
		return
	}
	n.Section = section
	n.Level = SectionNavInside
}

func (n *SectionNav) InOverview() bool {
	return n.Level == SectionNavOverview
}

func (n *SectionNav) InInside() bool {
	return n.Level == SectionNavInside
}

func sectionRowPrefix(nav SectionNav, section int) string {
	if nav.InOverview() && nav.Section == section {
		return "> "
	}
	return "  "
}

func overviewSectionRow(nav SectionNav, section int, title, summary string) string {
	row := sectionRowPrefix(nav, section) + title
	if nav.InOverview() && nav.Section == section {
		row = StyleAccent.Render(row)
	} else {
		row = StyleBase.Render(row)
	}
	if summary != "" {
		row += "  " + StyleMuted.Render(summary)
	}
	return row
}
