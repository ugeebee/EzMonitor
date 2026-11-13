package query

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// UserFunc returns the authenticated user's ID. It is supplied by whoever
// owns authentication (session/JWT middleware); ok=false yields 401.
type UserFunc func(r *http.Request) (userID int64, ok bool)

type Handler struct {
	Store   *Store
	User    UserFunc
	Timeout time.Duration    // per-request query budget; default 5s
	Now     func() time.Time // for tests; default time.Now
}

// Limits on what one request may ask for.
const (
	defaultRange       = 24 * time.Hour
	maxHotspotWindow   = 24 * time.Hour     // percentiles read raw rows
	maxEventsWindow    = 7 * 24 * time.Hour // LAG() reads raw rows
	defaultLookback    = 5 * time.Minute
	maxLookback        = 24 * time.Hour
	defaultMaxGap      = 30 * time.Second
	defaultAlertsLimit = 50
	maxAlertsLimit     = 500
)

// Register mounts the read API (Go 1.22+ method/path patterns).
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/fleet", h.fleet)
	mux.HandleFunc("GET /api/v1/instances/{id}/series", h.instanceSeries)
	mux.HandleFunc("GET /api/v1/instances/{id}/events", h.instanceEvents)
	mux.HandleFunc("GET /api/v1/groups/{id}/series", h.groupSeries)
	mux.HandleFunc("GET /api/v1/groups/{id}/hotspots", h.hotspots)
	mux.HandleFunc("GET /api/v1/groups/{id}/alerts/summary", h.alertSummary)
	mux.HandleFunc("GET /api/v1/groups/{id}/alerts", h.alertHistory)
}

// ---------------------------------------------------------------------
// handlers
// ---------------------------------------------------------------------

// GET /api/v1/fleet?group=&lookback=5m
func (h *Handler) fleet(w http.ResponseWriter, r *http.Request) {
	uid, ctx, cancel, ok := h.begin(w, r)
	if !ok {
		return
	}
	defer cancel()
	q := r.URL.Query()
	gid, err := optionalID(q.Get("group"))
	if err != nil {
		badRequest(w, err)
		return
	}
	lookback, err := durationParam(q.Get("lookback"), defaultLookback)
	if err == nil && (lookback <= 0 || lookback > maxLookback) {
		err = fmt.Errorf("lookback must be in (0, %s]", maxLookback)
	}
	if err != nil {
		badRequest(w, err)
		return
	}
	if gid != nil && !h.requireGroup(ctx, w, uid, *gid) {
		return
	}
	rows, err := h.Store.FleetStatus(ctx, uid, gid, lookback)
	if err != nil {
		serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instances": rows})
}

// GET /api/v1/instances/{id}/series?from=&to=&range=&step=
func (h *Handler) instanceSeries(w http.ResponseWriter, r *http.Request) {
	uid, ctx, cancel, ok := h.begin(w, r)
	if !ok {
		return
	}
	defer cancel()
	iid, ok := pathID(w, r)
	if !ok || !h.requireInstance(ctx, w, uid, iid) {
		return
	}
	win, err := h.window(r)
	if err != nil {
		badRequest(w, err)
		return
	}
	h.series(ctx, w, r, SeriesRequest{UserID: uid, InstanceID: &iid, Window: win})
}

// GET /api/v1/groups/{id}/series?from=&to=&range=&step=&label=env:prod
func (h *Handler) groupSeries(w http.ResponseWriter, r *http.Request) {
	uid, ctx, cancel, ok := h.begin(w, r)
	if !ok {
		return
	}
	defer cancel()
	gid, ok := pathID(w, r)
	if !ok || !h.requireGroup(ctx, w, uid, gid) {
		return
	}
	win, err := h.window(r)
	if err != nil {
		badRequest(w, err)
		return
	}
	labels, err := labelParams(r.URL.Query()["label"])
	if err != nil {
		badRequest(w, err)
		return
	}
	h.series(ctx, w, r, SeriesRequest{UserID: uid, GroupID: &gid, Labels: labels, Window: win})
}

func (h *Handler) series(ctx context.Context, w http.ResponseWriter, r *http.Request, req SeriesRequest) {
	series, err := h.Store.Series(ctx, req)
	if err != nil {
		serverError(w, r, err)
		return
	}
	if series == nil {
		series = []InstanceSeries{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"from":   req.Window.From,
		"to":     req.Window.To,
		"step_s": int64(req.Window.Step / time.Second),
		"rollup": req.Window.UsesRollup(),
		"series": series,
	})
}

// GET /api/v1/groups/{id}/hotspots?metric=cpu_util&from=&to=&range=&limit=10
func (h *Handler) hotspots(w http.ResponseWriter, r *http.Request) {
	uid, ctx, cancel, ok := h.begin(w, r)
	if !ok {
		return
	}
	defer cancel()
	gid, ok := pathID(w, r)
	if !ok || !h.requireGroup(ctx, w, uid, gid) {
		return
	}
	q := r.URL.Query()
	metric := q.Get("metric")
	if metric == "" {
		metric = "cpu_util"
	}
	if !slices.Contains(Metrics, metric) {
		badRequest(w, fmt.Errorf("metric must be one of %s", strings.Join(Metrics, ", ")))
		return
	}
	from, to, err := h.timeRange(r, maxHotspotWindow)
	if err != nil {
		badRequest(w, err)
		return
	}
	limit, err := intParam(q.Get("limit"), 10, 1, 100)
	if err != nil {
		badRequest(w, err)
		return
	}
	rows, err := h.Store.Hotspots(ctx, uid, &gid, from, to, metric, limit)
	if err != nil {
		serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "metric": metric, "instances": rows})
}

