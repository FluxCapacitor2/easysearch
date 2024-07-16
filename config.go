package main

import (
	"os"

	"gopkg.in/yaml.v3"
)

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

	data, err := os.ReadFile("./config.yml")
	if err != nil {
		return nil, err
	}

	config := &config{}
	err = yaml.Unmarshal([]byte(data), config)

	if err != nil {
		return nil, err
	}

	return config, nil
}
