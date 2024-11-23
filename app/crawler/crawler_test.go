package crawler

import (
	"path"
	"reflect"
	"testing"

	"github.com/fluxcapacitor2/easysearch/app/config"
	"github.com/fluxcapacitor2/easysearch/app/database"
)

func createDB(t *testing.T) database.Database {
	db, err := database.SQLiteFromFile(path.Join(t.TempDir(), "temp.db"))

	if err != nil {
		t.Fatalf("database creation failed: %v", err)
	}

	if err := db.Setup(); err != nil {
		t.Fatalf("database setup failed: %v", err)
	}

	return db
}

func TestCrawl(t *testing.T) {
	db := createDB(t)
	source := config.Source{
		ID:             "example",
		AllowedDomains: []string{"www.example.com"},
	}

	url := "https://www.example.com"
	res, err := Crawl(source, 1, url, db, url)

	if err != nil {
		t.Fatalf("error crawling URL %v: %v\n", url, err)
	}

	if res.Canonical != url {
		t.Fatalf("unexpected canonical: %v != %v\n", res.Canonical, url)
	}

	if len(res.URLs) != 1 || res.URLs[0] != "https://www.iana.org/domains/example" {
		t.Fatalf("unexpected URLs found: %v\n", res.URLs)
	}
}

func TestCrawlWithRedirect(t *testing.T) {
	db := createDB(t)
	source := config.Source{
		ID:             "example",
		AllowedDomains: []string{"bswanson.dev", "www.bswanson.dev"},
	}

	url := "https://bswanson.dev"
	expectedCanonical := "https://www.bswanson.dev/"
	res, err := Crawl(source, 1, url, db, url)

	if err != nil {
		t.Fatalf("error crawling URL %v: %v\n", url, err)
	}

	if res.Canonical != expectedCanonical {
		t.Fatalf("unexpected canonical: %v != %v\n", res.Canonical, expectedCanonical)
	}
}

func TestCrawlWithForbiddenDomain(t *testing.T) {
	db := createDB(t)
	source := config.Source{
		ID:             "example",
		AllowedDomains: []string{"www.example.com"},
	}

	url := "https://bswanson.dev/portfolio"
	_, err := Crawl(source, 1, url, db, url)

	if err == nil {
		t.Fatalf("expected error due to forbidden domain; none was received")
	}
}

func TestCrawlWithServerError(t *testing.T) {
	db := createDB(t)
	source := config.Source{
		ID:             "example",
		AllowedDomains: []string{"httpstat.us"},
	}

	url := "https://httpstat.us/500"
	_, err := Crawl(source, 1, url, db, url)

	if err == nil {
		t.Fatalf("expected error due to 500 status; none was received")
	}

	if err.Error() != "Internal Server Error" {
		t.Fatalf("expected error due to 500 status; got %v\n", err)
	}
}

func TestCrawlWithPageNotFound(t *testing.T) {
	db := createDB(t)
	source := config.Source{
		ID:             "example",
		AllowedDomains: []string{"httpstat.us"},
	}

	url := "https://httpstat.us/404"
	_, err := Crawl(source, 1, url, db, url)

	if err.Error() != "Not Found" {
		t.Fatalf("expected error due to 404 status; got %v\n", err)
	}
}

func TestSitemap(t *testing.T) {
	db := createDB(t)
	source := config.Source{
		ID:             "example",
		AllowedDomains: []string{"www.google.com"},
	}

	url := "https://www.google.com/sitemap.xml"
	res, err := Crawl(source, 1, url, db, url)

	if err != nil {
		t.Errorf("error crawling Google sitemap: %v\n", err)
	}

	if len(res.URLs) < 20 {
		t.Errorf("sitemap URLs were not discovered - expected >=20 URLs, got %+v\n", res)
	}
}

func TestTruncate(t *testing.T) {

	tests := []struct {
		max      int
		strings  []string
		expected []string
	}{
		{5, []string{"123", "45", "6"}, []string{"123", "45", ""}},
		{6, []string{"123", "45", "6"}, []string{"123", "45", "6"}},
		{10, []string{"123", "45", "6"}, []string{"123", "45", "6"}},
		{2, []string{"123", "45", "6"}, []string{"12", "", ""}},
		{5, []string{"lorem ipsum"}, []string{"lorem"}},
		{5, []string{"lorem", "", "", "", "", "", "ipsum"}, []string{"lorem", "", "", "", "", "", ""}},
		{10, []string{"lorem", "", "", "", "", "", "ipsum"}, []string{"lorem", "", "", "", "", "", "ipsum"}},
	}

	for _, test := range tests {
		result := Truncate(test.max, test.strings...)
		if !reflect.DeepEqual(result, test.expected) {
			t.Fatalf("incorrect Truncate result - expected %#v, got %#v\n", test.expected, result)
		}
	}
}
