package database

import (
	"path"
	"reflect"
	"testing"
)

func createDB(t *testing.T) Database {
	db, err := SQLite(path.Join(t.TempDir(), "temp.db"))

	if err != nil {
		t.Fatalf("database creation failed: %v", err)
	}

	if err := db.Setup(); err != nil {
		t.Fatalf("database setup failed: %v", err)
	}

	return db
}

func TestSetup(t *testing.T) {
	createDB(t)
}

func TestEscape(t *testing.T) {
	testCases := []struct {
		input  string
		output string
	}{
		{input: "Hello, world!", output: `"Hello" "world"*`},
		{input: "test123", output: `"test123"*`},
		{input: `"double quotes" "are" "escaped properly"`, output: `"double" "quotes" "are" "escaped" "properly"*`},
		{input: `using keywords like AND and OR`, output: `"using" "keywords" "like" "AND" "and" "OR"*`},
		{input: `using * * * wildcards * * *`, output: `"using" "wildcards"*`},
	}

	for _, c := range testCases {
		if escape(c.input) != c.output {
			t.Fatalf("Expected %v, received %v", c.output, escape(c.input))
		}
	}
}

func TestPopQueue(t *testing.T) {
	db := createDB(t)

	db.AddToQueue("source1", []string{"https://example.com/"}, 1)

	// The first time, there should be an item to pop off the queue
	{
		res, err := db.PopQueue("source1")

		if res == nil {
			t.Fatalf("expected non-nil QueueItem")
		}

		if err != nil {
			t.Fatalf("error popping queue: %v", err)
		}
	}

	// The second time, the first item shouldn't be accessible via PopQueue
	{
		res, err := db.PopQueue("source1")

		if res != nil {
			t.Fatalf("expected nil QueueItem, got %v", res)
		}

		if err != nil {
			t.Fatalf("error popping queue: %v", err)
		}
	}
}

func TestPopQueueWithOtherSource(t *testing.T) {
	db := createDB(t)

	db.AddToQueue("source1", []string{"https://example.com/"}, 1)

	res, err := db.PopQueue("source2")

	if res != nil {
		t.Fatalf("expected nil, got %v", res)
	}

	if err != nil {
		t.Fatalf("error popping queue: %v", err)
	}
}

func TestProcessResult(t *testing.T) {
	want := []Match{
		{Highlighted: true, Content: "the quick brown fox"},
		{Highlighted: false, Content: "jumped over"},
		{Highlighted: true, Content: "the lazy"},
		{Highlighted: false, Content: "dog"},
	}
	matches := processResult("AAAAthe quick brown foxBBBBjumped overAAAAthe lazyBBBBdog", "AAAA", "BBBB")

	if !reflect.DeepEqual(want, matches) {
		t.Fatalf("wanted %+v, got %+v", want, matches)
	}
}

func TestProcessResultWithContentBeforeStart(t *testing.T) {
	want := []Match{
		{Highlighted: false, Content: "the quick"},
		{Highlighted: true, Content: "brown fox"},
		{Highlighted: false, Content: "jumped over"},
		{Highlighted: true, Content: "the lazy"},
		{Highlighted: false, Content: "dog"},
	}
	matches := processResult("the quickAAAAbrown foxBBBBjumped overAAAAthe lazyBBBBdog", "AAAA", "BBBB")

	if !reflect.DeepEqual(want, matches) {
		t.Fatalf("wanted %+v, got %+v", want, matches)
	}
}

func TestProcessResultEmptyEnd(t *testing.T) {
	want := []Match{
		{Highlighted: true, Content: "the quick brown fox"},
		{Highlighted: false, Content: "jumped over"},
		{Highlighted: true, Content: "the lazy dog"},
	}
	matches := processResult("AAAAthe quick brown foxBBBBjumped overAAAAthe lazy dogBBBB", "AAAA", "BBBB")

	if !reflect.DeepEqual(want, matches) {
		t.Fatalf("wanted %+v, got %+v", want, matches)
	}
}

func TestHasDocument(t *testing.T) {
	db := createDB(t)

	db.AddDocument("source1", 1, "https://example.com/", Finished, "Example Domain", "", "This domain is for use in illustrative examples in documents. You may use this domain in literature without prior coordination or asking for permission.")

	res, err := db.HasDocument("source1", "https://example.com/")
	if err != nil {
		t.Fatalf("error fetching document: %v", err)
	}
	if !*res {
		t.Fatalf("document was not added to database: hasDocument returned false")
	}

}

func TestSearchQuery(t *testing.T) {
	db := createDB(t)

	err := db.AddDocument("source1", 1, "https://example.com/", Finished, "Example Domain", "", "This domain is for use in illustrative examples in documents. You may use this domain in literature without prior coordination or asking for permission.")
	if err != nil {
		t.Fatalf("error adding document: %v", err)
	}

	phrases := []struct {
		phrase   string
		expected int
	}{
		{phrase: "Example", expected: 1},
		{phrase: "Example Domain", expected: 1},
		{phrase: "\"Example Domain\"", expected: 1},
		{phrase: "illustrative examp", expected: 1},
		{phrase: "illustrative examples", expected: 1},
		{phrase: "a_nonexistant_word", expected: 0},
	}

	for _, testCase := range phrases {
		results, count, err := db.Search([]string{"source1"}, testCase.phrase, 1, 10)

		if err != nil {
			t.Fatalf("error fetching results for query '%v': %v", testCase.phrase, err)
		}

		if *count != uint32(testCase.expected) {
			t.Fatalf("encountered unexpected result count: expected %v, got %v", testCase.expected, *count)
		}

		if len(results) != int(*count) {
			t.Fatalf("'count' did not match length of results: %v != %v", *count, len(results))
		}
	}
}

func TestCleanQueue(t *testing.T) {
	db := createDB(t)
	db.AddToQueue("source1", []string{"https://example.com/1", "https://example.com/2"}, 1)
	db.UpdateQueueEntry("source1", "https://example.com/1", Finished)
	db.UpdateQueueEntry("source1", "https://example.com/2", Error)
	{
		err := db.CleanQueue()
		if err != nil {
			t.Fatalf("error cleaning queue: %v", err)
		}
	}
	item, err := db.PopQueue("source1")
	if err != nil {
		t.Fatalf("error popping queue: %v", err)
	}
	if item != nil {
		t.Fatalf("item was not removed from queue: %v", item)
	}
}
