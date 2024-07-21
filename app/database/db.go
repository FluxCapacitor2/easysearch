package database

type Database interface {
	// Create necessary tables
	Setup() error

	// Add a page to the search index.
	AddDocument(source string, depth int32, url string, status QueueItemStatus, title string, description string, content string) (*Page, error)
	// Returns whether the given URL (or the URL's canonical) is indexed
	HasDocument(source string, url string) (*bool, error)

	// Run a fulltext search with the given query
	Search(sources []string, query string, page uint32, pageSize uint32) ([]Result, *uint32, error)

	// Add an item to the crawl queue
	AddToQueue(source string, urls []string, depth int32) error
	// Update the status of the item in the queue denoted by the specified url
	UpdateQueueEntry(source string, url string, status QueueItemStatus) error
	// Return the first item in the queue
	GetFirstInQueue(source string) (*QueueItem, error)
	// Set the status of items that have been Processing for over 20 minutes to Pending and remove any Finished entries
	CleanQueue() error
	// Add pages older than `daysAgo` to the queue to be recrawled.
	QueuePagesOlderThan(source string, daysAgo int32) error

	GetCanonical(source string, url string) (*Canonical, error)
	SetCanonical(source string, url string, canonical string) error
}

type Page struct {
	Source      string
	URL         string
	Title       string
	Description string
	Content     string
	Depth       int32
	CrawledAt   string
	Status      QueueItemStatus
}

type Result struct {
	URL         string  `json:"url"`
	Title       []Match `json:"title"`
	Description []Match `json:"description"`
	Content     []Match `json:"content"`
	Rank        float64 `json:"rank"`
}

type Match struct {
	Highlighted bool   `json:"highlighted"`
	Content     string `json:"content"`
}

type QueueItemStatus int8

const (
	Pending QueueItemStatus = iota
	Processing
	Finished
	Error

	// Used in "pages" tables to record that a page _has_ been indexed, but doesn't have any content.
	// For example, a sitemap or RSS feed would be added as Unindexable with an empty title and description.
	Unindexable
)

type QueueItem struct {
	Source    string
	URL       string
	AddedAt   string
	UpdatedAt string
	Depth     int32
	Status    QueueItemStatus
}

type Canonical struct {
	Original  string
	Canonical string
	CrawledAt string
}
