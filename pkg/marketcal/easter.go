package marketcal

import "time"

// easterSunday returns the date of Western (Gregorian) Easter Sunday for
// year, using the anonymous Gregorian computus (Meeus/Jones/Butcher). It is
// valid for every year of the Gregorian calendar (1583 onwards).
func easterSunday(year int) Date {
	a := year % 19 // position in the 19-year Metonic cycle
	b, c := year/100, year%100
	d, e := b/4, b%4
	f := (b + 8) / 25
	g := (b - f + 1) / 3              // lunar (Metonic) correction
	h := (19*a + b - d - g + 15) % 30 // epact-derived offset to the paschal full moon
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7 // days from the full moon to the next Sunday
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1
	return Date{Year: year, Month: time.Month(month), Day: day}
}
