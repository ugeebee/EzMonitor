// Package query is the read path behind the dashboards: time-bucketed
// series, fleet status, hotspots, data gaps/reboots and alert reporting.
//
// Every query is tenant-scoped through user_group_roles; the caller's user
// ID comes from the session, never from the request. The SQL lives in
// sql/*.sql (embedded) so it can be read, EXPLAINed and graded on its own.
//
// The store takes a *sql.DB so it works with any PostgreSQL driver. With the
// backend's pgx pool:
//
//	db := stdlib.OpenDBFromPool(apiPool) // github.com/jackc/pgx/v5/stdlib
//	store := query.NewStore(db)
package query

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

var (
	//go:embed sql/series.sql
	sqlSeries string
	//go:embed sql/fleet_status.sql
	sqlFleetStatus string
	//go:embed sql/hotspots.sql
	sqlHotspots string
	//go:embed sql/instance_events.sql
	sqlInstanceEvents string
	//go:embed sql/alert_summary.sql
	sqlAlertSummary string
	//go:embed sql/alert_history.sql
	sqlAlertHistory string
)

// Role IDs are fixed by migration 000001.
const (
	RoleAdmin  int16 = 1
	RoleEditor int16 = 2
	RoleViewer int16 = 3
)

// Metrics are the metric_kind enum values.
var Metrics = []string{"cpu_util", "gpu_util", "ram_util", "uptime", "net_up_kbps", "net_down_kbps"}

// analyticWorkMem lets the two raw-scan analytics (percentiles, LAG over a
// week) sort in memory; measured in db/QUERIES.md. Set per transaction
// only, never role-wide.
const analyticWorkMem = "32MB"

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// ---------------------------------------------------------------------
// Access checks
// ---------------------------------------------------------------------

// GroupRole returns the caller's role in a group; ok is false when the
// user is not a member (the API answers 404, not 403, so group IDs of other
// tenants cannot be probed).
func (s *Store) GroupRole(ctx context.Context, userID, groupID int64) (role int16, ok bool, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT role_id FROM user_group_roles WHERE u_id = $1 AND g_id = $2`,
		userID, groupID).Scan(&role)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	return role, err == nil, err
}

// InstanceRole returns the caller's role in the instance's group.
func (s *Store) InstanceRole(ctx context.Context, userID, instanceID int64) (role int16, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT ugr.role_id
		FROM instances i
		JOIN user_group_roles ugr ON ugr.g_id = i.g_id AND ugr.u_id = $1
		WHERE i.i_id = $2`, userID, instanceID).Scan(&role)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	return role, err == nil, err
}

// ---------------------------------------------------------------------
// Series
// ---------------------------------------------------------------------

type LabelFilter struct{ Key, Value string }

type SeriesRequest struct {
	UserID     int64
	GroupID    *int64 // one of GroupID / InstanceID must be set
	InstanceID *int64
	Labels     []LabelFilter // instance must carry every pair
	Window     Window
}

type Point struct {
	T          time.Time `json:"t"`
	Samples    int64     `json:"samples"`
	CPUAvg     *float64  `json:"cpu_avg"`
	CPUMax     *float64  `json:"cpu_max"`
	RAMAvg     *float64  `json:"ram_avg"`
	RAMMax     *float64  `json:"ram_max"`
	GPUAvg     *float64  `json:"gpu_avg"`
	GPUMax     *float64  `json:"gpu_max"`
	NetUpAvg   *float64  `json:"net_up_avg"`
	NetUpMax   *float64  `json:"net_up_max"`
	NetDownAvg *float64  `json:"net_down_avg"`
	NetDownMax *float64  `json:"net_down_max"`
	Uptime     *int64    `json:"uptime"`
	// Window-function columns.
	CPUAvgSmooth *float64 `json:"cpu_avg_smooth"` // 3-bucket moving average
	FleetCPUAvg  *float64 `json:"fleet_cpu_avg"`  // mean over the instances in this response
	CPURank      *int64   `json:"cpu_rank"`       // 1 = busiest instance in this bucket
}

