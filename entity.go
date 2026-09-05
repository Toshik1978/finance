// Package finance provides a client for retrieving quotes, exchange rates and
// price history from financial data providers.
package finance

import (
	"github.com/shopspring/decimal"

	"github.com/Toshik1978/finance/civil"
)

// Exchanges, normalised from provider-specific codes.
const (
	ExchangeNASDAQ = "NASDAQ"
	ExchangeNYSE   = "NYSE"
	ExchangeCSE    = "CSE"
	ExchangeXETRA  = "XETRA"
)

// Instrument types, mirroring Yahoo's quoteType values.
const (
	TypeEquity     = "EQUITY"
	TypeETF        = "ETF"
	TypeIndex      = "INDEX"
	TypeMutualFund = "MUTUALFUND"
)

// Instrument identifies a tradable thing. It holds reference data only, no
// prices; Quote and History both embed it.
type Instrument struct {
	ISIN        string
	Ticker      string
	Type        string
	Name        string
	Description string
	Exchange    string
	Country     string
	Currency    string
}

// Quote is an instrument with its current price statistics and, where the
// provider supplies them, its fundamentals.
type Quote struct {
	Instrument

	// price statistics
	PreviousDayClose decimal.Decimal
	DayOpen          decimal.Decimal
	DayLow           decimal.Decimal
	DayHigh          decimal.Decimal
	Price            decimal.Decimal
	FiftyTwoWeekLow  decimal.Decimal
	FiftyTwoWeekHigh decimal.Decimal
	Volume           int64

	// fundamentals
	Industry      string
	Sector        string
	Beta          decimal.Decimal
	PriceToBook   decimal.Decimal
	TrailingPE    decimal.Decimal
	ForwardPE     decimal.Decimal
	DividendYield decimal.Decimal
}

// Bar is one OHLCV observation for a single calendar date.
type Bar struct {
	Date   civil.Date
	Open   decimal.Decimal
	Close  decimal.Decimal
	Low    decimal.Decimal
	High   decimal.Decimal
	Volume int64
}

// History is an instrument together with its price bars over a date range.
type History struct {
	Instrument

	Chart []Bar
}

// ExchangeRate is the value of one unit of From expressed in To, on Date.
type ExchangeRate struct {
	Date  civil.Date
	From  string
	To    string
	Price decimal.Decimal
}

// Match is one result of a ticker search.
type Match struct {
	Ticker string
	Type   string
	Name   string
}
