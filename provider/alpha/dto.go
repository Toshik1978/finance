package alpha

import (
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/Toshik1978/finance"
	"github.com/Toshik1978/finance/civil"
)

// lenientDecimal is a decimal that treats a provider's placeholders for "no
// value" — "", "-" and "None" — as zero, at the wire boundary only.
type lenientDecimal struct {
	decimal.Decimal
}

// isAbsent reports whether the raw bytes are one of the placeholders providers
// use for a missing number, with or without surrounding quotes.
func isAbsent(data []byte) bool {
	s := string(data)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}

	return s == "" || s == "-" || s == "None" || s == "null"
}

// UnmarshalJSON implements json.Unmarshaler.
func (d *lenientDecimal) UnmarshalJSON(data []byte) error {
	if isAbsent(data) {
		return nil
	}

	if err := d.Decimal.UnmarshalJSON(data); err != nil {
		return fmt.Errorf("failed to unmarshal decimal %q: %w", data, err)
	}

	return nil
}

// failure is AlphaVantage's in-band error channel. It answers HTTP 200 for
// everything and reports trouble in one of these three fields.
type failure struct {
	ErrorMessage string `json:"Error Message"`
	Note         string `json:"Note"`
	Information  string `json:"Information"`
}

// err translates the envelope into a sentinel-bearing error, or nil.
func (f failure) err() error {
	switch {
	case f.Note != "":
		return fmt.Errorf("alphavantage quota exhausted: %s: %w", f.Note, finance.ErrRateLimited)
	case f.Information != "":
		return fmt.Errorf("alphavantage refused the request: %s: %w", f.Information, finance.ErrRateLimited)
	case f.ErrorMessage != "":
		return fmt.Errorf("alphavantage rejected the request: %s: %w", f.ErrorMessage, finance.ErrNotFound)
	}

	return nil
}

// searchResponse is AlphaVantage's response to a SYMBOL_SEARCH query.
type searchResponse struct {
	failure

	BestMatches []struct {
		Symbol      string `json:"1. symbol"`
		Name        string `json:"2. name"`
		Type        string `json:"3. type"`
		Region      string `json:"4. region"`
		MarketOpen  string `json:"5. marketOpen"`
		MarketClose string `json:"6. marketClose"`
		Timezone    string `json:"7. timezone"`
		Currency    string `json:"8. currency"`
		MatchScore  string `json:"9. matchScore"`
	} `json:"bestMatches"`
}

// overviewResponse is AlphaVantage's response to an OVERVIEW query.
type overviewResponse struct {
	failure

	Symbol                     string         `json:"Symbol"`
	AssetType                  string         `json:"AssetType"`
	Name                       string         `json:"Name"`
	Description                string         `json:"Description"`
	CIK                        string         `json:"CIK"`
	Exchange                   string         `json:"Exchange"`
	Currency                   string         `json:"Currency"`
	Country                    string         `json:"Country"`
	Sector                     string         `json:"Sector"`
	Industry                   string         `json:"Industry"`
	Address                    string         `json:"Address"`
	FiscalYearEnd              string         `json:"FiscalYearEnd"`
	LatestQuarter              civil.Date     `json:"LatestQuarter"`
	MarketCapitalization       lenientDecimal `json:"MarketCapitalization"`
	EBITDA                     lenientDecimal `json:"EBITDA"`
	PERatio                    lenientDecimal `json:"PERatio"`
	PEGRatio                   lenientDecimal `json:"PEGRatio"`
	BookValue                  lenientDecimal `json:"BookValue"`
	DividendPerShare           lenientDecimal `json:"DividendPerShare"`
	DividendYield              lenientDecimal `json:"DividendYield"`
	EPS                        lenientDecimal `json:"EPS"`
	RevenuePerShareTTM         lenientDecimal `json:"RevenuePerShareTTM"`
	ProfitMargin               lenientDecimal `json:"ProfitMargin"`
	OperatingMarginTTM         lenientDecimal `json:"OperatingMarginTTM"`
	ReturnOnAssetsTTM          lenientDecimal `json:"ReturnOnAssetsTTM"`
	ReturnOnEquityTTM          lenientDecimal `json:"ReturnOnEquityTTM"`
	RevenueTTM                 lenientDecimal `json:"RevenueTTM"`
	GrossProfitTTM             lenientDecimal `json:"GrossProfitTTM"`
	DilutedEPSTTM              lenientDecimal `json:"DilutedEPSTTM"`
	QuarterlyEarningsGrowthYOY lenientDecimal `json:"QuarterlyEarningsGrowthYOY"`
	QuarterlyRevenueGrowthYOY  lenientDecimal `json:"QuarterlyRevenueGrowthYOY"`
	AnalystTargetPrice         lenientDecimal `json:"AnalystTargetPrice"`
	TrailingPE                 lenientDecimal `json:"TrailingPE"`
	ForwardPE                  lenientDecimal `json:"ForwardPE"`
	PriceToSalesRatioTTM       lenientDecimal `json:"PriceToSalesRatioTTM"`
	PriceToBookRatio           lenientDecimal `json:"PriceToBookRatio"`
	EVToRevenue                lenientDecimal `json:"EVToRevenue"`
	EVToEBITDA                 lenientDecimal `json:"EVToEBITDA"`
	Beta                       lenientDecimal `json:"Beta"`
	FiftyTwoWeekHigh           lenientDecimal `json:"52WeekHigh"`
	FiftyTwoWeekLow            lenientDecimal `json:"52WeekLow"`
	FiftyDayMovingAverage      lenientDecimal `json:"50DayMovingAverage"`
	TwoHundredDayMovingAverage lenientDecimal `json:"200DayMovingAverage"`
	SharesOutstanding          lenientDecimal `json:"SharesOutstanding"`
	DividendDate               civil.Date     `json:"DividendDate"`
	ExDividendDate             civil.Date     `json:"ExDividendDate"`
}

