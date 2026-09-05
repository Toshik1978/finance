package main

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/finance"
	"github.com/Toshik1978/finance/internal/cli"
)

// TestFinanceCmd is the package's single entry point; every suite is registered here.
func TestFinanceCmd(t *testing.T) {
	suite.Run(t, new(rootSuite))
}

// stubClient satisfies cli.Client without exercising it; these tests only
// reach the root command's own dispatch, never a subcommand's Run.
type stubClient struct{}

func (stubClient) RateToday(context.Context, string, string) (decimal.Decimal, error) {
	return decimal.Decimal{}, nil
}

func (stubClient) Quote(context.Context, string) (finance.Quote, error) {
	return finance.Quote{}, nil
}

type rootSuite struct {
	suite.Suite
}

func (s *rootSuite) newRoot() *cobra.Command {
	cmd := root(stubClient{}, slog.New(slog.DiscardHandler))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	return cmd
}

func (s *rootSuite) TestUnrecognisedSubcommandIsAUsageError() {
	cmd := s.newRoot()
	cmd.SetArgs([]string{"frobnicate"})

	err := cmd.Execute()

	s.Require().Error(err)
	s.ErrorIs(err, cli.ErrUsage, "cobra's own usage error must map to the same sentinel main checks for")
}

func (s *rootSuite) TestNoArgsShowsHelpWithoutError() {
	cmd := s.newRoot()
	cmd.SetArgs(nil)

	s.Require().NoError(cmd.Execute())
}

func (s *rootSuite) TestUnknownFlagIsAUsageError() {
	cmd := s.newRoot()
	cmd.SetArgs([]string{"quote", "--bogus"})

	err := cmd.Execute()

	s.Require().Error(err)
	s.ErrorIs(err, cli.ErrUsage, "a flag-parsing error is a usage error like any other")
}
