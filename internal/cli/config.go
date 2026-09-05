package cli

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// Config is the CLI's entire configuration. Every field has a usable default
// except the AlphaVantage key, whose absence simply disables that provider.
type Config struct {
	DatabasePath string `env:"FINANCE_DB_PATH,expand"   envDefault:"${HOME}/.local/share/finance/finance.db"`
	AlphaKey     string `env:"FINANCE_ALPHAVANTAGE_KEY"`

	HTTPTimeout time.Duration `env:"FINANCE_HTTP_TIMEOUT" envDefault:"15s"`
}

// LoadConfig reads configuration from the environment, loading a .env file
// first if one is present; a missing .env is not an error, since every setting has a default.
func LoadConfig() (Config, error) {
	_ = godotenv.Load()

	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, fmt.Errorf("failed to read configuration: %w", err)
	}

	return cfg, nil
}
