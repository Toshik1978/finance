package cli

import (
	"time"

	"github.com/stretchr/testify/suite"
)

type configSuite struct{ suite.Suite }

func (s *configSuite) TestDefaultsApplyWithNoEnvironment() {
	s.T().Setenv("HOME", "/home/tester")
	s.T().Setenv("FINANCE_DB_PATH", "")
	s.T().Setenv("FINANCE_ALPHAVANTAGE_KEY", "")
	s.T().Setenv("FINANCE_HTTP_TIMEOUT", "")

	cfg, err := LoadConfig()

	s.Require().NoError(err)
	s.Equal("/home/tester/.local/share/finance/finance.db", cfg.DatabasePath)
	s.Equal(15*time.Second, cfg.HTTPTimeout)
	s.Empty(cfg.AlphaKey)
}

func (s *configSuite) TestEnvironmentOverridesDefaults() {
	s.T().Setenv("FINANCE_DB_PATH", "/tmp/custom.db")
	s.T().Setenv("FINANCE_ALPHAVANTAGE_KEY", "key-123")
	s.T().Setenv("FINANCE_HTTP_TIMEOUT", "30s")

	cfg, err := LoadConfig()

	s.Require().NoError(err)
	s.Equal("/tmp/custom.db", cfg.DatabasePath)
	s.Equal("key-123", cfg.AlphaKey)
	s.Equal(30*time.Second, cfg.HTTPTimeout)
}

func (s *configSuite) TestMalformedTimeoutIsAnError() {
	s.T().Setenv("FINANCE_HTTP_TIMEOUT", "soon")

	_, err := LoadConfig()

	s.Require().Error(err)
}
