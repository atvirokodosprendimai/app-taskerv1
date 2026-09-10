package tracking

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// Refusals for time logged by hand.
var (
	// ErrInvalidDuration reports a duration that does not parse, or is not
	// above zero.
	ErrInvalidDuration = errors.New("tracking: invalid duration")
	// ErrDurationTooLong reports a duration over [MaxLogged].
	ErrDurationTooLong = errors.New("tracking: duration too long")
	// ErrLoggedInFuture reports logged time that would end after now.
	ErrLoggedInFuture = errors.New("tracking: logged time ends in the future")
)

// MaxLogged is the longest stretch one logged entry may record.
//
// A day bounds "this is how long I spent on it". Anything longer is nearly
// always a slip — 45 typed where 45m was meant — and is better refused than
// added to a total whose cause nobody can later see.
const MaxLogged = 24 * time.Hour

// unitWords rewrites the unit words people type into the letters
// time.ParseDuration reads. Longer words come first, because at any one
// position the first listed match wins.
var unitWords = strings.NewReplacer(
	"hours", "h", "hour", "h", "hrs", "h", "hr", "h",
	"minutes", "m", "minute", "m", "mins", "m", "min", "m",
)

// ParseDuration reads a duration the way people write one down:
//
//	45m, 45 min      minutes
//	1h30m, 1h30      hours and minutes
//	1:30             hours:minutes
//	1.5, 1,5         hours, as a decimal
//	2                hours
//
// Spaces and letter case do not matter, and the result is rounded to the
// minute. Anything else, or a value that rounds to zero, is
// [ErrInvalidDuration]; a value over [MaxLogged] is [ErrDurationTooLong].
func ParseDuration(s string) (time.Duration, error) {
	s = unitWords.Replace(strings.ToLower(strings.Join(strings.Fields(s), "")))
	var d time.Duration
	switch {
	case s == "":
		return 0, ErrInvalidDuration
	case strings.Contains(s, ":"):
		h, m, _ := strings.Cut(s, ":")
		if !digitsOnly(h) || len(m) != 2 || !digitsOnly(m) {
			return 0, ErrInvalidDuration
		}
		hours, err := strconv.Atoi(h)
		minutes, _ := strconv.Atoi(m)
		if err != nil || minutes > 59 {
			return 0, ErrInvalidDuration
		}
		if time.Duration(hours) > MaxLogged/time.Hour {
			return 0, ErrDurationTooLong
		}
		d = time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute
	case strings.ContainsAny(s, "hm"):
		if strings.Trim(s, "0123456789.,hm") != "" {
			return 0, ErrInvalidDuration
		}
		s = strings.ReplaceAll(s, ",", ".")
		// "1h30" is how many people write an hour and a half; the parser needs
		// the trailing unit spelled out.
		if last := s[len(s)-1]; last >= '0' && last <= '9' && strings.Contains(s, "h") {
			s += "m"
		}
		parsed, err := time.ParseDuration(s)
		if err != nil {
			return 0, ErrInvalidDuration
		}
		d = parsed
	default:
		if strings.Trim(s, "0123456789.,") != "" {
			return 0, ErrInvalidDuration
		}
		hours, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
		if err != nil {
			return 0, ErrInvalidDuration
		}
		// Compared before converting, so an enormous number cannot overflow
		// into a small or negative duration on the way.
		if hours > MaxLogged.Hours() {
			return 0, ErrDurationTooLong
		}
		d = time.Duration(hours * float64(time.Hour))
	}
	d = d.Round(time.Minute)
	switch {
	case d <= 0:
		return 0, ErrInvalidDuration
	case d > MaxLogged:
		return 0, ErrDurationTooLong
	}
	return d, nil
}

// digitsOnly reports whether s is one or more ASCII digits.
func digitsOnly(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}
