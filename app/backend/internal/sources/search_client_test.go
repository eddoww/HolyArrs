package sources

import (
	"context"
	"testing"

	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
)

// A stored Client is kept while the Source is unchanged, so its limits hold
// from one search to the next, even when the Source leaves the timeout and
// window at their defaults.
func TestSearcherKeepsClientAcrossSearches(t *testing.T) {
	s := &Searcher{}
	src := domain.Source{Meta: domain.Meta{ID: 7}, Format: "torznab", URL: "https://indexer.example/api", Protocol: "torrent",
		Limits: domain.SourceLimits{SearchesPerWindow: 10}}
	a, err := s.client(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := s.client(context.Background(), src)
	if a != b {
		t.Fatal("a new Client for an unchanged Source")
	}
	src.Limits.GapMS = 500
	if c, _ := s.client(context.Background(), src); c == a {
		t.Fatal("a changed Source kept its old Client")
	}
}
