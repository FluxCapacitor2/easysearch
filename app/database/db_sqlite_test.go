package database

import (
	"reflect"
	"testing"
)

func TestSetup(t *testing.T) {
	_, err := SQLite(":memory:")

	if err != nil {
		t.Fatalf("database creation failed: %v", err)
	}
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
	db, err := SQLite(":memory:")

	if err != nil {
		t.Fatalf("database creation failed: %v", err)
	}

	if err := db.Setup(); err != nil {
		t.Fatalf("database setup failed: %v", err)
	}

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
	db, err := SQLite(":memory:")

	if err != nil {
		t.Fatalf("database creation failed: %v", err)
	}

	if err := db.Setup(); err != nil {
		t.Fatalf("database setup failed: %v", err)
	}

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
