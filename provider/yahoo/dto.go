package yahoo

import (
	"fmt"

	"github.com/shopspring/decimal"
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

// yahooError is the error envelope Yahoo Finance returns instead of a result.
type yahooError struct {
	Code        string `json:"code"`
	Description string `json:"description"`
}

// yahooDecimal is Yahoo Finance's wrapper around a number, carrying formatted
// strings alongside the raw value.
type yahooDecimal struct {
	Value         lenientDecimal `json:"raw"`
	Formatted     string         `json:"fmt"`
	LongFormatted string         `json:"longFmt"`
}

// decimal reports the wrapped value as a plain decimal.Decimal.
func (d yahooDecimal) decimal() decimal.Decimal {
	return d.Value.Decimal
}

// searchResponse is Yahoo Finance's response to a search request.
type searchResponse struct {
	Count  int `json:"count"`
	Quotes []struct {
		Exchange string `json:"exchange"`
		Type     string `json:"quoteType"`
		Symbol   string `json:"symbol"`
		Name     string `json:"shortname"`
		LongName string `json:"longname"`
	} `json:"quotes"`
}

// quoteResult is one instrument's data within a quoteResponse.
type quoteResult struct {
	AssetProfile struct {
		Industry string `json:"industry"`
		Sector   string `json:"sector"`
		URL      string `json:"website"`
	} `json:"assetProfile"`
	QuoteType struct {
		Symbol         string `json:"symbol"`
		Name           string `json:"shortName"`
		LongName       string `json:"longName"`
		Exchange       string `json:"exchange"`
		Type           string `json:"quoteType"`
		TZ             string `json:"timeZoneFullName"`
		TradeTimestamp int64  `json:"firstTradeDateEpochUtc"`
	} `json:"quoteType"`
	SummaryDetail struct {
		Currency                    string       `json:"currency"`
		Beta                        yahooDecimal `json:"beta"`
		TrailingPE                  yahooDecimal `json:"trailingPE"`
		ForwardPE                   yahooDecimal `json:"forwardPE"`
		PreviousDayClose            yahooDecimal `json:"previousClose"`
		DayOpen                     yahooDecimal `json:"open"`
		DayLow                      yahooDecimal `json:"dayLow"`
		DayHigh                     yahooDecimal `json:"dayHigh"`
		Volume                      yahooDecimal `json:"volume"`
		FiftyTwoWeekLow             yahooDecimal `json:"fiftyTwoWeekLow"`
		FiftyTwoWeekHigh            yahooDecimal `json:"fiftyTwoWeekHigh"`
		Bid                         yahooDecimal `json:"bid"`
		Ask                         yahooDecimal `json:"ask"`
		BidSize                     yahooDecimal `json:"bidSize"`
		AskSize                     yahooDecimal `json:"askSize"`
		MarketCap                   yahooDecimal `json:"marketCap"`
		DividendYield               yahooDecimal `json:"dividendYield"`
		FiveYearAvgDividendYield    yahooDecimal `json:"fiveYearAvgDividendYield"`
		TrailingAnnualDividendYield yahooDecimal `json:"trailingAnnualDividendYield"`
	} `json:"summaryDetail"`
	DefaultKeyStatistics struct {
		EnterpriseValue yahooDecimal `json:"enterpriseValue"`
		BookValue       yahooDecimal `json:"bookValue"`
		PriceToBook     yahooDecimal `json:"priceToBook"`
		Beta            yahooDecimal `json:"beta"`
	} `json:"defaultKeyStatistics"`
	FinancialData struct {
		Currency          string       `json:"financialCurrency"`
		CurrentPrice      yahooDecimal `json:"currentPrice"`
		TotalCash         yahooDecimal `json:"totalCash"`
		TotalDebt         yahooDecimal `json:"totalDebt"`
		TotalRevenue      yahooDecimal `json:"totalRevenue"`
		EBITDA            yahooDecimal `json:"ebitda"`
		DebtToEquity      yahooDecimal `json:"debtToEquity"`
		TotalCashPerShare yahooDecimal `json:"totalCashPerShare"`
		RevenuePerShare   yahooDecimal `json:"revenuePerShare"`
		ReturnOnAssets    yahooDecimal `json:"returnOnAssets"`
		ReturnOnEquity    yahooDecimal `json:"returnOnEquity"`
		FreeCashFlow      yahooDecimal `json:"freeCashflow"`
		OperatingCashFlow yahooDecimal `json:"operatingCashflow"`
	} `json:"financialData"`
	Price struct {
		Currency         string       `json:"currency"`
		PreviousDayClose yahooDecimal `json:"regularMarketPreviousClose"`
		DayOpen          yahooDecimal `json:"regularMarketOpen"`
		DayLow           yahooDecimal `json:"regularMarketDayLow"`
		DayHigh          yahooDecimal `json:"regularMarketDayHigh"`
		Price            yahooDecimal `json:"regularMarketPrice"`
		Volume           yahooDecimal `json:"regularMarketVolume"`
		Timestamp        int64        `json:"regularMarketTime"`
	} `json:"price"`
	FundPerformance struct {
		Overview struct {
			YtdTotalReturn        yahooDecimal `json:"ytdReturnPct"`
			OneYearTotalReturn    yahooDecimal `json:"oneYearTotalReturn"`
			ThreeYearsTotalReturn yahooDecimal `json:"threeYearTotalReturn"`
		} `json:"performanceOverview"`
		TrailingReturns struct {
			Ytd        yahooDecimal `json:"ytd"`
			OneMonth   yahooDecimal `json:"oneMonth"`
			ThreeMonth yahooDecimal `json:"threeMonth"`
			OneYear    yahooDecimal `json:"oneYear"`
			ThreeYear  yahooDecimal `json:"threeYear"`
			FiveYear   yahooDecimal `json:"fiveYear"`
			TenYear    yahooDecimal `json:"tenYear"`
		} `json:"trailingReturns"`
	} `json:"fundPerformance"`
}

// quoteResponse is Yahoo Finance's response to a quote request.
type quoteResponse struct {
	Inner struct {
		Result []quoteResult `json:"result"`
		Error  *yahooError   `json:"error"`
	} `json:"quoteSummary"`
}

// chartResult is one instrument's series within a chartResponse.
type chartResult struct {
	Meta struct {
		Symbol         string `json:"symbol"`
		Exchange       string `json:"exchangeName"`
		Type           string `json:"instrumentType"`
		Currency       string `json:"currency"`
		TZ             string `json:"exchangeTimezoneName"`
		TradeTimestamp int64  `json:"firstTradeDate"`
	} `json:"meta"`
	Timestamp []int64 `json:"timestamp"`
	Events    struct {
		Dividends map[string]struct {
			Amount    lenientDecimal `json:"amount"`
			Timestamp int64          `json:"date"`
		} `json:"dividends"`
		Splits map[string]struct {
			Numerator   int64  `json:"numerator"`
			Denominator int64  `json:"denominator"`
			SplitRatio  string `json:"splitRatio"`
			Timestamp   int64  `json:"date"`
		} `json:"splits"`
	} `json:"events"`
	Indicators struct {
		Quote    []quoteSeries `json:"quote"`
		AdjClose []*struct {
			AdjClose []lenientDecimal `json:"adjclose"`
		} `json:"adjclose"`
	} `json:"indicators"`
}

// quoteSeries is one instrument's OHLCV arrays within a chartResult. The
// arrays can decode successfully while disagreeing in length; use
// chartSeries to obtain one safely rather than indexing it directly.
type quoteSeries struct {
	Open   []lenientDecimal `json:"open"`
	Close  []lenientDecimal `json:"close"`
	Low    []lenientDecimal `json:"low"`
	High   []lenientDecimal `json:"high"`
	Volume []int64          `json:"volume"`
}

// chartResponse is Yahoo Finance's response to a chart request.
type chartResponse struct {
	Inner struct {
		Result []chartResult `json:"result"`
		Error  *yahooError   `json:"error"`
	} `json:"chart"`
}
