package civil

import (
	"database/sql/driver"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

// TestCivil is the package's single entry point; every suite is registered here.
func TestCivil(t *testing.T) {
	suite.Run(t, new(dateSuite))
}

type dateSuite struct {
	suite.Suite
}

func (s *dateSuite) TestParseAndString() {
	_, err := ParseDate("2026-02-29")
	s.Require().Error(err, "2026 is not a leap year")

	d, err := ParseDate("2024-02-29")
	s.Require().NoError(err)
	s.Equal(2024, d.Year)
	s.Equal(time.February, d.Month)
	s.Equal(29, d.Day)
	s.Equal("2024-02-29", d.String())
}

func (s *dateSuite) TestAddDaysAcrossMonthBoundary() {
	d := Date{Year: 2026, Month: time.January, Day: 31}
	s.Equal("2026-02-01", d.AddDays(1).String())
	s.Equal("2025-12-31", d.AddDays(-31).String())
}

func (s *dateSuite) TestDaysSinceIsInverseOfAddDays() {
	d := Date{Year: 2026, Month: time.March, Day: 15}
	for _, n := range []int{-400, -1, 0, 1, 400} {
		s.Equal(n, d.AddDays(n).DaysSince(d), "AddDays(%d) then DaysSince", n)
	}
}

func (s *dateSuite) TestOrdering() {
	a := Date{Year: 2026, Month: time.March, Day: 1}
	b := Date{Year: 2026, Month: time.March, Day: 2}

	s.True(a.Before(b))
	s.False(b.Before(a))
	s.True(b.After(a))
	s.False(a.After(a))

	s.True(a.Between(a, b), "inclusive at the lower bound")
	s.True(b.Between(a, b), "inclusive at the upper bound")
	s.False(Date{Year: 2026, Month: time.April, Day: 1}.Between(a, b))

	// Before/After must compare year and month, not just day.
	s.True(Date{Year: 2025, Month: time.December, Day: 31}.Before(Date{Year: 2026, Month: time.January, Day: 1}))
	s.True(Date{Year: 2026, Month: time.February, Day: 1}.Before(Date{Year: 2026, Month: time.March, Day: 1}))
}

func (s *dateSuite) TestIsZeroAndIsValid() {
	s.True(Date{}.IsZero())
	s.False(Date{Year: 2026, Month: time.March, Day: 1}.IsZero())
	s.True(Date{Year: 2026, Month: time.March, Day: 1}.IsValid())
	s.False(Date{Year: 2026, Month: time.February, Day: 30}.IsValid())
}

func (s *dateSuite) TestUnmarshalTextToleratesProviderJunk() {
	// Providers send these where a date belongs; they mean "absent", not "error".
	for _, in := range []string{"", "-", "None"} {
		var d Date
		s.Require().NoError(d.UnmarshalText([]byte(in)), "input %q", in)
		s.True(d.IsZero(), "input %q must leave the date zero", in)
	}

	var d Date
	s.Require().NoError(d.UnmarshalText([]byte("2026-03-01")))
	s.Equal("2026-03-01", d.String())

	s.Require().Error(d.UnmarshalText([]byte("01/03/2026")))
}

func (s *dateSuite) TestUnmarshalTextZeroesReceiverOnError() {
	// A Date reused across repeated UnmarshalText calls must not leak a
	// prior value forward when the new input fails to parse.
	d := Date{Year: 2020, Month: time.January, Day: 1}
	s.Require().Error(d.UnmarshalText([]byte("01/03/2026")))
	s.True(d.IsZero(), "receiver must be zeroed, not left at its prior value")
}

func (s *dateSuite) TestMarshalText() {
	d := Date{Year: 2026, Month: time.March, Day: 1}
	b, err := d.MarshalText()
	s.Require().NoError(err)
	s.Equal("2026-03-01", string(b))
}

func (s *dateSuite) TestScanAndValue() {
	var d Date

	s.Require().NoError(d.Scan("2026-03-01"))
	s.Equal("2026-03-01", d.String())

	s.Require().NoError(d.Scan([]byte("2026-03-02")))
	s.Equal("2026-03-02", d.String())

	s.Require().NoError(d.Scan(time.Date(2026, time.March, 3, 12, 0, 0, 0, time.UTC)))
	s.Equal("2026-03-03", d.String())

	s.Require().NoError(d.Scan(nil))
	s.True(d.IsZero())

	s.Require().Error(d.Scan(42))

	s.Require().Error(d.Scan("not-a-date"))
	s.Require().Error(d.Scan([]byte("not-a-date")))

	v, err := Date{Year: 2026, Month: time.March, Day: 1}.Value()
	s.Require().NoError(err)
	s.Equal(driver.Value(time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)), v)
}

func (s *dateSuite) TestScanZeroesReceiverOnParseError() {
	// A Date reused across a loop of rows.Scan(&d) must not leak a prior
	// row's value into the current one when the new value fails to parse.
	d := Date{Year: 2020, Month: time.January, Day: 1}
	s.Require().Error(d.Scan("not-a-date"))
	s.True(d.IsZero(), "receiver must be zeroed, not left at its prior value")

	d = Date{Year: 2020, Month: time.January, Day: 1}
	s.Require().Error(d.Scan([]byte("not-a-date")))
	s.True(d.IsZero(), "receiver must be zeroed, not left at its prior value")
}

func (s *dateSuite) TestDateOf() {
	d := DateOf(time.Date(2026, time.March, 1, 15, 30, 0, 0, time.UTC))
	s.Equal(Date{Year: 2026, Month: time.March, Day: 1}, d)
}

func (s *dateSuite) TestIn() {
	d := Date{Year: 2026, Month: time.March, Day: 1}
	got := d.In(time.UTC)
	s.Equal(time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC), got)
}

func (s *dateSuite) TestUsableAsMapKey() {
	// timeline() keys a map by Date; this is the property that makes that safe,
	// and the reason the domain does not use time.Time for calendar dates.
	m := map[Date]int{}
	m[Date{Year: 2026, Month: time.March, Day: 1}]++
	m[Date{Year: 2026, Month: time.March, Day: 1}]++
	s.Equal(2, m[Date{Year: 2026, Month: time.March, Day: 1}])
}
