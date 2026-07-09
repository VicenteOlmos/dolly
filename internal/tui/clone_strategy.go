package tui

// cloneStrategyOption describes a selectable clone strategy in the TUI.
type cloneStrategyOption struct {
	Name        string
	Description string
}

var cloneStrategyOptions = []cloneStrategyOption{
	{
		Name:        "schema-replay",
		Description: "DDL replay, then dump + restore (default; supports sanitization)",
	},
	{
		Name:        "template",
		Description: "CREATE DATABASE … TEMPLATE on same server (fast, same instance only)",
	},
	{
		Name:        "logical-stream",
		Description: "Table-by-table COPY streaming (best for large cross-server clones)",
	},
	{
		Name:        "physical-backup",
		Description: "pg_basebackup cluster copy (entire data directory; needs target_dir)",
	},
}

// cloneStrategyChoices is the ordered list of strategy names for cyclers and config.
var cloneStrategyChoices = strategyNames(cloneStrategyOptions)

var cloneFormFieldHints = [cloneFormFieldCount]string{
	"Name of the new database on the target server",
	"Target server; Tab cycles Current / Saved / Manual",
	"", // strategy: use cloneStrategyDescription for the active choice
	"Preflight: table count and DB size before clone starts",
}

func strategyNames(opts []cloneStrategyOption) []string {
	names := make([]string, len(opts))
	for i, o := range opts {
		names[i] = o.Name
	}
	return names
}

func cloneStrategyDescription(name string) string {
	if name == "" {
		name = "schema-replay"
	}
	for _, o := range cloneStrategyOptions {
		if o.Name == name {
			return o.Description
		}
	}
	return ""
}
