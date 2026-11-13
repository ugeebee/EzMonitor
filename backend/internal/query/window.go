package query

import (
	"errors"
	"fmt"
	"time"
)

const (
	// MaxBuckets caps points per series so a chart never asks for more
	// rows than it can draw (2,000 points ~ one per pixel column).
	MaxBuckets = 2000
	// targetBuckets is what AutoStep aims for when the client sends no step.
	targetBuckets = 300
	// RollupStep is the granularity of metrics_rollup_5m. Steps that are a
	// multiple of it are served from the rollup (see series.sql).
	RollupStep = 5 * time.Minute
	// MaxRawSpan bounds windows whose step forces a raw-row scan.
	MaxRawSpan = 24 * time.Hour
)

// niceSteps are the steps AutoStep chooses from.
var niceSteps = []time.Duration{
	time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second,
	time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute,
	time.Hour, 2 * time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour,
}

// Window is a bucket-aligned time range: From and To are multiples of Step
// counted from the Unix epoch, which is the origin series.sql passes to
// date_bin, so Go and PostgreSQL agree on bucket boundaries.
type Window struct {
	From, To time.Time
	Step     time.Duration
}

// Buckets is the number of points each series in this window will have.
func (w Window) Buckets() int { return int(w.To.Sub(w.From) / w.Step) }

// UsesRollup reports whether series.sql will read metrics_rollup_5m.
func (w Window) UsesRollup() bool { return w.Step%RollupStep == 0 }

// AutoStep picks the smallest nice step that keeps the window near
// targetBuckets points.
func AutoStep(from, to time.Time) time.Duration {
	span := to.Sub(from)
	for _, s := range niceSteps {
		if span/s <= targetBuckets {
			return s
		}
	}
	return niceSteps[len(niceSteps)-1]
}

// NewWindow aligns [from, to) outward to step boundaries and validates it.
// step == 0 means "choose for me".
func NewWindow(from, to time.Time, step time.Duration) (Window, error) {
	if !from.Before(to) {
		return Window{}, errors.New("from must be before to")
	}
	if step == 0 {
		step = AutoStep(from, to)
	}
	if step < time.Second || step%time.Second != 0 {
		return Window{}, errors.New("step must be a whole number of seconds, at least 1s")
	}
	if step > 24*time.Hour {
		return Window{}, errors.New("step must be at most 24h")
	}
	w := Window{From: alignDown(from, step), To: alignUp(to, step), Step: step}
	if !w.UsesRollup() && w.To.Sub(w.From) > MaxRawSpan {
		return Window{}, fmt.Errorf("windows over %s need a step that is a multiple of %s", MaxRawSpan, RollupStep)
	}
	if n := w.Buckets(); n > MaxBuckets {
		return Window{}, fmt.Errorf("%d buckets requested; use a larger step (max %d buckets)", n, MaxBuckets)
	}
	return w, nil
}

func alignDown(t time.Time, step time.Duration) time.Time {
	s := int64(step / time.Second)
	u := t.Unix()
	r := u % s
	if r < 0 {
		r += s
	}
	return time.Unix(u-r, 0).UTC()
}

func alignUp(t time.Time, step time.Duration) time.Time {
	d := alignDown(t, step)
	if d.Equal(t) {
		return d
	}
	return d.Add(step)
}

// pgInterval renders a duration as a PostgreSQL interval literal. Strings
// work with every database/sql driver (pgx stdlib and lib/pq alike).
func pgInterval(d time.Duration) string {
	return fmt.Sprintf("%d microseconds", d.Microseconds())
}
