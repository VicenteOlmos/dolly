package tui

import "github.com/VicenteOlmos/dolly/internal/dumphistory"

func (a *App) openDumpHistoryStore() (dumphistory.Store, error) {
	if a.dumpHistoryStore != nil {
		return a.dumpHistoryStore, nil
	}
	if a.cfg == nil {
		return nil, nil
	}
	return dumphistory.OpenStore(a.cfg, ".")
}

func (a *App) refreshDumpHistoryList() {
	store, err := a.openDumpHistoryStore()
	if err != nil || store == nil {
		return
	}
	refreshDumpHistory(&a.dump, store)
}

func (a *App) registerDumpHistory(outDir string, seq int, schemas []string) {
	store, err := a.openDumpHistoryStore()
	if err != nil || store == nil {
		return
	}
	if err := dumphistory.RegisterCompletedDump(store, a.dump.OutputDir, seq, outDir, a.conn.Database, schemas); err != nil {
		appendDumpLog(&a.dumpLog, "history error: "+err.Error())
		a.statusMsg = truncateStatus(StyleWarning.Render("Dump history: "+err.Error()), a.width)
		return
	}
	a.refreshDumpHistoryList()
}
