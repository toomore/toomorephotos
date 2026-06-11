package db

import (
	"regexp"
	"time"
)

var (
	takenRealAnyExpr     = regexp.MustCompile(`(\d{4})[-/](\d{1,2})[-/](\d{1,2})`)
	takenRealLeadingExpr = regexp.MustCompile(`^\s*(\d{4})[-/](\d{1,2})[-/](\d{1,2})`)
)

// ExtractTakenReal pulls the real capture date that Toomore writes into the
// photo description (format YYYY/MM/DD or YYYY-MM-DD). Short captions put the
// date at the very start of the text; film descriptions put it on the last
// line after the roll-frame code (e.g. 0991-0019).
//
// Rule: a date at the very start wins (caption format); otherwise the LAST
// valid date wins (film format — this ignores dates merely mentioned in the
// middle of the narrative). Years are constrained to 1990–2030 and the date
// must be a real calendar date, which also rejects roll codes like 4942-0011.
// Returns the date as "2006-01-02", or "" when no usable date is present.
func ExtractTakenReal(description string) string {
	if m := takenRealLeadingExpr.FindStringSubmatch(description); m != nil {
		if d, ok := buildDate(m[1], m[2], m[3]); ok {
			return d
		}
	}
	all := takenRealAnyExpr.FindAllStringSubmatch(description, -1)
	for i := len(all) - 1; i >= 0; i-- {
		if d, ok := buildDate(all[i][1], all[i][2], all[i][3]); ok {
			return d
		}
	}
	return ""
}

func buildDate(y, m, d string) (string, bool) {
	t, err := time.Parse("2006-1-2", y+"-"+m+"-"+d)
	if err != nil {
		return "", false
	}
	if t.Year() < 1990 || t.Year() > 2030 {
		return "", false
	}
	return t.Format("2006-01-02"), true
}
