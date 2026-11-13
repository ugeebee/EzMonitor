package query

// Integration tests. They need a database with migrations 000001-000004
// applied and are skipped otherwise:
//
//	EZM_TEST_DATABASE_URL      owner role: builds fixtures, refreshes the rollup
//	EZM_TEST_API_DATABASE_URL  optional ezm_api role: runs the store, so the
//	                           000002/000004 grants are exercised too
//
// The test driver is lib/pq so this package needs no pgx in tests; the store
// itself only uses database/sql. Fixtures live in their own groups/users
// with random names and are deleted (ON DELETE CASCADE) afterwards.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

type fixture struct {
	owner, api *sql.DB
	store      *Store

	admin, viewer, outsider int64 // users
	g1, g2                  int64 // groups
	a, b, c, d              int64 // instances: a,b,c in g1; d in g2
	base                    time.Time
	rules                   []int64
}

func setup(t *testing.T) *fixture {
	t.Helper()
	url := os.Getenv("EZM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("EZM_TEST_DATABASE_URL not set")
	}
	owner, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatal(err)
	}
	api := owner
	if u := os.Getenv("EZM_TEST_API_DATABASE_URL"); u != "" {
		if api, err = sql.Open("postgres", u); err != nil {
			t.Fatal(err)
		}
	}
	fx := &fixture{owner: owner, api: api, store: NewStore(api)}
	ctx := context.Background()
	tag := randHex(4)

	one := func(q string, args ...any) int64 {
		t.Helper()
		var id int64
		if err := owner.QueryRowContext(ctx, q, args...).Scan(&id); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
		return id
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := owner.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	user := func(name string) int64 {
		return one(`INSERT INTO users (name, email, hash_pass) VALUES ($1, $2, 'x') RETURNING u_id`,
			name, fmt.Sprintf("%s-%s@test.ezm", name, tag))
	}
	group := func(name string) int64 {
		return one(`INSERT INTO groups (label) VALUES ($1) RETURNING g_id`, name+"-"+tag)
	}
	instance := func(g int64, label string, labels map[string]string) int64 {
		id := one(`INSERT INTO instances (g_id, label, fingerprint, token_hash)
		           VALUES ($1, $2, $3, $4) RETURNING i_id`, g, label, randHex(8), randHex(32))
		for k, v := range labels {
			exec(`INSERT INTO instance_labels VALUES ($1, $2, $3)`, id, k, v)
		}
		return id
	}

	fx.admin, fx.viewer, fx.outsider = user("admin"), user("viewer"), user("outsider")
	fx.g1, fx.g2 = group("lab"), group("other")
	exec(`INSERT INTO user_group_roles VALUES ($1,$3,1), ($2,$3,3), ($4,$5,1)`,
		fx.admin, fx.viewer, fx.g1, fx.outsider, fx.g2)
	fx.a = instance(fx.g1, "a", map[string]string{"env": "prod", "rack": "r1"})
	fx.b = instance(fx.g1, "b", map[string]string{"env": "prod", "rack": "r2"})
	fx.c = instance(fx.g1, "c", map[string]string{"env": "dev"})
	fx.d = instance(fx.g2, "d", nil)

	t.Cleanup(func() {
		owner.Exec(`DELETE FROM groups WHERE g_id IN ($1, $2)`, fx.g1, fx.g2)
		owner.Exec(`DELETE FROM users WHERE u_id IN ($1, $2, $3)`, fx.admin, fx.viewer, fx.outsider)
		if api != owner {
			api.Close()
		}
		owner.Close()
	})

	// One hour of 10 s samples starting 90 minutes ago (inside the
	// partitions that run_maintenance keeps: yesterday .. +7 days).
	fx.base = alignDown(time.Now().Add(-90*time.Minute), RollupStep)
	gen := `
		INSERT INTO metrics_data
		SELECT $1, $2::timestamptz + k * interval '10 seconds', %s
		FROM generate_series(0, 359) AS k %s`
	// a: cpu 10..59 sawtooth, no GPU, monotonic uptime
	exec(fmt.Sprintf(gen, `10 + k % 50, NULL, 40, 1000 + 10 * k, 5, 50`, ``), fx.a, fx.base)
	// b: cpu 80, GPU 50; silent for 3 min from +20m; reboots at +40m (uptime restarts at 5 s)
	exec(fmt.Sprintf(gen, `80, 50, 60,
		CASE WHEN k < 240 THEN 100000 + 10 * k ELSE 5 + 10 * (k - 240) END, 1, 2`,
		`WHERE k NOT BETWEEN 120 AND 137`), fx.b, fx.base)
	exec(fmt.Sprintf(gen, `20, NULL, 30, 50 + 10 * k, 0, 0`, ``), fx.c, fx.base)
	exec(fmt.Sprintf(gen, `99, NULL, 99, 10 * k, 0, 0`, ``), fx.d, fx.base)

	// Roll everything up, then add a raw tail after the watermark for "a"
	// so the hybrid (rollup + raw) path is exercised.
	// The fixture rows are older than the current watermark, so the
	// invalidation trigger has marked the rollup dirty; refresh rebuilds it.
	exec(`SELECT refresh_metrics_rollup()`)
	exec(`INSERT INTO metrics_data
	      SELECT $1, date_trunc('second', now()) - k * interval '5 seconds', 33, NULL, 40, 999999, 0, 0
	      FROM generate_series(1, 12) AS k`, fx.a)
	return fx
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func ptr[T any](v T) *T { return &v }

// oracle aggregates metrics_data directly, without the rollup.
type oracleRow struct {
	n             int64
	cpuAvg, cpuMx float64
	gpuAvg        sql.NullFloat64
	uptime        int64
}

func (fx *fixture) oracle(t *testing.T, ids []int64, w Window) map[string]oracleRow {
	t.Helper()
	arr := "{"
	for k, id := range ids {
		if k > 0 {
			arr += ","
		}
		arr += strconv.FormatInt(id, 10)
	}
	arr += "}"
	rows, err := fx.owner.Query(`
		SELECT i_id, date_bin($2::interval, recorded_at, 'epoch'), count(*),
		       avg(cpu_util), max(cpu_util), avg(gpu_util), max(uptime)
		FROM metrics_data
		WHERE i_id = ANY($1::bigint[]) AND recorded_at >= $3 AND recorded_at < $4
		GROUP BY 1, 2`, arr, pgInterval(w.Step), w.From, w.To)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]oracleRow{}
	for rows.Next() {
		var (
			id int64
			b  time.Time
			o  oracleRow
		)
		if err := rows.Scan(&id, &b, &o.n, &o.cpuAvg, &o.cpuMx, &o.gpuAvg, &o.uptime); err != nil {
			t.Fatal(err)
		}
		out[fmt.Sprint(id, b.UTC().Unix())] = o
	}
	return out
}

