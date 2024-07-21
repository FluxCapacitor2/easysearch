package database

import (
	"reflect"
	"testing"
)

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
