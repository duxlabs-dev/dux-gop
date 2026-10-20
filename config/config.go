package config

import (
	"log"

	"github.com/caarlos0/env/v11"
	"github.com/duxlabs-dev/dux-go/paths"
)

var appPaths = paths.New("dgop")

type Config struct {
	ApiPort string `env:"API_PORT" envDefault:":63484"` // Default port for the API server
}

// Parse environment variables into a Config struct
func NewConfig() *Config {
	cfg := Config{}
	if err := env.Parse(&cfg); err != nil {
		log.Fatal("Error parsing environment", "err", err)
	}

	return &cfg
}
