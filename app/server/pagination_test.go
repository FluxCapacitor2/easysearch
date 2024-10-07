package server

import (
	"net/url"
	"reflect"
	"testing"
)

func TestPagination(t *testing.T) {

	url, err := url.Parse("http://localhost:8080/?page=1&q=hello&source=test")

	if err != nil {
		t.Fatalf("Failed to parse URL: %v", err)
	}

	table := []struct {
		page      int32
		pageCount int32
		want      []ResultsPage
	}{
		// When the cursor is at the start
		{page: 1, pageCount: 10,
			want: []ResultsPage{
				{Number: "«", Current: true, URL: "http://localhost:8080/?page=1&q=hello&source=test"},
				{Number: "←", Current: true, URL: "http://localhost:8080/?page=0&q=hello&source=test"},
				{Number: "1", Current: true, URL: "http://localhost:8080/?page=1&q=hello&source=test"},
				{Number: "2", Current: false, URL: "http://localhost:8080/?page=2&q=hello&source=test"},
				{Number: "3", Current: false, URL: "http://localhost:8080/?page=3&q=hello&source=test"},
				{Number: "4", Current: false, URL: "http://localhost:8080/?page=4&q=hello&source=test"},
				{Number: "5", Current: false, URL: "http://localhost:8080/?page=5&q=hello&source=test"},
				{Number: "6", Current: false, URL: "http://localhost:8080/?page=6&q=hello&source=test"},
				{Number: "7", Current: false, URL: "http://localhost:8080/?page=7&q=hello&source=test"},
				{Number: "8", Current: false, URL: "http://localhost:8080/?page=8&q=hello&source=test"},
				{Number: "9", Current: false, URL: "http://localhost:8080/?page=9&q=hello&source=test"},
				{Number: "10", Current: false, URL: "http://localhost:8080/?page=10&q=hello&source=test"},
				{Number: "→", Current: false, URL: "http://localhost:8080/?page=2&q=hello&source=test"},
				{Number: "»", Current: false, URL: "http://localhost:8080/?page=10&q=hello&source=test"},
			}},
		// Cursor in the middle of the results
		{page: 5, pageCount: 20,
			want: []ResultsPage{
				{Number: "«", Current: false, URL: "http://localhost:8080/?page=1&q=hello&source=test"},
				{Number: "←", Current: false, URL: "http://localhost:8080/?page=4&q=hello&source=test"},
				{Number: "1", Current: false, URL: "http://localhost:8080/?page=1&q=hello&source=test"},
				{Number: "2", Current: false, URL: "http://localhost:8080/?page=2&q=hello&source=test"},
				{Number: "3", Current: false, URL: "http://localhost:8080/?page=3&q=hello&source=test"},
				{Number: "4", Current: false, URL: "http://localhost:8080/?page=4&q=hello&source=test"},
				{Number: "5", Current: true, URL: "http://localhost:8080/?page=5&q=hello&source=test"},
				{Number: "6", Current: false, URL: "http://localhost:8080/?page=6&q=hello&source=test"},
				{Number: "7", Current: false, URL: "http://localhost:8080/?page=7&q=hello&source=test"},
				{Number: "8", Current: false, URL: "http://localhost:8080/?page=8&q=hello&source=test"},
				{Number: "9", Current: false, URL: "http://localhost:8080/?page=9&q=hello&source=test"},
				{Number: "10", Current: false, URL: "http://localhost:8080/?page=10&q=hello&source=test"},
				{Number: "→", Current: false, URL: "http://localhost:8080/?page=6&q=hello&source=test"},
				{Number: "»", Current: false, URL: "http://localhost:8080/?page=20&q=hello&source=test"},
			}},
		// Cursor at the end
		{page: 20, pageCount: 20,
			want: []ResultsPage{
				{Number: "«", Current: false, URL: "http://localhost:8080/?page=1&q=hello&source=test"},
				{Number: "←", Current: false, URL: "http://localhost:8080/?page=19&q=hello&source=test"},
				{Number: "11", Current: false, URL: "http://localhost:8080/?page=11&q=hello&source=test"},
				{Number: "12", Current: false, URL: "http://localhost:8080/?page=12&q=hello&source=test"},
				{Number: "13", Current: false, URL: "http://localhost:8080/?page=13&q=hello&source=test"},
				{Number: "14", Current: false, URL: "http://localhost:8080/?page=14&q=hello&source=test"},
				{Number: "15", Current: false, URL: "http://localhost:8080/?page=15&q=hello&source=test"},
				{Number: "16", Current: false, URL: "http://localhost:8080/?page=16&q=hello&source=test"},
				{Number: "17", Current: false, URL: "http://localhost:8080/?page=17&q=hello&source=test"},
				{Number: "18", Current: false, URL: "http://localhost:8080/?page=18&q=hello&source=test"},
				{Number: "19", Current: false, URL: "http://localhost:8080/?page=19&q=hello&source=test"},
				{Number: "20", Current: true, URL: "http://localhost:8080/?page=20&q=hello&source=test"},
				{Number: "→", Current: true, URL: "http://localhost:8080/?page=21&q=hello&source=test"},
				{Number: "»", Current: true, URL: "http://localhost:8080/?page=20&q=hello&source=test"},
			}},
	}

	for i, testCase := range table {
		actual := CreatePagination(url, testCase.page, 10, testCase.pageCount)
		if !reflect.DeepEqual(testCase.want, actual) {
			t.Fatalf("test case %v failed: wanted %+v, got %+v", i+1, testCase.want, actual)
		}
	}
}
