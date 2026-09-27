// Package feed answers two questions from the site's public course feed
// (/api/trainings.json): is this booking code bookable right now, and what are
// its facts. It is a consumer of that feed like the other arc42 sites, bound by
// ADR-0004: it reads only fields that exist and tolerates new ones.
package feed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
	_ "time/tzdata" // the production image is FROM scratch and has no zoneinfo
)

type Price struct {
	Amount   int    `json:"amount"`
	Currency string `json:"currency"`
}

type Date struct {
	ID       string   `json:"id"`
	Code     string   `json:"code"`
	Start    string   `json:"start"`
	End      string   `json:"end"`
	City     string   `json:"city"`
	Format   string   `json:"format"`
	Language string   `json:"language"`
	Status   string   `json:"status"`
	Trainers []string `json:"trainers"`
	Price    *Price   `json:"price"`
}

type course struct {
	ID         string   `json:"id"`
	ShortTitle string   `json:"short_title"`
	Title      string   `json:"title"`
	Trainers   []string `json:"trainers"`
	Dates      []Date   `json:"dates"`
}

// Entry is one date with the course facts it needs. Trainers is already
// resolved: the date's own list, else the course default.
type Entry struct {
	CourseTitle      string
	CourseShortTitle string
	Date
}

type Result int

const (
	Bookable    Result = iota // open or waitlist, not past
	Closed                    // exists, but full, cancelled or past
	Unknown                   // not in the feed
	Unavailable               // the feed has never been fetched successfully
)

// How long a fetched feed is trusted, and how long to wait after a failed
// fetch before trying again, so an outage of the site does not turn every
// submission into a slow timeout.
const (
	TTL         = 5 * time.Minute
	RetryPause  = 30 * time.Second
	httpTimeout = 5 * time.Second
)

var berlin = mustBerlin()

func mustBerlin() *time.Location {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		panic(err)
	}
	return loc
}

type Feed struct {
	url    string
	client *http.Client
	now    func() time.Time

	mu        sync.Mutex
	entries   map[string]Entry // by booking code
	fetchedAt time.Time
	triedAt   time.Time
}

func New(url string, client *http.Client, now func() time.Time) *Feed {
	if client == nil {
		client = &http.Client{Timeout: httpTimeout}
	}
	return &Feed{url: url, client: client, now: now}
}

// Lookup refreshes the cache when it is stale, then classifies code. A failed
// refresh keeps the last good copy: a GitHub Pages outage must not reject
// real bookings.
func (f *Feed) Lookup(ctx context.Context, code string) (Entry, Result) {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := f.now()
	if (f.entries == nil || now.Sub(f.fetchedAt) > TTL) && now.Sub(f.triedAt) > RetryPause {
		f.triedAt = now
		// Detached from the request: a client hanging up must not cancel the
		// service's own fetch and leave every code unchecked for RetryPause.
		// The http.Client timeout still bounds it.
		if entries, err := f.fetch(context.WithoutCancel(ctx)); err == nil {
			f.entries, f.fetchedAt = entries, now
		}
	}
	if f.entries == nil {
		return Entry{}, Unavailable
	}
	e, ok := f.entries[code]
	if !ok {
		return Entry{}, Unknown
	}
	today := now.In(berlin).Format("2006-01-02")
	if (e.Status == "open" || e.Status == "waitlist") && e.End >= today {
		return e, Bookable
	}
	return e, Closed
}

func (f *Feed) fetch(ctx context.Context) (map[string]Entry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("feed: %s", resp.Status)
	}
	var doc struct {
		Courses []course `json:"courses"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("feed: %w", err)
	}
	entries := map[string]Entry{}
	for _, c := range doc.Courses {
		for _, d := range c.Dates {
			if len(d.Trainers) == 0 {
				d.Trainers = c.Trainers
			}
			entries[d.Code] = Entry{CourseTitle: c.Title, CourseShortTitle: c.ShortTitle, Date: d}
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("feed: no dates at all, refusing to treat every code as unknown")
	}
	return entries, nil
}
