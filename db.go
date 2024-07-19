package main

type Database interface {
	// Create necessary tables
	setup() error

	// Add a page to the search index.
	addDocument(source string, depth int32, url string, status QueueItemStatus, title string, description string, content string) (*Page, error)
	// Returns whether the given URL (or the URL's canonical) is indexed
	hasDocument(source string, url string) (*bool, error)

	// Run a fulltext search with the given query
	search(sources []string, query string, page uint32, pageSize uint32) ([]Result, *uint32, error)

	// Add an item to the crawl queue
	addToQueue(source string, urls []string, depth int32) error
	// Update the status of the item in the queue denoted by the specified url
	updateQueueEntry(source string, url string, status QueueItemStatus) error
	// Return the first item in the queue
	getFirstInQueue(source string) (*QueueItem, error)
	// Set the status of items that have been Processing for over 20 minutes to Pending and remove any Finished entries
	cleanQueue() error
	// Add pages older than `daysAgo` to the queue to be recrawled.
	queuePagesOlderThan(source string, daysAgo int32) error

	getCanonical(source string, url string) (*Canonical, error)
	setCanonical(source string, url string, canonical string) error
}

type Page struct {
	source      string
	url         string
	title       string
	description string
	content     string
	depth       int32
	crawledAt   string
	status      QueueItemStatus
}

type Result struct {
	Url         string  `json:"url"`
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
	source    string
	url       string
	addedAt   string
	updatedAt string
	depth     int32
	status    QueueItemStatus
}

type Canonical struct {
	url       string
	canonical string
	crawledAt string
}
