package main

import (
	"gopkg.in/yaml.v3"
)

var data = `
db:
  driver: sqlite
  connectionString: "./data.db"
  #connectionString: ":memory:"

http:
  listen: localhost
  port: 8080

sources:
  - id: brendan
    url: https://www.bswanson.dev
    allowedDomains:
      - "www.bswanson.dev"
      - "webquiz.bswanson.dev"
      - "favicon.bswanson.dev"
      - "todo.bswanson.dev"
    maxDepth: 100
    speed: 30
    refresh:
      enabled: true
      minAge: 7
  - id: pitchlabs
    url: https://www.pitchlabs.org
    allowedDomains:
      - "www.pitchlabs.org"
    maxDepth: 50
    speed: 60
    refresh:
      enabled: true
      minAge: 7 # Refresh content weekly (every 7 days)
`

type config struct {
	Http struct {
		Listen string
		Port   int16
	}
	Db struct {
		Driver           string
		ConnectionString string `yaml:"connectionString"`
	}
	Sources []Source
}

type Source struct {
	// A unique identifier for this source. Used to distinguish between different sites if used with multiple tenants.
	Id string
	// The URL of the site you want to build an index for.
	Url string
	// The maximum amount of requests per minute that can be made to this source.
	Speed int32

	AllowedDomains []string `yaml:"allowedDomains"`

	MaxDepth int32 `yaml:"maxDepth"`

	// Configuration for content that has already been indexed.
	Refresh struct {
		// Whether content that has already been indexed should be refetched after a certain duration has passed.
		Enabled bool
		// The amount of time in between refreshes per URL, in days.
		MinAge int32 `yaml:"minAge"`
	}
}

func readConfig() (*config, error) {

	config := &config{}
	err := yaml.Unmarshal([]byte(data), config)

	if err != nil {
		return nil, err
	}

	return config, nil
}