// GET /api/v1/instances/{id}/events?from=&to=&range=&max_gap=30s
func (h *Handler) instanceEvents(w http.ResponseWriter, r *http.Request) {
	uid, ctx, cancel, ok := h.begin(w, r)
	if !ok {
		return
	}
	defer cancel()
	iid, ok := pathID(w, r)
	if !ok || !h.requireInstance(ctx, w, uid, iid) {
		return
	}
	from, to, err := h.timeRange(r, maxEventsWindow)
	if err != nil {
		badRequest(w, err)
		return
	}
	maxGap, err := durationParam(r.URL.Query().Get("max_gap"), defaultMaxGap)
	if err == nil && maxGap < time.Second {
		err = errors.New("max_gap must be at least 1s")
	}
	if err != nil {
		badRequest(w, err)
		return
	}
	events, err := h.Store.InstanceEvents(ctx, uid, iid, from, to, maxGap)
	if err != nil {
		serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"from": from, "to": to, "max_gap_s": maxGap.Seconds(),
		"availability": Availability(events, from, to, h.now()),
		"events":       events,
	})
}

// GET /api/v1/groups/{id}/alerts/summary?from=&to=&range=
func (h *Handler) alertSummary(w http.ResponseWriter, r *http.Request) {
	uid, ctx, cancel, ok := h.begin(w, r)
	if !ok {
		return
	}
	defer cancel()
	gid, ok := pathID(w, r)
	if !ok || !h.requireGroup(ctx, w, uid, gid) {
		return
	}
	from, to, err := h.timeRange(r, 0)
	if err != nil {
		badRequest(w, err)
		return
	}
	rows, err := h.Store.AlertSummary(ctx, uid, gid, from, to)
	if err != nil {
		serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "rules": rows})
}

// GET /api/v1/groups/{id}/alerts?status=active|resolved&limit=50&cursor=
func (h *Handler) alertHistory(w http.ResponseWriter, r *http.Request) {
	uid, ctx, cancel, ok := h.begin(w, r)
	if !ok {
		return
	}
	defer cancel()
	gid, ok := pathID(w, r)
	if !ok || !h.requireGroup(ctx, w, uid, gid) {
		return
	}
	q := r.URL.Query()
	var status *string
	if s := q.Get("status"); s != "" {
		if s != "active" && s != "resolved" {
			badRequest(w, errors.New("status must be active or resolved"))
			return
		}
		status = &s
	}
	limit, err := intParam(q.Get("limit"), defaultAlertsLimit, 1, maxAlertsLimit)
	if err != nil {
		badRequest(w, err)
		return
	}
	var after *Cursor
	if c := q.Get("cursor"); c != "" {
		if after, err = DecodeCursor(c); err != nil {
			badRequest(w, err)
			return
		}
	}
	rows, next, err := h.Store.AlertHistory(ctx, uid, gid, after, status, limit)
	if err != nil {
		serverError(w, r, err)
		return
	}
	var nextStr *string
	if next != nil {
		s := next.Encode()
		nextStr = &s
	}
	writeJSON(w, http.StatusOK, map[string]any{"alerts": rows, "next_cursor": nextStr})
}

// ---------------------------------------------------------------------
// request plumbing
// ---------------------------------------------------------------------

func (h *Handler) begin(w http.ResponseWriter, r *http.Request) (int64, context.Context, context.CancelFunc, bool) {
	uid, ok := h.User(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return 0, nil, nil, false
	}
	t := h.Timeout
	if t == 0 {
		t = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), t)
	return uid, ctx, cancel, true
}

