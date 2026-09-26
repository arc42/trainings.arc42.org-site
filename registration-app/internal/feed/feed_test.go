package feed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const sample = `{"generated":"x","courses":[
 {"id":"msa","short_title":"MSA","title":"Mastering Software Architectures",
  "trainers":["Peter Hruschka","Gernot Starke"],"future_field":1,
  "dates":[
   {"id":"msa-dez-2026","code":"26-12 MSA","start":"2026-12-01","end":"2026-12-04","city":"München","format":"public","language":"de","status":"open","price":{"amount":2890,"currency":"EUR","early_bird":{"amount":2690,"until":"2026-11-02"}}},
   {"id":"msa-online-sep-2026","code":"26-09 MSA-EN","start":"2026-09-29","end":"2026-10-01","format":"online","language":"en","status":"open","trainers":["Wolfgang Reimesch"]},
   {"id":"msa-w","code":"27-03 MSA","start":"2027-03-02","end":"2027-03-05","format":"public","language":"de","status":"waitlist"},
   {"id":"msa-f","code":"27-06 MSA","start":"2027-06-08","end":"2027-06-11","format":"public","language":"de","status":"full"}
  ]}]}`

func server(t *testing.T, body *atomic.Value, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		b := body.Load().(string)
		if b == "" {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(b))
	}))
	t.Cleanup(s.Close)
	return s
}

func at(s string) func() time.Time {
	tm, _ := time.Parse(time.RFC3339, s)
	return func() time.Time { return tm }
}

func TestLookupClassifiesCodes(t *testing.T) {
	var body atomic.Value
	body.Store(sample)
	var hits atomic.Int32
	f := New(server(t, &body, &hits).URL, nil, at("2026-09-30T10:00:00Z"))
	ctx := context.Background()

	cases := map[string]Result{
		"26-12 MSA":    Bookable,
		"26-09 MSA-EN": Bookable, // ends 2026-10-01, today is 2026-09-30
		"27-03 MSA":    Bookable, // waitlist is registrable
		"27-06 MSA":    Closed,   // full
		"99-99 NOPE":   Unknown,
	}
	for code, want := range cases {
		if _, got := f.Lookup(ctx, code); got != want {
			t.Errorf("Lookup(%q) = %v, want %v", code, got, want)
		}
	}
	if hits.Load() != 1 {
		t.Errorf("feed fetched %d times, want 1 (cached)", hits.Load())
	}
}

func TestLookupResolvesTrainersAndIgnoresEarlyBird(t *testing.T) {
	var body atomic.Value
	body.Store(sample)
	var hits atomic.Int32
	f := New(server(t, &body, &hits).URL, nil, at("2026-09-30T10:00:00Z"))
	e, _ := f.Lookup(context.Background(), "26-12 MSA")
	if len(e.Trainers) != 2 || e.Trainers[0] != "Peter Hruschka" {
		t.Errorf("course-default trainers not applied: %v", e.Trainers)
	}
	if e.Price == nil || e.Price.Amount != 2890 {
		t.Errorf("price = %+v, want the regular 2890", e.Price)
	}
	e, _ = f.Lookup(context.Background(), "26-09 MSA-EN")
	if len(e.Trainers) != 1 || e.Trainers[0] != "Wolfgang Reimesch" {
		t.Errorf("date trainers must win: %v", e.Trainers)
	}
}

// "Today" is the German calendar day. 23:30 UTC on 1 October is already
// 2 October in Berlin, so a date that ended on 1 October is past.
func TestPastIsDecidedInBerlinTime(t *testing.T) {
	var body atomic.Value
	body.Store(sample)
	var hits atomic.Int32
	f := New(server(t, &body, &hits).URL, nil, at("2026-10-01T23:30:00Z"))
	if _, got := f.Lookup(context.Background(), "26-09 MSA-EN"); got != Closed {
		t.Errorf("got %v, want Closed", got)
	}
}

func TestOutageKeepsTheLastGoodCopy(t *testing.T) {
	var body atomic.Value
	body.Store(sample)
	var hits atomic.Int32
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	f := New(server(t, &body, &hits).URL, nil, func() time.Time { return now })
	f.Lookup(context.Background(), "26-12 MSA")

	body.Store("")
	now = now.Add(TTL + time.Minute)
	if _, got := f.Lookup(context.Background(), "26-12 MSA"); got != Bookable {
		t.Errorf("after a failed refresh got %v, want Bookable from the cached copy", got)
	}
}

func TestNeverFetchedIsUnavailableAndRetriesAreSpaced(t *testing.T) {
	var body atomic.Value
	body.Store("")
	var hits atomic.Int32
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	f := New(server(t, &body, &hits).URL, nil, func() time.Time { return now })
	if _, got := f.Lookup(context.Background(), "26-12 MSA"); got != Unavailable {
		t.Fatalf("got %v, want Unavailable", got)
	}
	f.Lookup(context.Background(), "26-12 MSA")
	if hits.Load() != 1 {
		t.Errorf("retried after %d requests within RetryPause, want 1", hits.Load())
	}
	now = now.Add(RetryPause + time.Second)
	body.Store(sample)
	if _, got := f.Lookup(context.Background(), "26-12 MSA"); got != Bookable {
		t.Errorf("after the pause got %v, want Bookable", got)
	}
}

func TestAnEmptyFeedIsNotTrusted(t *testing.T) {
	var body atomic.Value
	body.Store(`{"courses":[]}`)
	var hits atomic.Int32
	f := New(server(t, &body, &hits).URL, nil, at("2026-09-30T10:00:00Z"))
	if _, got := f.Lookup(context.Background(), "26-12 MSA"); got != Unavailable {
		t.Errorf("got %v, want Unavailable: an empty feed would silently drop every real booking", got)
	}
}