type InstanceSeries struct {
	InstanceID int64   `json:"i_id"`
	Label      string  `json:"label"`
	Points     []Point `json:"points"`
}

func (s *Store) Series(ctx context.Context, req SeriesRequest) ([]InstanceSeries, error) {
	if req.GroupID == nil && req.InstanceID == nil {
		return nil, fmt.Errorf("query: series needs a group or an instance")
	}
	keys := make([]string, len(req.Labels))
	vals := make([]string, len(req.Labels))
	for i, l := range req.Labels {
		keys[i], vals[i] = l.Key, l.Value
	}
	rows, err := s.db.QueryContext(ctx, sqlSeries,
		req.UserID, nullable(req.GroupID), nullable(req.InstanceID),
		pgTextArray(keys), pgTextArray(vals),
		req.Window.From, req.Window.To, pgInterval(req.Window.Step))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []InstanceSeries
	for rows.Next() {
		var (
			iID                     int64
			label                   string
			p                       Point
			cpuA, cpuM, ramA, ramM  sql.NullFloat64
			gpuA, gpuM, upA, upM    sql.NullFloat64
			dnA, dnM, smooth, fleet sql.NullFloat64
			uptime, rank            sql.NullInt64
		)
		if err := rows.Scan(&iID, &label, &p.T, &p.Samples,
			&cpuA, &cpuM, &ramA, &ramM, &gpuA, &gpuM,
			&upA, &upM, &dnA, &dnM, &uptime,
			&smooth, &fleet, &rank); err != nil {
			return nil, err
		}
		p.T = p.T.UTC()
		p.CPUAvg, p.CPUMax, p.RAMAvg, p.RAMMax = f(cpuA), f(cpuM), f(ramA), f(ramM)
		p.GPUAvg, p.GPUMax, p.NetUpAvg, p.NetUpMax = f(gpuA), f(gpuM), f(upA), f(upM)
		p.NetDownAvg, p.NetDownMax, p.Uptime = f(dnA), f(dnM), i(uptime)
		p.CPUAvgSmooth, p.FleetCPUAvg, p.CPURank = f(smooth), f(fleet), i(rank)

		// Rows arrive ordered by (i_id, bucket).
		if n := len(out); n == 0 || out[n-1].InstanceID != iID {
			out = append(out, InstanceSeries{InstanceID: iID, Label: label,
				Points: make([]Point, 0, req.Window.Buckets())})
		}
		last := &out[len(out)-1]
		last.Points = append(last.Points, p)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------
// Fleet status
// ---------------------------------------------------------------------

type Latest struct {
	RecordedAt  time.Time `json:"recorded_at"`
	CPUUtil     float64   `json:"cpu_util"`
	GPUUtil     *float64  `json:"gpu_util"`
	RAMUtil     float64   `json:"ram_util"`
	Uptime      int64     `json:"uptime"`
	NetUpKbps   float64   `json:"net_up_kbps"`
	NetDownKbps float64   `json:"net_down_kbps"`
}

type FleetRow struct {
	InstanceID   int64             `json:"i_id"`
	GroupID      int64             `json:"g_id"`
	GroupLabel   string            `json:"group_label"`
	Label        string            `json:"label"`
	Role         int16             `json:"role_id"`
	Enabled      bool              `json:"enabled"`
	Status       string            `json:"status"`
	LastSeenAt   *time.Time        `json:"last_seen_at"`
	Labels       map[string]string `json:"labels"`
	Latest       *Latest           `json:"latest"` // nil: no sample within the lookback
	ActiveAlerts int64             `json:"active_alerts"`
}

func (s *Store) FleetStatus(ctx context.Context, userID int64, groupID *int64, lookback time.Duration) ([]FleetRow, error) {
	rows, err := s.db.QueryContext(ctx, sqlFleetStatus, userID, nullable(groupID), pgInterval(lookback))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FleetRow{}
	for rows.Next() {
		var (
			r                       FleetRow
			lastSeen, recAt         sql.NullTime
			labels                  []byte
			cpu, gpu, ram, up, down sql.NullFloat64
			uptime                  sql.NullInt64
		)
		if err := rows.Scan(&r.InstanceID, &r.GroupID, &r.GroupLabel, &r.Label, &r.Role,
			&r.Enabled, &r.Status, &lastSeen, &labels,
			&recAt, &cpu, &gpu, &ram, &uptime, &up, &down,
			&r.ActiveAlerts); err != nil {
			return nil, err
		}
		if lastSeen.Valid {
			t := lastSeen.Time.UTC()
			r.LastSeenAt = &t
		}
		r.Labels = map[string]string{}
		if err := json.Unmarshal(labels, &r.Labels); err != nil {
			return nil, err
		}
		if recAt.Valid {
			r.Latest = &Latest{RecordedAt: recAt.Time.UTC(), CPUUtil: cpu.Float64, GPUUtil: f(gpu),
				RAMUtil: ram.Float64, Uptime: uptime.Int64, NetUpKbps: up.Float64, NetDownKbps: down.Float64}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------
// Hotspots
// ---------------------------------------------------------------------

type Hotspot struct {
	InstanceID      int64   `json:"i_id"`
	Label           string  `json:"label"`
	Samples         int64   `json:"samples"`
	Avg             float64 `json:"avg"`
	Max             float64 `json:"max"`
	P95             float64 `json:"p95"`
	Rank            int64   `json:"rank"`
	P95VsFleetAvg   float64 `json:"p95_vs_fleet_avg"`
	FleetPercentile float64 `json:"fleet_percentile"`
}

func (s *Store) Hotspots(ctx context.Context, userID int64, groupID *int64, from, to time.Time, metric string, limit int) ([]Hotspot, error) {
	out := []Hotspot{}
	err := s.analytic(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, sqlHotspots, userID, nullable(groupID), from, to, metric, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var h Hotspot
			if err := rows.Scan(&h.InstanceID, &h.Label, &h.Samples, &h.Avg, &h.Max, &h.P95,
				&h.Rank, &h.P95VsFleetAvg, &h.FleetPercentile); err != nil {
				return err
			}
			out = append(out, h)
		}
		return rows.Err()
	})
	return out, err
}

// ---------------------------------------------------------------------
// Instance events (gaps, reboots)
// ---------------------------------------------------------------------

type Event struct {
	Kind      string    `json:"kind"` // "gap" | "reboot"
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
	DurationS float64   `json:"duration_s"`
}

func (s *Store) InstanceEvents(ctx context.Context, userID, instanceID int64, from, to time.Time, maxGap time.Duration) ([]Event, error) {
	out := []Event{}
	err := s.analytic(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, sqlInstanceEvents, userID, instanceID, from, to, pgInterval(maxGap))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e Event
			if err := rows.Scan(&e.Kind, &e.StartedAt, &e.EndedAt, &e.DurationS); err != nil {
				return err
			}
			e.StartedAt, e.EndedAt = e.StartedAt.UTC(), e.EndedAt.UTC()
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}

// Availability is the share of [from, min(to, now)) not covered by gaps.
func Availability(events []Event, from, to, now time.Time) float64 {
	if to.After(now) {
		to = now
	}
	span := to.Sub(from).Seconds()
	if span <= 0 {
		return 1
	}
	var gap float64
	for _, e := range events {
		if e.Kind == "gap" {
			gap += e.DurationS
		}
	}
	if gap > span {
		gap = span
	}
	return 1 - gap/span
}

// ---------------------------------------------------------------------
// Alerts
// ---------------------------------------------------------------------

type RuleSummary struct {
	RuleID            int64      `json:"rule_id"`
	Metric            string     `json:"metric_name"`
	Operator          string     `json:"operator"`
	Threshold         float64    `json:"threshold_value"`
	Enabled           bool       `json:"enabled"`
	Fired             int64      `json:"fired"`
	InstancesAffected int64      `json:"instances_affected"`
	MTTRSeconds       *float64   `json:"mttr_s"`
	WorstValue        *float64   `json:"worst_value"`
	LastFired         *time.Time `json:"last_fired"`
	ActiveNow         int64      `json:"active_now"`
}

func (s *Store) AlertSummary(ctx context.Context, userID, groupID int64, from, to time.Time) ([]RuleSummary, error) {
	rows, err := s.db.QueryContext(ctx, sqlAlertSummary, userID, groupID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RuleSummary{}
	for rows.Next() {
		var (
			r           RuleSummary
			mttr, worst sql.NullFloat64
			last        sql.NullTime
		)
		if err := rows.Scan(&r.RuleID, &r.Metric, &r.Operator, &r.Threshold, &r.Enabled,
			&r.Fired, &r.InstancesAffected, &mttr, &worst, &last, &r.ActiveNow); err != nil {
			return nil, err
		}
		r.MTTRSeconds, r.WorstValue = f(mttr), f(worst)
		if last.Valid {
			t := last.Time.UTC()
			r.LastFired = &t
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type AlertRow struct {
	AlertID       int64      `json:"a_id"`
	RuleID        int64      `json:"rule_id"`
	Metric        string     `json:"metric_name"`
	Operator      string     `json:"operator"`
	Threshold     float64    `json:"threshold_value"`
	InstanceID    int64      `json:"i_id"`
	InstanceLabel string     `json:"instance_label"`
	Value         float64    `json:"alert_value"`
	TriggeredAt   time.Time  `json:"triggered_at"`
	ResolvedAt    *time.Time `json:"resolved_at"`
	Status        string     `json:"status"`
}

// Cursor marks the last row of a page: (triggered_at, a_id).
type Cursor struct {
	TriggeredAt time.Time
	AlertID     int64
}

// AlertHistory returns one page, newest first, and the cursor for the next
// page (nil when this was the last one).
func (s *Store) AlertHistory(ctx context.Context, userID, groupID int64, after *Cursor, status *string, limit int) ([]AlertRow, *Cursor, error) {
	var cAt, cID any
	if after != nil {
		cAt, cID = after.TriggeredAt, after.AlertID
	}
	var st any
	if status != nil {
		st = *status
	}
	// Fetch one extra row to know whether another page exists.
	rows, err := s.db.QueryContext(ctx, sqlAlertHistory, userID, groupID, cAt, cID, st, limit+1)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	out := []AlertRow{}
	for rows.Next() {
		var (
			a        AlertRow
			resolved sql.NullTime
		)
		if err := rows.Scan(&a.AlertID, &a.RuleID, &a.Metric, &a.Operator, &a.Threshold,
			&a.InstanceID, &a.InstanceLabel, &a.Value, &a.TriggeredAt, &resolved, &a.Status); err != nil {
			return nil, nil, err
		}
		a.TriggeredAt = a.TriggeredAt.UTC()
		if resolved.Valid {
			t := resolved.Time.UTC()
			a.ResolvedAt = &t
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if len(out) <= limit {
		return out, nil, nil
	}
	out = out[:limit]
	last := out[limit-1]
	return out, &Cursor{TriggeredAt: last.TriggeredAt, AlertID: last.AlertID}, nil
}

// ---------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------

// analytic runs fn in a read-only transaction with a larger sort budget.
func (s *Store) analytic(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // read-only; rollback after commit is a no-op
	if _, err := tx.ExecContext(ctx, "SET LOCAL work_mem = '"+analyticWorkMem+"'"); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func nullable(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func f(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}

func i(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

// pgTextArray renders a PostgreSQL text[] literal, quoting every element.
// Passing arrays as text keeps the store driver-agnostic.
func pgTextArray(ss []string) string {
	var b strings.Builder
	b.WriteByte('{')
	for k, s := range ss {
		if k > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('"')
		for _, r := range s {
			if r == '"' || r == '\\' {
				b.WriteByte('\\')
			}
			b.WriteRune(r)
		}
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String()
}
