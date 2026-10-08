package calendar

import (
	"strings"
	"time"
	_ "time/tzdata" // embedded IANA database: TZIDs resolve on hosts without zoneinfo

	ical "github.com/emersion/go-ical"
)

// tzResolver maps a TZID parameter to an IANA location. Resolution order:
//
//  1. the TZID is itself an IANA name ("America/New_York");
//  2. a Windows zone name ("Eastern Standard Time"), case-insensitive;
//  3. the X-LIC-LOCATION of the feed's VTIMEZONE with that TZID;
//  4. the longest IANA suffix (up to three components) of a path-style TZID
//     ("/mozilla.org/20050126_1/America/New_York").
//
// Unresolvable TZIDs return nil and the value is treated as floating time.
// VTIMEZONE offset rules are never interpreted. TZIDs longer than
// maxTZIDLen are not looked up, and step 4 tries at most three suffixes
// (IANA names have at most three components), so a hostile feed cannot
// force unbounded zone-database lookups.
type tzResolver struct {
	licLocation map[string]string
	cache       map[string]*time.Location
}

func newTZResolver(cal *ical.Calendar) *tzResolver {
	r := &tzResolver{licLocation: map[string]string{}, cache: map[string]*time.Location{}}
	for _, child := range cal.Children {
		if child.Name != ical.CompTimezone {
			continue
		}
		tzid := child.Props.Get(ical.PropTimezoneID)
		lic := child.Props.Get("X-LIC-LOCATION")
		if tzid != nil && lic != nil {
			r.licLocation[strings.TrimSpace(tzid.Value)] = strings.TrimSpace(lic.Value)
		}
	}
	return r
}

func (r *tzResolver) resolve(tzid string) *time.Location {
	tzid = strings.TrimSpace(tzid)
	if loc, ok := r.cache[tzid]; ok {
		return loc
	}
	loc := r.lookup(tzid)
	r.cache[tzid] = loc
	return loc
}

// maxTZIDLen bounds TZIDs worth resolving; real ones are under 64 bytes.
const maxTZIDLen = 128

func (r *tzResolver) lookup(tzid string) *time.Location {
	if len(tzid) > maxTZIDLen {
		return nil
	}
	if loc := loadIANA(tzid); loc != nil {
		return loc
	}
	if name, ok := windowsZones[strings.ToLower(tzid)]; ok {
		return loadIANA(name)
	}
	if lic := r.licLocation[tzid]; lic != "" {
		if loc := loadIANA(lic); loc != nil {
			return loc
		}
	}
	parts := strings.Split(tzid, "/")
	for n := min(3, len(parts)-1); n >= 1; n-- { // longest suffix first
		if loc := loadIANA(strings.Join(parts[len(parts)-n:], "/")); loc != nil {
			return loc
		}
	}
	return nil
}

// loadIANA loads an IANA zone, refusing "Local" (the server's own zone is
// never what a feed means) and anything time.LoadLocation rejects.
func loadIANA(name string) *time.Location {
	if name == "" || strings.EqualFold(name, "Local") {
		return nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil
	}
	return loc
}