func (h *Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func (h *Handler) requireGroup(ctx context.Context, w http.ResponseWriter, uid, gid int64) bool {
	_, ok, err := h.Store.GroupRole(ctx, uid, gid)
	if err != nil {
		serverError(w, nil, err)
		return false
	}
	if !ok {
		writeError(w, http.StatusNotFound, "group not found")
	}
	return ok
}

func (h *Handler) requireInstance(ctx context.Context, w http.ResponseWriter, uid, iid int64) bool {
	_, ok, err := h.Store.InstanceRole(ctx, uid, iid)
	if err != nil {
		serverError(w, nil, err)
		return false
	}
	if !ok {
		writeError(w, http.StatusNotFound, "instance not found")
	}
	return ok
}

// timeRange reads from/to/range: explicit RFC 3339 bounds, or range=24h
// ending now (the default). max = 0 means unlimited.
func (h *Handler) timeRange(r *http.Request, max time.Duration) (time.Time, time.Time, error) {
	q := r.URL.Query()
	now := h.now().UTC()
	to := now
	if s := q.Get("to"); s != "" {
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("to: %w", err)
		}
		to = t.UTC()
	}
	var from time.Time
	switch {
	case q.Get("from") != "" && q.Get("range") != "":
		return time.Time{}, time.Time{}, errors.New("give from or range, not both")
	case q.Get("from") != "":
		t, err := time.Parse(time.RFC3339Nano, q.Get("from"))
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("from: %w", err)
		}
		from = t.UTC()
	default:
		d, err := durationParam(q.Get("range"), defaultRange)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		from = to.Add(-d)
	}
	if !from.Before(to) {
		return time.Time{}, time.Time{}, errors.New("from must be before to")
	}
	if max > 0 && to.Sub(from) > max {
		return time.Time{}, time.Time{}, fmt.Errorf("window may be at most %s", max)
	}
	return from, to, nil
}

func (h *Handler) window(r *http.Request) (Window, error) {
	from, to, err := h.timeRange(r, 0)
	if err != nil {
		return Window{}, err
	}
	step, err := durationParam(r.URL.Query().Get("step"), 0)
	if err != nil {
		return Window{}, err
	}
	return NewWindow(from, to, step)
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "not found")
		return 0, false
	}
	return id, true
}

func optionalID(s string) (*int64, error) {
	if s == "" {
		return nil, nil
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return nil, fmt.Errorf("invalid id %q", s)
	}
	return &id, nil
}

// durationParam accepts Go durations ("15s", "5m", "24h") and a "d" suffix
// for days ("7d").
func durationParam(s string, def time.Duration) (time.Duration, error) {
	if s == "" {
		return def, nil
	}
	if n, ok := strings.CutSuffix(s, "d"); ok {
		days, err := strconv.Atoi(n)
		if err != nil || days <= 0 {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	return d, nil
}

func intParam(s string, def, min, max int) (int, error) {
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < min || n > max {
		return 0, fmt.Errorf("expected an integer in [%d, %d], got %q", min, max, s)
	}
	return n, nil
}

var labelKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`) // same rule as instance_labels

// labelParams parses repeated label=key:value parameters.
func labelParams(raw []string) ([]LabelFilter, error) {
	out := make([]LabelFilter, 0, len(raw))
	for _, s := range raw {
		k, v, ok := strings.Cut(s, ":")
		if !ok || !labelKeyRe.MatchString(k) || v == "" || len(v) > 100 {
			return nil, fmt.Errorf("label must be key:value, got %q", s)
		}
		out = append(out, LabelFilter{Key: k, Value: v})
	}
	return out, nil
}

// Encode renders an opaque cursor for the next_cursor field.
func (c Cursor) Encode() string {
	raw := fmt.Sprintf("%d:%d", c.TriggeredAt.UnixMicro(), c.AlertID)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func DecodeCursor(s string) (*Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, errors.New("invalid cursor")
	}
	ts, id, ok := strings.Cut(string(raw), ":")
	us, err1 := strconv.ParseInt(ts, 10, 64)
	aid, err2 := strconv.ParseInt(id, 10, 64)
	if !ok || err1 != nil || err2 != nil {
		return nil, errors.New("invalid cursor")
	}
	return &Cursor{TriggeredAt: time.UnixMicro(us).UTC(), AlertID: aid}, nil
}

// ---------------------------------------------------------------------
// responses
// ---------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func badRequest(w http.ResponseWriter, err error) {
	writeError(w, http.StatusBadRequest, err.Error())
}

func serverError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.DeadlineExceeded) {
		writeError(w, http.StatusGatewayTimeout, "query timed out; narrow the time range")
		return
	}
	path := ""
	if r != nil {
		path = r.URL.Path
	}
	log.Printf("query api %s: %v", path, err)
	writeError(w, http.StatusInternalServerError, "internal error")
}
