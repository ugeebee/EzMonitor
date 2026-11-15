package query

import (
	"testing"
	"time"
)

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestNewWindowAlignsOutward(t *testing.T) {
	w, err := NewWindow(mustTime("2026-11-10T10:02:13Z"), mustTime("2026-11-10T11:58:01Z"), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !w.From.Equal(mustTime("2026-11-10T10:00:00Z")) || !w.To.Equal(mustTime("2026-11-10T12:00:00Z")) {
		t.Fatalf("got %s..%s", w.From, w.To)
	}
	if w.Buckets() != 24 || !w.UsesRollup() {
		t.Fatalf("buckets=%d rollup=%v", w.Buckets(), w.UsesRollup())
	}
	// Already aligned bounds stay put.
	w, _ = NewWindow(mustTime("2026-11-10T10:00:00Z"), mustTime("2026-11-10T11:00:00Z"), time.Hour)
	if w.Buckets() != 1 {
		t.Fatalf("aligned window grew to %d buckets", w.Buckets())
	}
}

func TestNewWindowOddStepUsesEpochOrigin(t *testing.T) {
	// 7 minutes does not divide an hour; alignment must follow the Unix
	// epoch exactly like date_bin(..., 'epoch') in series.sql.
	w, err := NewWindow(mustTime("2026-11-10T10:00:00Z"), mustTime("2026-11-10T11:00:00Z"), 7*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if w.From.Unix()%(7*60) != 0 || w.To.Unix()%(7*60) != 0 || w.UsesRollup() {
		t.Fatalf("misaligned: %s..%s", w.From, w.To)
	}
}

func TestNewWindowRejects(t *testing.T) {
	from := mustTime("2026-11-01T00:00:00Z")
	cases := []struct {
		name     string
		from, to time.Time
		step     time.Duration
	}{
		{"empty", from, from, time.Minute},
		{"sub-second", from, from.Add(time.Hour), 500 * time.Millisecond},
		{"fractional seconds", from, from.Add(time.Hour), 1500 * time.Millisecond},
		{"too many buckets", from, from.Add(24 * time.Hour), 15 * time.Second},
		{"long raw scan", from, from.Add(48 * time.Hour), 7 * time.Minute},
		{"step over a day", from, from.Add(30 * 24 * time.Hour), 48 * time.Hour},
	}
	for _, c := range cases {
		if _, err := NewWindow(c.from, c.to, c.step); err == nil {
			t.Errorf("%s: expected an error", c.name)
		}
	}
}

func TestAutoStep(t *testing.T) {
	base := mustTime("2026-11-10T00:00:00Z")
	cases := map[time.Duration]time.Duration{
		5 * time.Minute:     time.Second,
		time.Hour:           15 * time.Second,
		24 * time.Hour:      5 * time.Minute,
		7 * 24 * time.Hour:  time.Hour,
		30 * 24 * time.Hour: 3 * time.Hour,
	}
	for span, want := range cases {
		if got := AutoStep(base, base.Add(span)); got != want {
			t.Errorf("AutoStep(%s) = %s, want %s", span, got, want)
		}
		if _, err := NewWindow(base, base.Add(span), 0); err != nil {
			t.Errorf("NewWindow(%s, auto): %v", span, err)
		}
	}
}

func TestPgTextArray(t *testing.T) {
	got := pgTextArray([]string{"prod", `a"b`, `c\d`, "x,y", ""})
	want := `{"prod","a\"b","c\\d","x,y",""}`
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	if pgTextArray(nil) != "{}" {
		t.Fatal("empty array")
	}
}

func TestCursorRoundTrip(t *testing.T) {
	c := Cursor{TriggeredAt: time.Date(2026, 11, 4, 17, 22, 6, 740531000, time.UTC), AlertID: 72008}
	d, err := DecodeCursor(c.Encode())
	if err != nil || !d.TriggeredAt.Equal(c.TriggeredAt) || d.AlertID != c.AlertID {
		t.Fatalf("round trip: %+v %v", d, err)
	}
	for _, bad := range []string{"!!", "MTIz", "YTpi"} {
		if _, err := DecodeCursor(bad); err == nil {
			t.Errorf("DecodeCursor(%q) accepted", bad)
		}
	}
}

func TestParams(t *testing.T) {
	if d, _ := durationParam("7d", 0); d != 7*24*time.Hour {
		t.Errorf("7d = %s", d)
	}
	for _, bad := range []string{"-5m", "0s", "xd", "1y"} {
		if _, err := durationParam(bad, 0); err == nil {
			t.Errorf("durationParam(%q) accepted", bad)
		}
	}
	l, err := labelParams([]string{"env:prod", "rack:r1:a"})
	if err != nil || l[1].Value != "r1:a" {
		t.Errorf("labels: %+v %v", l, err)
	}
	for _, bad := range []string{"env", "Env:prod", "env:", "1x:y"} {
		if _, err := labelParams([]string{bad}); err == nil {
			t.Errorf("labelParams(%q) accepted", bad)
		}
	}
}

func TestAvailability(t *testing.T) {
	from := mustTime("2026-11-10T00:00:00Z")
	to := from.Add(100 * time.Second)
	ev := []Event{{Kind: "gap", DurationS: 10}, {Kind: "reboot", DurationS: 50}, {Kind: "gap", DurationS: 15}}
	if a := Availability(ev, from, to, to); a != 0.75 {
		t.Fatalf("availability = %v", a)
	}
	// Future part of the window is not counted as downtime.
	if a := Availability(nil, from, to.Add(time.Hour), to); a != 1 {
		t.Fatalf("availability with future = %v", a)
	}
}