// windowsZones maps Windows time zone IDs (lower-cased) to IANA zones, per
// the CLDR windowsZones.xml territory "001" mappings, using current
// canonical IANA names. Outlook and Exchange exports use these as TZIDs.
var windowsZones = map[string]string{
	"dateline standard time":          "Etc/GMT+12",
	"utc-11":                          "Etc/GMT+11",
	"aleutian standard time":          "America/Adak",
	"hawaiian standard time":          "Pacific/Honolulu",
	"marquesas standard time":         "Pacific/Marquesas",
	"alaskan standard time":           "America/Anchorage",
	"utc-09":                          "Etc/GMT+9",
	"pacific standard time (mexico)":  "America/Tijuana",
	"utc-08":                          "Etc/GMT+8",
	"pacific standard time":           "America/Los_Angeles",
	"us mountain standard time":       "America/Phoenix",
	"mountain standard time (mexico)": "America/Mazatlan",
	"mountain standard time":          "America/Denver",
	"yukon standard time":             "America/Whitehorse",
	"central america standard time":   "America/Guatemala",
	"central standard time":           "America/Chicago",
	"easter island standard time":     "Pacific/Easter",
	"central standard time (mexico)":  "America/Mexico_City",
	"canada central standard time":    "America/Regina",
	"sa pacific standard time":        "America/Bogota",
	"eastern standard time (mexico)":  "America/Cancun",
	"eastern standard time":           "America/New_York",
	"haiti standard time":             "America/Port-au-Prince",
	"cuba standard time":              "America/Havana",
	"us eastern standard time":        "America/Indiana/Indianapolis",
	"turks and caicos standard time":  "America/Grand_Turk",
	"paraguay standard time":          "America/Asuncion",
	"atlantic standard time":          "America/Halifax",
	"venezuela standard time":         "America/Caracas",
	"central brazilian standard time": "America/Cuiaba",
	"sa western standard time":        "America/La_Paz",
	"pacific sa standard time":        "America/Santiago",
	"newfoundland standard time":      "America/St_Johns",
	"tocantins standard time":         "America/Araguaina",
	"e. south america standard time":  "America/Sao_Paulo",
	"sa eastern standard time":        "America/Cayenne",
	"argentina standard time":         "America/Argentina/Buenos_Aires",
	"greenland standard time":         "America/Nuuk",
	"montevideo standard time":        "America/Montevideo",
	"magallanes standard time":        "America/Punta_Arenas",
	"saint pierre standard time":      "America/Miquelon",
	"bahia standard time":             "America/Bahia",
	"utc-02":                          "Etc/GMT+2",
	"azores standard time":            "Atlantic/Azores",
	"cape verde standard time":        "Atlantic/Cape_Verde",
	"utc":                             "Etc/UTC",
	"gmt standard time":               "Europe/London",
	"greenwich standard time":         "Atlantic/Reykjavik",
	"sao tome standard time":          "Africa/Sao_Tome",
	"morocco standard time":           "Africa/Casablanca",
	"w. europe standard time":         "Europe/Berlin",
	"central europe standard time":    "Europe/Budapest",
	"romance standard time":           "Europe/Paris",
	"central european standard time":  "Europe/Warsaw",
	"w. central africa standard time": "Africa/Lagos",
	"jordan standard time":            "Asia/Amman",
	"gtb standard time":               "Europe/Bucharest",
	"middle east standard time":       "Asia/Beirut",
	"egypt standard time":             "Africa/Cairo",
	"e. europe standard time":         "Europe/Chisinau",
	"syria standard time":             "Asia/Damascus",
	"west bank standard time":         "Asia/Hebron",
	"south africa standard time":      "Africa/Johannesburg",
	"fle standard time":               "Europe/Kyiv",
	"israel standard time":            "Asia/Jerusalem",
	"south sudan standard time":       "Africa/Juba",
	"kaliningrad standard time":       "Europe/Kaliningrad",
	"sudan standard time":             "Africa/Khartoum",
	"libya standard time":             "Africa/Tripoli",
	"namibia standard time":           "Africa/Windhoek",
	"arabic standard time":            "Asia/Baghdad",
	"turkey standard time":            "Europe/Istanbul",
	"arab standard time":              "Asia/Riyadh",
	"belarus standard time":           "Europe/Minsk",
	"russian standard time":           "Europe/Moscow",
	"e. africa standard time":         "Africa/Nairobi",
	"volgograd standard time":         "Europe/Volgograd",
	"iran standard time":              "Asia/Tehran",
	"arabian standard time":           "Asia/Dubai",
	"astrakhan standard time":         "Europe/Astrakhan",
	"azerbaijan standard time":        "Asia/Baku",
	"russia time zone 3":              "Europe/Samara",
	"mauritius standard time":         "Indian/Mauritius",
	"saratov standard time":           "Europe/Saratov",
	"georgian standard time":          "Asia/Tbilisi",
	"caucasus standard time":          "Asia/Yerevan",
	"afghanistan standard time":       "Asia/Kabul",
	"west asia standard time":         "Asia/Tashkent",
	"ekaterinburg standard time":      "Asia/Yekaterinburg",
	"pakistan standard time":          "Asia/Karachi",
	"qyzylorda standard time":         "Asia/Qyzylorda",
	"india standard time":             "Asia/Kolkata",
	"sri lanka standard time":         "Asia/Colombo",
	"nepal standard time":             "Asia/Kathmandu",
	"central asia standard time":      "Asia/Bishkek",
	"bangladesh standard time":        "Asia/Dhaka",
	"omsk standard time":              "Asia/Omsk",
	"myanmar standard time":           "Asia/Yangon",
	"se asia standard time":           "Asia/Bangkok",
	"altai standard time":             "Asia/Barnaul",
	"w. mongolia standard time":       "Asia/Hovd",
	"north asia standard time":        "Asia/Krasnoyarsk",
	"n. central asia standard time":   "Asia/Novosibirsk",
	"tomsk standard time":             "Asia/Tomsk",
	"china standard time":             "Asia/Shanghai",
	"north asia east standard time":   "Asia/Irkutsk",
	"singapore standard time":         "Asia/Singapore",
	"w. australia standard time":      "Australia/Perth",
	"taipei standard time":            "Asia/Taipei",
	"ulaanbaatar standard time":       "Asia/Ulaanbaatar",
	"aus central w. standard time":    "Australia/Eucla",
	"transbaikal standard time":       "Asia/Chita",
	"tokyo standard time":             "Asia/Tokyo",
	"north korea standard time":       "Asia/Pyongyang",
	"korea standard time":             "Asia/Seoul",
	"yakutsk standard time":           "Asia/Yakutsk",
	"cen. australia standard time":    "Australia/Adelaide",
	"aus central standard time":       "Australia/Darwin",
	"e. australia standard time":      "Australia/Brisbane",
	"aus eastern standard time":       "Australia/Sydney",
	"west pacific standard time":      "Pacific/Port_Moresby",
	"tasmania standard time":          "Australia/Hobart",
	"vladivostok standard time":       "Asia/Vladivostok",
	"lord howe standard time":         "Australia/Lord_Howe",
	"bougainville standard time":      "Pacific/Bougainville",
	"russia time zone 10":             "Asia/Srednekolymsk",
	"magadan standard time":           "Asia/Magadan",
	"norfolk standard time":           "Pacific/Norfolk",
	"sakhalin standard time":          "Asia/Sakhalin",
	"central pacific standard time":   "Pacific/Guadalcanal",
	"russia time zone 11":             "Asia/Kamchatka",
	"new zealand standard time":       "Pacific/Auckland",
	"utc+12":                          "Etc/GMT-12",
	"fiji standard time":              "Pacific/Fiji",
	"chatham islands standard time":   "Pacific/Chatham",
	"utc+13":                          "Etc/GMT-13",
	"tonga standard time":             "Pacific/Tongatapu",
	"samoa standard time":             "Pacific/Apia",
	"line islands standard time":      "Pacific/Kiritimati",
}
