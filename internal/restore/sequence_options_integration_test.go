//go:build integration

package restore

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/VicenteOlmos/dolly/internal/dump"
)

func TestIntegrationRestoreWidensIntegerSequenceToBigint(t *testing.T) {
	conn := openIntegrationDB(t)
	ctx := context.Background()
	schema := fmt.Sprintf("dolly_seqwide_%d", time.Now().UnixNano())
	qSchema := quoteIdentifier(schema)
	if _, err := conn.ExecContext(ctx, "CREATE SCHEMA "+qSchema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), "DROP SCHEMA IF EXISTS "+qSchema+" CASCADE")
	})
	if _, err := conn.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE %s.u (id integer);
		CREATE SEQUENCE %s.u_id_seq AS integer INCREMENT BY 1 MINVALUE 1 MAXVALUE 2147483647 CACHE 1 NO CYCLE OWNED BY %s.u.id;
		SELECT setval('%s.u_id_seq', 3, true);
	`, qSchema, qSchema, qSchema, schema)); err != nil {
		t.Fatal(err)
	}
	inc, min, max, cache := int64(1), int64(1), int64(math.MaxInt64), int64(1)
	meta := dump.Metadata{
		Tables: []db.Table{{Schema: schema, Name: "u", Columns: []db.Column{{Name: "id"}}}},
		Sequences: []dump.SequenceState{{
			Schema: schema, Name: "u_id_seq", StartValue: 1,
			LastValue: ptrInt64(20), IsCalled: true,
			IncrementBy: &inc, MinValue: &min, MaxValue: &max, CacheSize: &cache,
			Cycle: ptrBool(false), DataType: "bigint",
		}},
	}
	if err := RestoreSequencesFromMetadata(ctx, conn, meta, []string{schema}, nil); err != nil {
		t.Fatal(err)
	}
	var dataType string
	var gotMax, last int64
	if err := conn.QueryRowContext(ctx, `
		SELECT data_type, max_value, last_value
		FROM pg_sequences
		WHERE schemaname = $1 AND sequencename = 'u_id_seq'`, schema).Scan(&dataType, &gotMax, &last); err != nil {
		t.Fatal(err)
	}
	if dataType != "bigint" || gotMax != math.MaxInt64 || last != 20 {
		t.Fatalf("sequence = %s max=%d last=%d, want bigint %d 20", dataType, gotMax, last, int64(math.MaxInt64))
	}
}

func TestIntegrationRestoreKeepsAdvancedSequenceUsable(t *testing.T) {
	conn := openIntegrationDB(t)
	ctx := context.Background()
	schema := fmt.Sprintf("dolly_seqbound_%d", time.Now().UnixNano())
	qSchema := quoteIdentifier(schema)
	if _, err := conn.ExecContext(ctx, "CREATE SCHEMA "+qSchema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), "DROP SCHEMA IF EXISTS "+qSchema+" CASCADE")
	})
	if _, err := conn.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE %s.v (id integer);
		CREATE SEQUENCE %s.v_id_seq AS bigint INCREMENT BY 1 MINVALUE 1 MAXVALUE 1000000 CACHE 1 NO CYCLE OWNED BY %s.v.id;
		SELECT setval('%s.v_id_seq', 1000, true);
	`, qSchema, qSchema, qSchema, schema)); err != nil {
		t.Fatal(err)
	}
	inc, min, max, cache := int64(1), int64(1), int64(100), int64(1)
	meta := dump.Metadata{
		Tables: []db.Table{{Schema: schema, Name: "v", Columns: []db.Column{{Name: "id"}}}},
		Sequences: []dump.SequenceState{{
			Schema: schema, Name: "v_id_seq", StartValue: 1,
			LastValue: ptrInt64(5), IsCalled: true,
			IncrementBy: &inc, MinValue: &min, MaxValue: &max, CacheSize: &cache,
			Cycle: ptrBool(true), DataType: "bigint",
		}},
	}
	if err := RestoreSequencesFromMetadata(ctx, conn, meta, []string{schema}, nil); err != nil {
		t.Fatal(err)
	}
	var gotMax, last int64
	var cycle bool
	if err := conn.QueryRowContext(ctx, `
		SELECT max_value, last_value, cycle
		FROM pg_sequences
		WHERE schemaname = $1 AND sequencename = 'v_id_seq'`, schema).Scan(&gotMax, &last, &cycle); err != nil {
		t.Fatal(err)
	}
	if gotMax != 1000000 || last != 1000 || cycle {
		t.Fatalf("max=%d last=%d cycle=%v, want max 1000000 last 1000 cycle false", gotMax, last, cycle)
	}
	var next int64
	if err := conn.QueryRowContext(ctx, fmt.Sprintf(`SELECT nextval('%s.v_id_seq')`, schema)).Scan(&next); err != nil || next != 1001 {
		t.Fatalf("nextval = %d, err = %v, want 1001", next, err)
	}
}

func TestIntegrationRestoreNonOwnerSkipsMatchingAlter(t *testing.T) {
	conn := openIntegrationDB(t)
	ctx := context.Background()
	conn.SetMaxOpenConns(1)
	schema := fmt.Sprintf("dolly_seqown_%d", time.Now().UnixNano())
	role := fmt.Sprintf("dolly_seqrole_%d", time.Now().UnixNano())
	qSchema := quoteIdentifier(schema)
	qRole := quoteIdentifier(role)
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = conn.ExecContext(bg, "RESET ROLE")
		_, _ = conn.ExecContext(bg, "DROP SCHEMA IF EXISTS "+qSchema+" CASCADE")
		_, _ = conn.ExecContext(bg, "DROP ROLE IF EXISTS "+qRole)
		conn.SetMaxOpenConns(0)
	})
	if _, err := conn.ExecContext(ctx, "CREATE SCHEMA "+qSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE %s.t (id integer);
		CREATE SEQUENCE %s.t_id_seq AS integer INCREMENT BY 1 MINVALUE 1 MAXVALUE 1000 CACHE 1 NO CYCLE OWNED BY %s.t.id;
		SELECT setval('%s.t_id_seq', 1, true);
		CREATE ROLE %s;
		GRANT USAGE ON SCHEMA %s TO %s;
		GRANT SELECT, UPDATE ON SEQUENCE %s.t_id_seq TO %s;
	`, qSchema, qSchema, qSchema, schema, qRole, qSchema, qRole, qSchema, qRole)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "SET ROLE "+qRole); err != nil {
		t.Fatal(err)
	}
	inc, min, max, cache := int64(1), int64(1), int64(1000), int64(1)
	meta := dump.Metadata{
		Tables: []db.Table{{Schema: schema, Name: "t", Columns: []db.Column{{Name: "id"}}}},
		Sequences: []dump.SequenceState{{
			Schema: schema, Name: "t_id_seq", StartValue: 1,
			LastValue: ptrInt64(10), IsCalled: true,
			IncrementBy: &inc, MinValue: &min, MaxValue: &max, CacheSize: &cache,
			Cycle: ptrBool(false), DataType: "integer",
		}},
	}
	if err := RestoreSequencesFromMetadata(ctx, conn, meta, []string{schema}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "RESET ROLE"); err != nil {
		t.Fatal(err)
	}
	var last int64
	if err := conn.QueryRowContext(ctx, `
		SELECT last_value FROM pg_sequences
		WHERE schemaname = $1 AND sequencename = 't_id_seq'`, schema).Scan(&last); err != nil || last != 10 {
		t.Fatalf("last_value = %d, err = %v, want 10", last, err)
	}
}
