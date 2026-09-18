package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Schema is applied on startup (CREATE TABLE IF NOT EXISTS).
const Schema = `
CREATE TABLE IF NOT EXISTS steering_records (
    id            BIGSERIAL PRIMARY KEY,
    batch_id      TEXT,
    batch_index   INTEGER,
    kind          TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    angle_unit    TEXT NOT NULL DEFAULT 'deg',
    wheelbase     DOUBLE PRECISION,
    track         DOUBLE PRECISION,
    inside_angle  DOUBLE PRECISION,
    success       BOOLEAN NOT NULL,
    error_code    TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    result        JSONB,
    direction     TEXT GENERATED ALWAYS AS
                     (COALESCE(result->>'direction', '')) STORED
);
CREATE INDEX IF NOT EXISTS idx_steering_created ON steering_records (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_steering_batch   ON steering_records (batch_id);
CREATE INDEX IF NOT EXISTS idx_steering_success ON steering_records (success);
CREATE INDEX IF NOT EXISTS idx_steering_kind    ON steering_records (kind);
`

// PostgresStore is the durable Store backed by PostgreSQL.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgres dials the database, applies the schema and returns the store.
func NewPostgres(ctx context.Context, dsn string) (*PostgresStore, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	cfg.MaxConns = 10
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	if _, err := pool.Exec(ctx, Schema); err != nil {
		pool.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &PostgresStore{pool: pool}, nil
}

func (p *PostgresStore) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

func (p *PostgresStore) Close(_ context.Context) error {
	p.pool.Close()
	return nil
}

func (p *PostgresStore) Insert(ctx context.Context, r *Record) (*Record, error) {
	var resultJSON []byte
	if r.Result != nil {
		b, err := json.Marshal(r.Result)
		if err != nil {
			return nil, fmt.Errorf("marshal result: %w", err)
		}
		resultJSON = b
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}

	row := p.pool.QueryRow(ctx, `
		INSERT INTO steering_records
		    (batch_id, batch_index, kind, created_at, angle_unit,
		     wheelbase, track, inside_angle, success, error_code, error_message, result)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING id`,
		r.BatchID, r.BatchIndex, r.Kind, r.CreatedAt, r.AngleUnit,
		r.Wheelbase, r.Track, r.InsideAngle, r.Success, r.ErrorCode, r.ErrorMessage,
		nullableJSON(resultJSON),
	)
	if err := row.Scan(&r.ID); err != nil {
		return nil, fmt.Errorf("insert record: %w", err)
	}
	return r, nil
}

func (p *PostgresStore) Count(ctx context.Context) (int64, error) {
	var n int64
	err := p.pool.QueryRow(ctx, `SELECT count(*) FROM steering_records`).Scan(&n)
	return n, err
}

func (p *PostgresStore) Query(ctx context.Context, f Filter) ([]Record, error) {
	limit, offset := NormalizeLimit(f.Limit, f.Offset)

	q := `SELECT id, batch_id, batch_index, kind, created_at, angle_unit,
	             wheelbase, track, inside_angle, success, error_code, error_message, result
	        FROM steering_records
	       WHERE TRUE`
	args := []interface{}{}
	idx := 0
	add := func(cond string, val interface{}) {
		idx++
		q += fmt.Sprintf(" AND %s $%d", cond, idx)
		args = append(args, val)
	}
	if f.Success != nil {
		add("success =", *f.Success)
	}
	if f.Kind != "" {
		add("kind =", f.Kind)
	}
	if f.BatchID != "" {
		add("batch_id =", f.BatchID)
	}
	if f.Direction != "" {
		add("direction =", f.Direction)
	}
	if f.Since != nil {
		add("created_at >=", *f.Since)
	}
	if f.Until != nil {
		add("created_at <=", *f.Until)
	}
	q += " ORDER BY id DESC"
	q += fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)

	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Record
	for rows.Next() {
		r, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanRecord(s rowScanner) (Record, error) {
	var (
		r                                Record
		batchID, errorCode, errorMessage *string
		batchIndex                       *int
		unit                             string
		wheelbase, track, inside         *float64
		resultRaw                        []byte
	)
	if err := s.Scan(&r.ID, &batchID, &batchIndex, &r.Kind, &r.CreatedAt, &unit,
		&wheelbase, &track, &inside, &r.Success, &errorCode, &errorMessage, &resultRaw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return r, err
		}
		return r, err
	}
	r.BatchID, r.BatchIndex = batchID, batchIndex
	r.AngleUnit = unit
	r.Wheelbase, r.Track, r.InsideAngle = wheelbase, track, inside
	if errorCode != nil {
		r.ErrorCode = *errorCode
	}
	if errorMessage != nil {
		r.ErrorMessage = *errorMessage
	}
	if len(resultRaw) > 0 {
		var v interface{}
		if err := json.Unmarshal(resultRaw, &v); err != nil {
			return r, fmt.Errorf("unmarshal result json: %w", err)
		}
		r.Result = v
	}
	return r, nil
}

func nullableJSON(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	return b
}
