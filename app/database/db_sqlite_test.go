package database

import (
	"path"
	"reflect"
	"testing"
	"time"
)

func createDB(t *testing.T) Database {
	db, err := SQLiteFromFile(path.Join(t.TempDir(), "temp.db"))

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

	db.AddToQueue("source1", "https://www.bswanson.dev", []string{"https://example.com/"}, 1, false)

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

	db.AddToQueue("source1", "https://www.bswanson.dev", []string{"https://example.com/"}, 1, false)

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

	db.AddDocument("source1", 1, "", "https://example.com/", Finished, "Example Domain", "", "This domain is for use in illustrative examples in documents. You may use this domain in literature without prior coordination or asking for permission.", "")

	res, err := db.HasDocument("source1", "https://example.com/")
	if err != nil {
		t.Fatalf("error fetching document: %v", err)
	}
	if !*res {
		t.Fatalf("document was not added to database: hasDocument returned false")
	}
}

func TestGetDocument(t *testing.T) {
	db := createDB(t)

	page := Page{
		Source:      "source1",
		Referrer:    "",
		URL:         "https://example.com/",
		Title:       "Example Domain",
		Description: "",
		Content:     "This domain is for use in illustrative examples in documents. You may use this domain in literature without prior coordination or asking for permission.",
		Depth:       1,
		Status:      Finished,
		ErrorInfo:   "",
	}

	db.AddDocument(page.Source, page.Depth, page.Referrer, page.URL, page.Status, page.Title, page.Description, page.Content, page.ErrorInfo)

	doc, err := db.GetDocument("source1", "https://example.com/")
	if err != nil {
		t.Fatalf("error fetching document: %v", err)
	}
	if doc == nil {
		t.Fatalf("document was not added to database: hasDocument returned false")
	}
	doc.CrawledAt = "" // We don't want to compare CrawledAt because it's generated when the row is added

	if !reflect.DeepEqual(page, *doc) {
		t.Fatalf("document was improperly added or retrieved from the database: expected %v, got %v", page, doc)
	}
}

func TestDeleteCanonicalsOnDeletePage(t *testing.T) {
	db := createDB(t)

	err := db.AddDocument("source1", 0, "", "https://example.com/", Finished, "Title", "Description", "Content", "")
	if err != nil {
		t.Fatalf("failed to add page: %v", err)
	}
	err = db.SetCanonical("source1", "https://www.example.com/", "https://example.com/")
	if err != nil {
		t.Fatalf("failed to set canonical: %v", err)
	}
	err = db.RemoveDocument("source1", "https://example.com/")
	if err != nil {
		t.Fatalf("failed to delete document: %v", err)
	}

	canonical, err := db.GetCanonical("source1", "https://www.example.com/")
	if err != nil {
		t.Fatalf("failed to get canonical: %v", err)
	}

	if canonical != nil {
		t.Fatalf("canonical was not deleted: expected nil, got %+v", canonical)
	}
}

func TestSearchQuery(t *testing.T) {
	db := createDB(t)

	err := db.AddDocument("source1", 1, "", "https://example.com/", Finished, "Example Domain", "", "This domain is for use in illustrative examples in documents. You may use this domain in literature without prior coordination or asking for permission.", "")
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

func TestQueuePagesOlderThan(t *testing.T) {
	db := createDB(t)

	err := db.AddDocument("source", 1, "", "", Finished, "", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error adding document: %v", err)
	}
	time.Sleep(1 * time.Second) // Ensure the document is at least 1 second old
	err = db.QueuePagesOlderThan("source", 0)
	if err != nil {
		t.Fatalf("failed to queue pages older than 0 days: %v", err)
	}

	doc, err := db.PopQueue("source")
	if err != nil {
		t.Fatalf("error popping queue: %v", err)
	}
	if doc == nil {
		t.Fatalf("page was not queued; expected to be able to pop 1 item off the queue")
	}

	doc, err = db.PopQueue("source")
	if err != nil {
		t.Fatalf("error popping queue: %v", err)
	}
	if doc != nil {
		t.Fatalf("more than one page was queued; expected exactly 1 page in the queue")
	}
}
