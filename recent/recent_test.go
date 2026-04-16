package recent

import "testing"

func TestSortEntriesOrdersNewestFirst(t *testing.T) {
	entries := []Entry{
		{Path: "/b", LastUsedAt: 10},
		{Path: "/a", LastUsedAt: 30},
		{Path: "/c", LastUsedAt: 20},
	}

	sortEntries(entries)

	if entries[0].Path != "/a" || entries[1].Path != "/c" || entries[2].Path != "/b" {
		t.Fatalf("unexpected order: %#v", entries)
	}
}