// quoteResponse is AlphaVantage's response to a GLOBAL_QUOTE query.
type quoteResponse struct {
	failure

	GlobalQuote struct {
		Symbol           string         `json:"01. symbol"`
		Open             lenientDecimal `json:"02. open"`
		High             lenientDecimal `json:"03. high"`
		Low              lenientDecimal `json:"04. low"`
		Price            lenientDecimal `json:"05. price"`
		Volume           lenientDecimal `json:"06. volume"`
		LatestTradingDay civil.Date     `json:"07. latest trading day"`
		PreviousClose    lenientDecimal `json:"08. previous close"`
		Change           lenientDecimal `json:"09. change"`
		ChangePercent    string         `json:"10. change percent"`
	} `json:"Global Quote"`
}

// chartResponse is AlphaVantage's response to a TIME_SERIES_DAILY query.
type chartResponse struct {
	failure

	MetaData struct {
		Information   string `json:"1. Information"`
		Symbol        string `json:"2. Symbol"`
		LastRefreshed string `json:"3. Last Refreshed"`
		OutputSize    string `json:"4. Output Size"`
		TimeZone      string `json:"5. Time Zone"`
	} `json:"Meta Data"`
	TimeSeriesDaily map[civil.Date]struct {
		Open   lenientDecimal `json:"1. open"`
		High   lenientDecimal `json:"2. high"`
		Low    lenientDecimal `json:"3. low"`
		Close  lenientDecimal `json:"4. close"`
		Volume lenientDecimal `json:"5. volume"`
	} `json:"Time Series (Daily)"`
}

// rateResponse is AlphaVantage's response to a CURRENCY_EXCHANGE_RATE query.
type rateResponse struct {
	failure

	RealtimeCurrencyExchangeRate struct {
		FromCurrencyCode string         `json:"1. From_Currency Code"`
		FromCurrencyName string         `json:"2. From_Currency Name"`
		ToCurrencyCode   string         `json:"3. To_Currency Code"`
		ToCurrencyName   string         `json:"4. To_Currency Name"`
		ExchangeRate     lenientDecimal `json:"5. Exchange Rate"`
		LastRefreshed    string         `json:"6. Last Refreshed"`
		TimeZone         string         `json:"7. Time Zone"`
		BidPrice         lenientDecimal `json:"8. Bid Price"`
		AskPrice         lenientDecimal `json:"9. Ask Price"`
	} `json:"Realtime Currency Exchange Rate"`
}

// rateChartResponse is AlphaVantage's response to an FX_DAILY query.
type rateChartResponse struct {
	failure

	MetaData struct {
		Information   string `json:"1. Information"`
		FromSymbol    string `json:"2. From Symbol"`
		ToSymbol      string `json:"3. To Symbol"`
		OutputSize    string `json:"4. Output Size"`
		LastRefreshed string `json:"5. Last Refreshed"`
		TimeZone      string `json:"6. Time Zone"`
	} `json:"Meta Data"`
	TimeSeriesFXDaily map[civil.Date]struct {
		Open  lenientDecimal `json:"1. open"`
		High  lenientDecimal `json:"2. high"`
		Low   lenientDecimal `json:"3. low"`
		Close lenientDecimal `json:"4. close"`
	} `json:"Time Series FX (Daily)"`
}
