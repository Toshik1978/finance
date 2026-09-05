// Copyright 2016 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// This file was copied from cloud.google.com/go/civil and modified: the Time
// and DateTime types were removed, and Between, IsZero, Scan, Value and a
// lenient UnmarshalText (tolerating "", "-" and "None") were added.

// Package civil implements types for civil time, a time-zone-independent
// representation of time that follows the rules of the proleptic
// Gregorian calendar with exactly 24-hour days, 60-minute hours, and 60-second
// minutes.
//
// Because they lack location information, these types do not represent unique
// moments or intervals of time. Use time.Time for that purpose.
package civil

import (
	"database/sql/driver"
	"fmt"
	"time"
)

// A Date represents a date (year, month, day).
//
// This type does not include location information, and therefore does not
// describe a unique 24-hour timespan.
//
//nolint:recvcheck // Scan and UnmarshalText need a pointer receiver; the rest are value methods by design.
type Date struct {
	Year  int        // Year (e.g., 2014).
	Month time.Month // Month of the year (January = 1, ...).
	Day   int        // Day of the month, starting at 1.
}

// DateOf returns the Date in which a time occurs in that time's location.
func DateOf(t time.Time) Date {
	var d Date
	d.Year, d.Month, d.Day = t.Date()

	return d
}

// ParseDate parses a string in RFC3339 full-date format and returns the date value it represents.
func ParseDate(s string) (Date, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return Date{}, fmt.Errorf("parse date %q: %w", s, err)
	}

	return DateOf(t), nil
}

// String returns the date in RFC3339 full-date format.
func (d Date) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day)
}

// IsValid reports whether the date is valid.
func (d Date) IsValid() bool {
	return DateOf(d.In(time.UTC)) == d
}

// In returns the time corresponding to time 00:00:00 of the date in the location.
//
// In is always consistent with time.Date, even when time.Date returns a time
// on a different day. For example, if loc is America/Indiana/Vincennes, then both
// time.Date(1955, time.May, 1, 0, 0, 0, 0, loc)
// and
// civil.Date{Year: 1955, Month: time.May, Day: 1}.In(loc)
// return 23:00:00 on April 30, 1955.
//
// In panics if loc is nil.
func (d Date) In(loc *time.Location) time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, loc)
}

// AddDays returns the date that is n days in the future.
// n can also be negative to go into the past.
func (d Date) AddDays(n int) Date {
	return DateOf(d.In(time.UTC).AddDate(0, 0, n))
}

// DaysSince returns the signed number of days between the date and s, not including the end day.
// This is the inverse operation to AddDays.
func (d Date) DaysSince(s Date) int {
	// We convert to Unix time so we do not have to worry about leap seconds:
	// Unix time increases by exactly 86400 seconds per day.
	deltaUnix := d.In(time.UTC).Unix() - s.In(time.UTC).Unix()

	return int(deltaUnix / 86400)
}

// Before reports whether d occurs before d2.
func (d Date) Before(d2 Date) bool {
	if d.Year != d2.Year {
		return d.Year < d2.Year
	}

	if d.Month != d2.Month {
		return d.Month < d2.Month
	}

	return d.Day < d2.Day
}

// After reports whether d occurs after d2.
func (d Date) After(d2 Date) bool {
	return d2.Before(d)
}

// Between reports whether d is between d1 and d2, inclusive of both bounds.
func (d Date) Between(d1, d2 Date) bool {
	return d == d1 || d == d2 || (d.After(d1) && d.Before(d2))
}

// IsZero reports whether date fields are set to their default value.
func (d Date) IsZero() bool {
	return (d.Year == 0) && (int(d.Month) == 0) && (d.Day == 0)
}

// MarshalText implements the encoding.TextMarshaler interface.
// The output is the result of d.String().
func (d Date) MarshalText() ([]byte, error) {
	return []byte(d.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler. "", "-" and "None" mean absent and leave d
// unchanged; any other parse failure zeroes d, matching ParseDate.
func (d *Date) UnmarshalText(data []byte) error {
	s := string(data)
	if s == "" || s == "-" || s == "None" {
		return nil
	}

	parsed, err := ParseDate(s)
	*d = parsed

	return err
}

// Scan implements the sql.Scanner interface. On a parse failure d is zeroed, matching ParseDate's
// own zero-value-on-error contract.
func (d *Date) Scan(value any) error {
	if value == nil {
		*d = Date{}
		return nil
	}

	switch src := value.(type) {
	case string:
		parsed, err := ParseDate(src)
		*d = parsed

		return err
	case []byte:
		parsed, err := ParseDate(string(src))
		*d = parsed

		return err
	case time.Time:
		*d = DateOf(src)
		return nil
	}

	return fmt.Errorf("civil: cannot scan %T into Date", value)
}

// Value implements the driver.Value interface.
func (d Date) Value() (driver.Value, error) {
	return d.In(time.UTC), nil
}
