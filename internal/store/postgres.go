package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres persists records in PostgreSQL. The pool is safe for concurrent
// use; every request is an independent transaction-free statement keyed by a
// generated id, so concurrent calculations can never mix their history.
type Postgres struct {
	pool *pgxpool.Pool
}

// NewPostgres connects (with the caller-supplied connection string), verifies
// the connection and ensures the schema exists.
func NewPostgres(ctx context.Context, dsn string) (*Postgres, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database dsn: %w", err)
	}
	cfg.MaxConns = 10
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	p := &Postgres{pool: pool}
	if err := p.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if err := p.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return p, nil
}

func (p *Postgres) migrate(ctx context.Context) error {
	_, err := p.pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS steering_records (
    id          TEXT PRIMARY KEY,
    batch_id    TEXT NOT NULL DEFAULT '',
    item_index  INTEGER NOT NULL DEFAULT -1,
    kind        TEXT NOT NULL,
    source      TEXT NOT NULL,
    status      TEXT NOT NULL,
    wheelbase_m DOUBLE PRECISION NOT NULL DEFAULT 0,
    track_m     DOUBLE PRECISION NOT NULL DEFAULT 0,
    inner_deg   DOUBLE PRECISION NOT NULL DEFAULT 0,
    angle_unit  TEXT NOT NULL DEFAULT '',
    direction   TEXT NOT NULL DEFAULT '',
    radius_m    DOUBLE PRECISION,
    request     JSONB NOT NULL,
    result      JSONB NOT NULL,
    error       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_steering_created ON steering_records (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_steering_batch   ON steering_records (batch_id);
CREATE INDEX IF NOT EXISTS idx_steering_status  ON steering_records (status);
CREATE INDEX IF NOT EXISTS idx_steering_dir     ON steering_records (direction);
CREATE INDEX IF NOT EXISTS idx_steering_source  ON steering_records (source);
	`)
	if err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	return nil
}

// Save inserts one record. It is called twice per record (insert with a null
// result then update once the id is known) only when the result needs the id;
// normally callers marshal the full result first and call Save once.
func (p *Postgres) Save(ctx context.Context, r Record) (Record, error) {
	if r.ID == "" {
		r.ID = NewID("rec")
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	_, err := p.pool.Exec(ctx, `
INSERT INTO steering_records
  (id, batch_id, item_index, kind, source, status, wheelbase_m, track_m,
   inner_deg, angle_unit, direction, radius_m, request, result, error, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		r.ID, r.BatchID, r.Index, r.Kind, r.Source, r.Status,
		r.Wheelbase, r.Track, r.InnerDeg, r.AngleUnit, r.Direction, r.Radius,
		string(r.Request), string(r.Result), r.Error, r.CreatedAt)
	if err != nil {
		return Record{}, fmt.Errorf("insert record: %w", err)
	}
	return r, nil
}

func (p *Postgres) Get(ctx context.Context, id string) (Record, bool, error) {
	row := p.pool.QueryRow(ctx, baseSelect+` WHERE id = $1`, id)
	r, err := scan(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, err
	}
	return r, true, nil
}

func (p *Postgres) Query(ctx context.Context, f Filter) ([]Record, int, error) {
	var where []string
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if f.Status != "" {
		add("status = $%d", f.Status)
	}
	if f.Kind != "" {
		add("kind = $%d", f.Kind)
	}
	if f.Source != "" {
		add("source = $%d", f.Source)
	}
	if f.Direction != "" {
		add("direction = $%d", f.Direction)
	}
	if f.BatchID != "" {
		add("batch_id = $%d", f.BatchID)
	}
	if f.MinRadius != 0 {
		add("radius_m >= $%d", f.MinRadius)
	}
	if f.MaxRadius != 0 {
		add("radius_m <= $%d", f.MaxRadius)
	}
	if !f.Since.IsZero() {
		add("created_at >= $%d", f.Since)
	}
	if !f.Until.IsZero() {
		add("created_at <= $%d", f.Until)
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := p.pool.QueryRow(ctx, `SELECT count(*) FROM steering_records`+clause, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count records: %w", err)
	}

	q := baseSelect + clause + " ORDER BY created_at DESC, id DESC"
	if f.Limit > 0 {
		args = append(args, f.Limit)
		q += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if f.Offset > 0 {
		args = append(args, f.Offset)
		q += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query records: %w", err)
	}
	defer rows.Close()

	var out []Record
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

func (p *Postgres) Ping(ctx context.Context) error {
	if err := p.pool.Ping(ctx); err != nil {
		return fmt.Errorf("database ping: %w", err)
	}
	return nil
}

func (p *Postgres) Close() error {
	p.pool.Close()
	return nil
}

const baseSelect = `
SELECT id, batch_id, item_index, kind, source, status, wheelbase_m, track_m,
       inner_deg, angle_unit, direction, radius_m, request::text, result::text,
       error, created_at
FROM steering_records`

// row is the minimal interface shared by pgx.Row and pgx.Rows.
type row interface {
	Scan(dest ...any) error
}

func scan(r row) (Record, error) {
	var rec Record
	var req, res string
	var batchID, angleUnit, direction, errMsg string
	var radius *float64
	err := r.Scan(&rec.ID, &batchID, &rec.Index, &rec.Kind, &rec.Source, &rec.Status,
		&rec.Wheelbase, &rec.Track, &rec.InnerDeg, &angleUnit, &direction, &radius,
		&req, &res, &errMsg, &rec.CreatedAt)
	if err != nil {
		return Record{}, err
	}
	rec.BatchID = batchID
	rec.AngleUnit = angleUnit
	rec.Direction = direction
	rec.Radius = radius
	rec.Request = []byte(req)
	rec.Result = []byte(res)
	rec.Error = errMsg
	return rec, nil
}