// checkSeries compares Store.Series for group g1 with the raw oracle.
func (fx *fixture) checkSeries(t *testing.T, step time.Duration) {
	t.Helper()
	w, err := NewWindow(fx.base.Add(-10*time.Minute), time.Now().Add(time.Minute), step)
	if err != nil {
		t.Fatal(err)
	}
	got, err := fx.store.Series(context.Background(), SeriesRequest{UserID: fx.admin, GroupID: &fx.g1, Window: w})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 instances, got %d", len(got))
	}
	want := fx.oracle(t, []int64{fx.a, fx.b, fx.c}, w)
	nonEmpty := 0
	for _, s := range got {
		if len(s.Points) != w.Buckets() {
			t.Fatalf("instance %d: %d points, want %d (gap-filled)", s.InstanceID, len(s.Points), w.Buckets())
		}
		for _, p := range s.Points {
			o, ok := want[fmt.Sprint(s.InstanceID, p.T.Unix())]
			if !ok {
				if p.Samples != 0 || p.CPUAvg != nil {
					t.Fatalf("%d@%s: expected empty bucket, got %+v", s.InstanceID, p.T, p)
				}
				continue
			}
			nonEmpty++
			if p.Samples != o.n || math.Abs(*p.CPUAvg-o.cpuAvg) > 1e-6 || *p.CPUMax != o.cpuMx ||
				*p.Uptime != o.uptime || (p.GPUAvg == nil) == o.gpuAvg.Valid ||
				(p.GPUAvg != nil && math.Abs(*p.GPUAvg-o.gpuAvg.Float64) > 1e-6) {
				t.Fatalf("step %s, %d@%s: got n=%d cpu=%v/%v up=%v gpu=%v, want %+v",
					step, s.InstanceID, p.T, p.Samples, *p.CPUAvg, *p.CPUMax, *p.Uptime, p.GPUAvg, o)
			}
		}
	}
	if nonEmpty != len(want) {
		t.Fatalf("matched %d buckets, oracle has %d", nonEmpty, len(want))
	}
}

