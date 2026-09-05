package finance

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"
)

// TestFinance is the package's single entry point; every suite is registered here.
func TestFinance(t *testing.T) {
	suite.Run(t, new(entitySuite))
	suite.Run(t, new(clientSuite))
	suite.Run(t, new(ratesSuite))
	suite.Run(t, new(alignSuite))
}

type entitySuite struct {
	suite.Suite
}

func (s *entitySuite) TestSentinelsAreDistinct() {
	s.Require().NotErrorIs(ErrNotFound, ErrRateLimited)
}

func (s *entitySuite) TestSentinelsSurviveWrapping() {
	// Every layer wraps with %w; errors.Is must still see through it.
	wrapped := fmt.Errorf("yahoo: %w", fmt.Errorf("quote: %w", ErrNotFound))
	s.Require().ErrorIs(wrapped, ErrNotFound)
	s.Require().NotErrorIs(wrapped, ErrRateLimited)
}

func (s *entitySuite) TestZeroValuesAreUsable() {
	// Providers leave fields unset when they supply nothing; the zero value must
	// be meaningful rather than a trap.
	var q Quote
	s.True(q.Price.IsZero())
	s.Zero(q.Volume)

	var h History
	s.Empty(h.Chart)
}
