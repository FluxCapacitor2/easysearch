package config

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

type Config struct {
	HTTP struct {
		Listen string
		Port   int16
	} `yaml:"http"`
	DB struct {
		Driver           string
		ConnectionString string `yaml:"connectionString"`
	} `yaml:"db"`
	Sources     []Source
	ResultsPage ResultsPageConfig `yaml:"resultsPage"`
}

type ResultsPageConfig struct {
	// Whether the search results page should be enabled.
	Enabled bool
	// An arbitrary HTML string that gets injected at the bottom of the `<head>` on the search results page.
	// Use this to add custom scripts or styles.
	CustomHTML string `yaml:"customHTML"`
}

type Source struct {
	// A unique identifier for this source. Used to distinguish between different sites if used with multiple tenants.
	ID string `yaml:"id"`
	// The URL of the site you want to build an index for.
	URL string `yaml:"url"`
	// The maximum amount of requests per minute that can be made to this source.
	Speed int32
	// The maximum amount of text content to index per page, in bytes
	SizeLimit int `yaml:"sizeLimit"`

	AllowedDomains []string `yaml:"allowedDomains"`

	MaxDepth int32 `yaml:"maxDepth"`

	// Configuration for content that has already been indexed.
	Refresh struct {
		// Whether content that has already been indexed should be refetched after a certain duration has passed.
		Enabled bool
		// The amount of time in between refreshes per URL, in days.
		MinAge int32 `yaml:"minAge"`
	}

	Embeddings struct {
		Enabled bool
		// The maximum number of requests per minute to the embeddings API
		Speed         int
		BatchSize     int    `yaml:"batchSize"`
		OpenAIBaseURL string `yaml:"openaiBaseUrl"`
		APIKey        string `yaml:"apiKey"`
		Model         string
		Dimensions    int
		ChunkSize     int `yaml:"chunkSize"`
		ChunkOverlap  int `yaml:"chunkOverlap"`
	}
}

var sourceIDPattern = regexp.MustCompile("^[a-zA-Z0-9_]+$")

func Read() (*Config, error) {

	data, err := os.ReadFile("./config.yml")
	if err != nil {
		return nil, err
	}

	config := &Config{}
	err = yaml.Unmarshal([]byte(data), config)

	if err != nil {
		return nil, err
	}

	// Validate the loaded configuration

	for _, src := range config.Sources {
		if !sourceIDPattern.MatchString(src.ID) {
			panic(fmt.Sprintf("Invalid source ID: %v. Source IDs may only contain alphanumeric characters and underscores.", src.ID))
		}
	}

	return config, nil
}
