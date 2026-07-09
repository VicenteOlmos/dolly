package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/dumphistory"
)

type dumpListFlags struct {
	Output string
	JSON   bool
}

func dumpListFlagSet(flags *dumpListFlags) *flag.FlagSet {
	fs := flag.NewFlagSet("dump list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&flags.Output, "output", "", "dump base directory (default: config dump.output_dir)")
	fs.BoolVar(&flags.JSON, "json", false, "emit JSON array of records")
	return fs
}

func parseDumpListFlags(args []string) (dumpListFlags, error) {
	if wantsHelp(args) {
		printDumpListUsage()
		return dumpListFlags{}, errHelp
	}
	var flags dumpListFlags
	fs := dumpListFlagSet(&flags)
	fs.Usage = printDumpListUsage
	if err := fs.Parse(args); err != nil {
		return flags, mapFlagHelp(err)
	}
	return flags, nil
}

func runDumpList(args []string) (err error) {
	flags, ferr := parseDumpListFlags(args)

	defer func() {
		if err != nil && flags.JSON {
			emitJSONError(os.Stderr, "dump list", err.Error())
			err = errJSONHandled
		}
	}()

	if ferr != nil {
		if errors.Is(ferr, errHelp) {
			return nil
		}
		err = ferr
		return
	}

	cfg, err := dumpLoadConfig(config.ResolveConfigPath())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	base := flags.Output
	if base == "" {
		base = cfg.Dump.OutputDir
	}
	if base == "" {
		return errors.New("required flag --output or config dump.output_dir")
	}

	store, err := dumphistory.OpenStore(cfg, ".")
	if err != nil {
		return fmt.Errorf("open dump history: %w", err)
	}

	recs, err := dumphistory.ListBaseMerged(base, store)
	if err != nil {
		return fmt.Errorf("list dumps: %w", err)
	}

	if flags.JSON {
		if recs == nil {
			recs = []dumphistory.Record{}
		}
		data, err := json.MarshalIndent(recs, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal dump list: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	if len(recs) == 0 {
		fmt.Printf("No dumps under %s\n", base)
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SEQ\tPATH\tSCHEMA\tTABLES\tSOURCE\tCREATED")
	for _, r := range recs {
		created := ""
		if !r.CreatedAt.IsZero() {
			created = r.CreatedAt.UTC().Format("2006-01-02 15:04")
		}
		source := r.SourceDatabase
		if source == "" {
			source = "-"
		}
		schema := r.SchemaLabel
		if schema == "" {
			schema = "?"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%d\t%s\t%s\n",
			r.Seq, r.Path, schema, r.TableCount, source, created)
	}
	return w.Flush()
}

func printDumpListUsage() {
	fmt.Fprintln(os.Stderr, "usage: dolly dump list [flags]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "List completed dumps for a base output directory (read-only, no DB connection).")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Flags:")
	fmt.Fprintln(os.Stderr, "  --output string")
	fmt.Fprintln(os.Stderr, "        dump base directory (default: config dump.output_dir)")
	fmt.Fprintln(os.Stderr, "  --json")
	fmt.Fprintln(os.Stderr, "        emit JSON array of history records")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Examples:")
	fmt.Fprintln(os.Stderr, "  dolly dump list")
	fmt.Fprintln(os.Stderr, "  dolly dump list --output ./dolly_dump")
	fmt.Fprintln(os.Stderr, "  dolly dump list --json")
}

func isDumpListInvocation(args []string) bool {
	if len(args) == 0 {
		return false
	}
	return strings.EqualFold(args[0], "list")
}