func (fx *fixture) dirtyFrom(t *testing.T) *time.Time {
	t.Helper()
	var d sql.NullTime
	if err := fx.owner.QueryRow(`SELECT dirty_from FROM metrics_rollup_state`).Scan(&d); err != nil {
		t.Fatal(err)
	}
	if !d.Valid {
		return nil
	}
	v := d.Time.UTC()
	return &v
}

func TestSeriesMatchesRawAggregate(t *testing.T) {
	fx := setup(t)
	for _, step := range []time.Duration{5 * time.Minute, time.Hour, 10 * time.Second, 7 * time.Minute} {
		t.Run(step.String(), func(t *testing.T) { fx.checkSeries(t, step) })
	}
}

// Late writes below the watermark must be visible at once (raw fallback)
// and be absorbed by the next refresh.
func TestRollupInvalidation(t *testing.T) {
	fx := setup(t)
	if d := fx.dirtyFrom(t); d != nil {
		t.Fatalf("fresh rollup is dirty from %s", d)
	}
	steps := []time.Duration{5 * time.Minute, time.Hour}

	// BR4 re-delivery: overwrite a's samples at +10m..+12m (ingest-style upsert).
	if _, err := fx.owner.Exec(`
		INSERT INTO metrics_data
		SELECT $1, $2::timestamptz + k * interval '10 seconds', 100, NULL, 40, 7, 0, 0
		FROM generate_series(60, 72) k
		ON CONFLICT (i_id, recorded_at) DO UPDATE SET cpu_util = EXCLUDED.cpu_util, uptime = EXCLUDED.uptime`,
		fx.a, fx.base); err != nil {
		t.Fatal(err)
	}
	if d := fx.dirtyFrom(t); d == nil || !d.Equal(fx.base.Add(10*time.Minute)) {
		t.Fatalf("dirty_from after upsert = %v, want %s", d, fx.base.Add(10*time.Minute))
	}
	// A late batch that is older still lowers dirty_from.
	if _, err := fx.owner.Exec(`INSERT INTO metrics_data VALUES ($1, $2::timestamptz + interval '125 seconds', 99, NULL, 30, 1, 0, 0)`,
		fx.c, fx.base); err != nil {
		t.Fatal(err)
	}
	// dirty_from is kept on bucket boundaries: +125s marks the bucket from +0s.
	if d := fx.dirtyFrom(t); !d.Equal(fx.base) {
		t.Fatalf("dirty_from after late insert = %v, want %s", d, fx.base)
	}
	for _, s := range steps {
		fx.checkSeries(t, s) // before refresh: served raw from dirty_from
	}
	if _, err := fx.owner.Exec(`SELECT refresh_metrics_rollup()`); err != nil {
		t.Fatal(err)
	}
	if d := fx.dirtyFrom(t); d != nil {
		t.Fatalf("refresh left dirty_from = %s", d)
	}
	for _, s := range steps {
		fx.checkSeries(t, s) // after refresh: served from the rebuilt rollup
	}

	// Deletes invalidate too.
	if _, err := fx.owner.Exec(`DELETE FROM metrics_data WHERE i_id = $1 AND recorded_at >= $2 AND recorded_at < $3`,
		fx.b, fx.base.Add(30*time.Minute), fx.base.Add(35*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if d := fx.dirtyFrom(t); d == nil {
		t.Fatal("delete did not mark the rollup dirty")
	}
	fx.checkSeries(t, 5*time.Minute)
	fx.owner.Exec(`SELECT refresh_metrics_rollup()`)
	fx.checkSeries(t, 5*time.Minute)

	// Fresh samples above the watermark do not touch the state row.
	if _, err := fx.owner.Exec(`INSERT INTO metrics_data VALUES ($1, date_trunc('second', now()), 1, NULL, 1, 1, 0, 0)
		ON CONFLICT DO NOTHING`, fx.c); err != nil {
		t.Fatal(err)
	}
	if d := fx.dirtyFrom(t); d != nil {
		t.Fatalf("fresh sample marked rollup dirty from %s", d)
	}
}

func TestSeriesGapFillAndWindowFunctions(t *testing.T) {
	fx := setup(t)
	w, _ := NewWindow(fx.base.Add(19*time.Minute), fx.base.Add(25*time.Minute), time.Minute)
	got, err := fx.store.Series(context.Background(), SeriesRequest{UserID: fx.admin, GroupID: &fx.g1, Window: w})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int64]InstanceSeries{}
	for _, s := range got {
		byID[s.InstanceID] = s
	}
	b := byID[fx.b].Points
	// b is silent for k = 120..137, i.e. +20m00s..+22m50s; it resumes at +23m00s.
	for k := 1; k <= 3; k++ {
		if b[k].Samples != 0 || b[k].CPUAvg != nil || b[k].CPURank != nil {
			t.Fatalf("bucket +%dm should be an empty gap: %+v", 19+k, b[k])
		}
	}
	if b[0].Samples != 6 || b[4].Samples != 6 {
		t.Fatalf("edges of the gap: %d, %d samples", b[0].Samples, b[4].Samples)
	}
	// Bucket +19m: a has k = 114..119 -> cpu 24..29 (avg 26.5), b = 80, c = 20.
	p := b[0]
	if *p.CPURank != 1 || *byID[fx.c].Points[0].CPURank != 3 {
		t.Fatalf("rank: b=%v c=%v", *p.CPURank, *byID[fx.c].Points[0].CPURank)
	}
	if math.Abs(*p.FleetCPUAvg-(26.5+80+20)/3) > 1e-9 {
		t.Fatalf("fleet avg %v", *p.FleetCPUAvg)
	}
	// The 3-bucket moving average skips NULLs: +22m sees only empty
	// buckets (NULL), +23m sees only itself.
	if b[3].CPUAvgSmooth != nil || math.Abs(*b[4].CPUAvgSmooth-80) > 1e-9 {
		t.Fatalf("smooth: %v %v", b[3].CPUAvgSmooth, *b[4].CPUAvgSmooth)
	}
}

func TestSeriesTenantIsolationAndLabels(t *testing.T) {
	fx := setup(t)
	ctx := context.Background()
	w, _ := NewWindow(fx.base, fx.base.Add(time.Hour), 5*time.Minute)
	count := func(req SeriesRequest) []int64 {
		t.Helper()
		req.Window = w
		got, err := fx.store.Series(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		var ids []int64
		for _, s := range got {
			ids = append(ids, s.InstanceID)
		}
		return ids
	}
	if ids := count(SeriesRequest{UserID: fx.outsider, InstanceID: &fx.a}); len(ids) != 0 {
		t.Fatalf("outsider sees %v", ids)
	}
	if ids := count(SeriesRequest{UserID: fx.outsider, GroupID: &fx.g1}); len(ids) != 0 {
		t.Fatalf("outsider sees group: %v", ids)
	}
	if ids := count(SeriesRequest{UserID: fx.viewer, InstanceID: &fx.a}); len(ids) != 1 {
		t.Fatalf("viewer: %v", ids)
	}
	// Relational division: instances carrying ALL requested labels.
	prod := []LabelFilter{{"env", "prod"}}
	if ids := count(SeriesRequest{UserID: fx.admin, GroupID: &fx.g1, Labels: prod}); len(ids) != 2 {
		t.Fatalf("env=prod: %v", ids)
	}
	both := append(prod, LabelFilter{"rack", "r2"})
	if ids := count(SeriesRequest{UserID: fx.admin, GroupID: &fx.g1, Labels: both}); len(ids) != 1 || ids[0] != fx.b {
		t.Fatalf("env=prod,rack=r2: %v", ids)
	}
	if ids := count(SeriesRequest{UserID: fx.admin, GroupID: &fx.g1, Labels: []LabelFilter{{"env", `x"}`}}}); len(ids) != 0 {
		t.Fatalf("quoted label value: %v", ids)
	}
}

func TestInstanceEvents(t *testing.T) {
	fx := setup(t)
	ev, err := fx.store.InstanceEvents(context.Background(), fx.admin, fx.b,
		fx.base, fx.base.Add(time.Hour), 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 2 {
		t.Fatalf("want gap + reboot, got %+v", ev)
	}
	gap, reboot := ev[0], ev[1]
	if gap.Kind != "gap" || !gap.StartedAt.Equal(fx.base.Add(1190*time.Second)) || gap.DurationS != 190 {
		t.Fatalf("gap: %+v", gap)
	}
	// Reboot: last sample before at +39m50s, new boot at +40m00s - 5s.
	if reboot.Kind != "reboot" || !reboot.EndedAt.Equal(fx.base.Add(40*time.Minute-5*time.Second)) {
		t.Fatalf("reboot: %+v", reboot)
	}
	// A window ending after the data shows trailing silence.
	ev, _ = fx.store.InstanceEvents(context.Background(), fx.admin, fx.c,
		fx.base, fx.base.Add(70*time.Minute), 30*time.Second)
	if len(ev) != 1 || ev[0].Kind != "gap" || ev[0].DurationS != 610 {
		t.Fatalf("trailing gap: %+v", ev)
	}
	// Other tenants get nothing, not even the trailing-gap row.
	ev, _ = fx.store.InstanceEvents(context.Background(), fx.outsider, fx.c,
		fx.base, fx.base.Add(70*time.Minute), 30*time.Second)
	if len(ev) != 0 {
		t.Fatalf("outsider: %+v", ev)
	}
}

func TestFleetStatusAndHotspots(t *testing.T) {
	fx := setup(t)
	ctx := context.Background()
	rows, err := fx.store.FleetStatus(ctx, fx.admin, nil, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("admin should see 3 instances, got %d", len(rows))
	}
	for _, r := range rows {
		switch r.InstanceID {
		case fx.a: // only "a" has samples in the last 5 minutes (the raw tail)
			if r.Latest == nil || r.Latest.Uptime != 999999 || r.Labels["rack"] != "r1" || r.Role != RoleAdmin {
				t.Fatalf("a: %+v %+v", r, r.Latest)
			}
		default:
			if r.Latest != nil {
				t.Fatalf("%s should be stale: %+v", r.Label, r.Latest)
			}
		}
	}
	hs, err := fx.store.Hotspots(ctx, fx.admin, &fx.g1, fx.base, fx.base.Add(time.Hour), "cpu_util", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(hs) != 2 || hs[0].InstanceID != fx.b || hs[0].P95 != 80 || hs[0].Rank != 1 || hs[1].InstanceID != fx.a {
		t.Fatalf("hotspots: %+v", hs)
	}
	// Fleet comparison uses all three, not just the top 2.
	if math.Abs(hs[0].P95VsFleetAvg-(80-(80+hs[1].P95+20)/3)) > 1e-9 {
		t.Fatalf("p95 vs fleet: %+v", hs[0])
	}
	gpu, _ := fx.store.Hotspots(ctx, fx.admin, &fx.g1, fx.base, fx.base.Add(time.Hour), "gpu_util", 10)
	if len(gpu) != 1 || gpu[0].InstanceID != fx.b {
		t.Fatalf("only b has a GPU: %+v", gpu)
	}
}

func TestAlertsPaginationAndSummary(t *testing.T) {
	fx := setup(t)
	ctx := context.Background()
	for _, m := range []string{"cpu_util", "ram_util"} {
		var id int64
		if err := fx.owner.QueryRow(`INSERT INTO alert_rules (g_id, metric_name, operator, threshold_value)
			VALUES ($1, $2, '>', 70) RETURNING rule_id`, fx.g1, m).Scan(&id); err != nil {
			t.Fatal(err)
		}
		fx.rules = append(fx.rules, id)
	}
	// 30 resolved cpu alerts, pairs share a timestamp (ties exercise the a_id tiebreak),
	// 5 resolved ram alerts, 1 active cpu alert.
	_, err := fx.owner.Exec(`
		INSERT INTO alert_logs (rule_id, i_id, g_id, alert_value, triggered_at, resolved_at, status)
		SELECT $1::bigint, CASE WHEN k % 2 = 0 THEN $3::bigint ELSE $4::bigint END, $5::bigint, 80 + k,
		       $6::timestamptz + (k / 2) * interval '1 minute',
		       $6::timestamptz + (k / 2) * interval '1 minute' + interval '90 seconds', 'resolved'::alert_status
		FROM generate_series(0, 29) k
		UNION ALL
		SELECT $2::bigint, $3::bigint, $5::bigint, 90, $6::timestamptz + k * interval '1 minute', $6::timestamptz + k * interval '1 minute' + interval '30 seconds', 'resolved'
		FROM generate_series(0, 4) k`,
		fx.rules[0], fx.rules[1], fx.a, fx.b, fx.g1, fx.base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fx.owner.Exec(`INSERT INTO alert_logs (rule_id, i_id, g_id, alert_value, triggered_at)
		VALUES ($1, $2, $3, 95, now())`, fx.rules[0], fx.b, fx.g1); err != nil {
		t.Fatal(err)
	}

	var all []AlertRow
	var cur *Cursor
	pages := 0
	for {
		page, next, err := fx.store.AlertHistory(ctx, fx.viewer, fx.g1, cur, nil, 7)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, page...)
		pages++
		if next == nil {
			break
		}
		// Cursors survive the trip through the API encoding.
		if cur, err = DecodeCursor(next.Encode()); err != nil {
			t.Fatal(err)
		}
	}
	if len(all) != 36 || pages != 6 {
		t.Fatalf("got %d alerts in %d pages", len(all), pages)
	}
	seen := map[int64]bool{}
	for k, a := range all {
		if seen[a.AlertID] {
			t.Fatalf("duplicate alert %d", a.AlertID)
		}
		seen[a.AlertID] = true
		if k > 0 {
			p := all[k-1]
			if a.TriggeredAt.After(p.TriggeredAt) || (a.TriggeredAt.Equal(p.TriggeredAt) && a.AlertID > p.AlertID) {
				t.Fatalf("order broken at %d", k)
			}
		}
	}
	active, next, _ := fx.store.AlertHistory(ctx, fx.viewer, fx.g1, nil, ptr("active"), 10)
	if len(active) != 1 || next != nil || active[0].ResolvedAt != nil {
		t.Fatalf("active filter: %+v", active)
	}
	if rows, _, _ := fx.store.AlertHistory(ctx, fx.outsider, fx.g1, nil, nil, 10); len(rows) != 0 {
		t.Fatal("outsider read alert history")
	}

	sum, err := fx.store.AlertSummary(ctx, fx.admin, fx.g1, fx.base.Add(-time.Hour), time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(sum) != 2 {
		t.Fatalf("summary rows: %+v", sum)
	}
	cpu, ram := sum[0], sum[1]
	if cpu.RuleID != fx.rules[0] || cpu.Fired != 31 || cpu.InstancesAffected != 2 || cpu.ActiveNow != 1 ||
		*cpu.MTTRSeconds != 90 || *cpu.WorstValue != 109 {
		t.Fatalf("cpu summary: %+v mttr=%v", cpu, *cpu.MTTRSeconds)
	}
	if ram.Fired != 5 || ram.InstancesAffected != 1 || *ram.MTTRSeconds != 30 || ram.ActiveNow != 0 {
		t.Fatalf("ram summary: %+v", ram)
	}
}

func TestHandlers(t *testing.T) {
	fx := setup(t)
	mux := http.NewServeMux()
	(&Handler{Store: fx.store, User: func(r *http.Request) (int64, bool) {
		id, err := strconv.ParseInt(r.Header.Get("X-Test-User"), 10, 64)
		return id, err == nil
	}}).Register(mux)

	get := func(user int64, path string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest("GET", path, nil)
		if user != 0 {
			req.Header.Set("X-Test-User", strconv.FormatInt(user, 10))
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		var body map[string]any
		json.Unmarshal(rec.Body.Bytes(), &body)
		return rec.Code, body
	}
	from := fx.base.Format(time.RFC3339)
	to := fx.base.Add(time.Hour).Format(time.RFC3339)

	cases := []struct {
		user int64
		path string
		want int
	}{
		{0, fmt.Sprintf("/api/v1/instances/%d/series", fx.a), 401},
		{fx.outsider, fmt.Sprintf("/api/v1/instances/%d/series", fx.a), 404},
		{fx.outsider, fmt.Sprintf("/api/v1/groups/%d/alerts", fx.g1), 404},
		{fx.admin, "/api/v1/instances/999999999/series", 404},
		{fx.admin, "/api/v1/instances/abc/series", 404},
		{fx.admin, fmt.Sprintf("/api/v1/instances/%d/series?step=1s&range=24h", fx.a), 400},
		{fx.admin, fmt.Sprintf("/api/v1/instances/%d/series?from=%s&range=1h", fx.a, from), 400},
		{fx.admin, fmt.Sprintf("/api/v1/groups/%d/series?label=Bad", fx.g1), 400},
		{fx.admin, fmt.Sprintf("/api/v1/groups/%d/hotspots?metric=disk", fx.g1), 400},
		{fx.admin, fmt.Sprintf("/api/v1/groups/%d/hotspots?range=48h", fx.g1), 400},
		{fx.admin, fmt.Sprintf("/api/v1/instances/%d/events?range=8d", fx.a), 400},
		{fx.admin, fmt.Sprintf("/api/v1/groups/%d/alerts?cursor=zz", fx.g1), 400},
		{fx.admin, "/api/v1/fleet?lookback=48h", 400},
		{fx.viewer, fmt.Sprintf("/api/v1/groups/%d/series?from=%s&to=%s&step=15m&label=env:prod", fx.g1, from, to), 200},
		{fx.viewer, fmt.Sprintf("/api/v1/instances/%d/events?from=%s&to=%s", fx.b, from, to), 200},
		{fx.viewer, fmt.Sprintf("/api/v1/groups/%d/hotspots?range=2h", fx.g1), 200},
		{fx.viewer, fmt.Sprintf("/api/v1/groups/%d/alerts/summary?range=7d", fx.g1), 200},
		{fx.viewer, fmt.Sprintf("/api/v1/groups/%d/alerts?status=resolved", fx.g1), 200},
		{fx.viewer, fmt.Sprintf("/api/v1/fleet?group=%d", fx.g1), 200},
	}
	for _, c := range cases {
		if code, body := get(c.user, c.path); code != c.want {
			t.Errorf("%s as %d: %d %v, want %d", c.path, c.user, code, body, c.want)
		}
	}

	_, body := get(fx.viewer, fmt.Sprintf("/api/v1/groups/%d/series?from=%s&to=%s&step=15m&label=env:prod", fx.g1, from, to))
	series := body["series"].([]any)
	if len(series) != 2 || body["step_s"].(float64) != 900 || body["rollup"] != true {
		t.Fatalf("series body: %v", body)
	}
	// base is 5-minute aligned; the window widens outward to 15-minute boundaries.
	w, _ := NewWindow(fx.base, fx.base.Add(time.Hour), 15*time.Minute)
	if pts := series[0].(map[string]any)["points"].([]any); len(pts) != w.Buckets() {
		t.Fatalf("want %d x 15m points, got %d", w.Buckets(), len(pts))
	}
	_, body = get(fx.viewer, fmt.Sprintf("/api/v1/instances/%d/events?from=%s&to=%s", fx.b, from, to))
	if a := body["availability"].(float64); math.Abs(a-(1-190.0/3600)) > 1e-9 {
		t.Fatalf("availability %v", a)
	}
}
