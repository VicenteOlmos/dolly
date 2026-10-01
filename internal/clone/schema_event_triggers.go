package clone

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

type eventTriggerRow struct {
	name       string
	event      string
	evtenabled string
	tags       []string
	fnSchema   string
	fnName     string
	fnArgs     string
	prokind    string
}

func loadEventTriggers(ctx context.Context, q *sql.DB, schemas []string) ([]eventTriggerRow, error) {
	allowed := make(map[string]struct{}, len(schemas))
	for _, schema := range schemas {
		allowed[schema] = struct{}{}
	}
	const query = `
		SELECT e.evtname,
		       e.evtevent,
		       e.evtenabled,
		       COALESCE(array_to_json(e.evttags)::text, '[]'),
		       pn.nspname,
		       p.proname,
		       pg_get_function_identity_arguments(p.oid),
		       p.prokind
		FROM pg_event_trigger e
		JOIN pg_proc p ON p.oid = e.evtfoid
		JOIN pg_namespace pn ON pn.oid = p.pronamespace
		WHERE e.evtenabled <> 'D'
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_depend d
		    WHERE d.classid = 'pg_event_trigger'::regclass
		      AND d.objid = e.oid
		      AND d.deptype = 'e'
		  )
		ORDER BY e.evtname`
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list event triggers: %w", err)
	}
	defer rows.Close()

	var out []eventTriggerRow
	for rows.Next() {
		var row eventTriggerRow
		var tagsJSON string
		if err := rows.Scan(&row.name, &row.event, &row.evtenabled, &tagsJSON, &row.fnSchema, &row.fnName, &row.fnArgs, &row.prokind); err != nil {
			return nil, fmt.Errorf("scan event trigger: %w", err)
		}
		if row.fnSchema != "pg_catalog" {
			if _, ok := allowed[row.fnSchema]; !ok {
				continue
			}
		}
		if tagsJSON != "" && tagsJSON != "null" {
			if err := json.Unmarshal([]byte(tagsJSON), &row.tags); err != nil {
				return nil, fmt.Errorf("parse event trigger tags for %q: %w", row.name, err)
			}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list event triggers: %w", err)
	}
	return out, nil
}

func applyEventTriggers(ctx context.Context, tgt execer, triggers []eventTriggerRow) error {
	for _, et := range triggers {
		stmt := formatCreateEventTrigger(et)
		if _, err := tgt.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create event trigger %q: %w", et.name, err)
		}
		if alter := formatAlterEventTriggerEnabled(et); alter != "" {
			if _, err := tgt.ExecContext(ctx, alter); err != nil {
				return fmt.Errorf("alter event trigger %q: %w", et.name, err)
			}
		}
	}
	return nil
}

func formatAlterEventTriggerEnabled(et eventTriggerRow) string {
	switch et.evtenabled {
	case "R":
		return "ALTER EVENT TRIGGER " + quoteIdentifier(et.name) + " ENABLE REPLICA"
	case "A":
		return "ALTER EVENT TRIGGER " + quoteIdentifier(et.name) + " ENABLE ALWAYS"
	default:
		return ""
	}
}

func formatCreateEventTrigger(et eventTriggerRow) string {
	var b strings.Builder
	b.WriteString("CREATE EVENT TRIGGER ")
	b.WriteString(quoteIdentifier(et.name))
	b.WriteString(" ON ")
	b.WriteString(et.event)
	if len(et.tags) > 0 {
		quoted := make([]string, len(et.tags))
		for i, tag := range et.tags {
			quoted[i] = quoteLiteral(tag)
		}
		b.WriteString(" WHEN TAG IN (")
		b.WriteString(strings.Join(quoted, ", "))
		b.WriteString(")")
	}
	kind := "FUNCTION"
	if et.prokind == "p" {
		kind = "PROCEDURE"
	}
	b.WriteString(" EXECUTE ")
	b.WriteString(kind)
	b.WriteString(" ")
	b.WriteString(formatRoutineCall(et.fnSchema, et.fnName, et.fnArgs))
	return b.String()
}

func formatRoutineCall(schema, name, args string) string {
	call := quoteQualifiedType(schema, name) + "(" + args + ")"
	return call
}
