package cli

import (
	"strconv"
	"strings"
)

// Thousands renders n with comma separators: 1234567 becomes "1,234,567".
// This replaces golang.org/x/text/message, which was pulled in for this alone.
func Thousands(n int64) string {
	s := strconv.FormatInt(n, 10)

	sign := ""
	if s[0] == '-' {
		sign, s = "-", s[1:]
	}

	head := len(s) % 3
	if head == 0 {
		head = 3
	}

	var out strings.Builder

	out.WriteString(s[:head])
	for i := head; i < len(s); i += 3 {
		out.WriteString(",")
		out.WriteString(s[i : i+3])
	}

	return sign + out.String()
}
