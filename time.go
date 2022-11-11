// Copyright 2022 Daniel Erat.
// All rights reserved.

package mpeg

import (
	"strconv"
	"time"

	"github.com/derat/taglib-go/taglib"
)

// Time contains a UTC timestamp (including date) read from an MP3 ID3v2 tag.
// Individual components of the timestamp may be unset.
type Time struct {
	t     time.Time // UTC
	parts timePart  // known parts of t
}

// These functions return different components of t.
// If the corresponding component is unset, -1 is returned.
func (t *Time) Year() int   { return t.getPart(yearPart, t.t.Year()) }
func (t *Time) Month() int  { return t.getPart(monthPart, int(t.t.Month())) }
func (t *Time) Day() int    { return t.getPart(dayPart, int(t.t.Day())) }
func (t *Time) Hour() int   { return t.getPart(hourPart, t.t.Hour()) }
func (t *Time) Minute() int { return t.getPart(minPart, int(t.t.Minute())) }
func (t *Time) Second() int { return t.getPart(secPart, int(t.t.Second())) }

// getPart returns val if p is set in t.parts or -1 otherwise.
func (t *Time) getPart(p timePart, val int) int {
	if t.parts&p != 0 {
		return val
	}
	return -1
}

// Time returns t's underlying time.Time object.
// Components that were unset in the original timestamp will have default values.
func (t *Time) Time() time.Time { return t.t }

// Empty returns true if none of the components in t are set.
func (t *Time) Empty() bool { return t.parts == 0 }

func (t *Time) String() string {
	if t.parts == 0 {
		return ""
	}
	var layout string
	add := func(part timePart, val, unk, sep string) {
		if t.parts&part != 0 {
			if layout != "" {
				layout = val + sep + layout
			} else {
				layout = val
			}
		} else if layout != "" {
			layout = unk + sep + layout
		}
	}
	add(secPart, "05", "", "")
	add(minPart, "04", "??", ":")
	add(hourPart, "15", "??", ":")
	add(dayPart, "02", "??", "T")
	add(monthPart, "01", "??", "-")
	add(yearPart, "2006", "????", "-")
	return t.t.Format(layout)
}

// timePart describes a component of a timestamp.
type timePart uint8

const (
	yearPart timePart = 1 << iota
	monthPart
	dayPart
	hourPart
	minPart
	secPart
)

// TimeType describes a timestamp stored in an ID3v2 tag.
type TimeType int

const (
	RecordingTime TimeType = iota
	OriginalReleaseTime
	ReleaseTime
)

// GetID3v2Time returns the requested timestamp from tag.
// ID3 v2.4 frames are preferred before falling back to v2.3 frames.
// If the requested timestamp time is not set, an empty time is returned.
// A non-nil error is only returned if an error was encountered while reading the tag.
func GetID3v2Time(tag taglib.GenericTag, typ TimeType) (Time, error) {
	fn := func(id string) (string, error) { return GetID3v2TextFrame(tag, id) }
	return getTimeInternal(fn, typ)
}

// getTimeInternal is a testable helper function wrapped by GetID3v2Time.
// getFrame should call mpeg.GetID3v2TextFrame.
func getTimeInternal(getFrame func(id string) (string, error), typ TimeType) (Time, error) {
	// Look for the appropriate v2.4 frame first:
	//   TDOR (Original release time): "timestamp describing when the original recording of the audio was released"
	//   TDRC (Recording time): "timestamp describing when the audio was recorded"
	//   TDRL (Release time): "timestamp describing when the audio was first released"
	var id string
	switch typ {
	case RecordingTime:
		id = "TDRC"
	case OriginalReleaseTime:
		id = "TDOR"
	case ReleaseTime:
		id = "TDRL"
	}
	if val, err := getFrame(id); err != nil {
		return Time{}, err
	} else if len(val) >= 4 {
		if t := ParseID3v24Time(val); !t.Empty() {
			return t, nil
		}
	}

	// Fall back to v2.3 frames:
	//   TYER (Year): "numeric string with a year of the recording (always 4 characters)"
	//   TDAT (Date): "numeric string in the DDMM format containing the date for the recording"
	//   TIME (Time): "numeric string in the HHMM format containing the time for the recording"
	//   TORY (Original release year): "the year when the original recording ... was released"
	var yearID, dateID, timeID string
	switch typ {
	case RecordingTime:
		yearID = "TYER"
		dateID = "TDAT"
		timeID = "TIME"
	case OriginalReleaseTime:
		yearID = "TORY"
	case ReleaseTime:
		// unsupported in v2.3
	}
	var year, date, time string
	for id, dst := range map[string]*string{yearID: &year, dateID: &date, timeID: &time} {
		if id == "" {
			continue
		}
		var err error
		if *dst, err = getFrame(id); err != nil {
			return Time{}, err
		}
	}
	if t := ParseID3v23Time(year, date, time); !t.Empty() {
		return t, nil
	}

	return Time{}, nil
}

// ParseID3v24Time parses the supplied ID3 v2.4 variable-precision timestamp,
// e.g. "2021", "2021-04-10", or "2021-04-10T15:06:47".
// See "4. ID3v2 frame overview" in https://id3.org/id3v2.4.0-structure.
// If the timestamp is empty or invalid, an empty Time object is returned.
func ParseID3v24Time(str string) Time {
	// "The timestamp fields are based on a subset of ISO 8601. When being as precise as possible
	// the format of a time string is yyyy-MM-ddTHH:mm:ss (year, "-", month, "-", day, "T", hour
	// (out of 24), ":", minutes, ":", seconds), but the precision may be reduced by removing as
	// many time indicators as wanted. Hence valid timestamps are yyyy, yyyy-MM, yyyy-MM-dd,
	// yyyy-MM-ddTHH, yyyy-MM-ddTHH:mm and yyyy-MM-ddTHH:mm:ss. All time stamps are UTC. For
	// durations, use the slash character as described in 8601, and for multiple non-contiguous
	// dates, use multiple strings, if allowed by the frame definition."
	for _, info := range []struct {
		layout string
		parts  timePart
	}{
		{"2006-01-02T15:04:05", yearPart | monthPart | dayPart | hourPart | minPart | secPart},
		{"2006-01-02T15:04", yearPart | monthPart | dayPart | hourPart | minPart},
		{"2006-01-02T15", yearPart | monthPart | dayPart | hourPart},
		{"2006-01-02", yearPart | monthPart | dayPart},
		{"2006-01", yearPart | monthPart},
		{"2006", yearPart},
	} {
		if t, err := time.Parse(info.layout, str); err == nil {
			return Time{t, info.parts}
		}
	}
	return Time{}
}

// ParseID3v23Time parses the supplied ID3 v2.3 timestamp as split across YYYY, DDMM, and HHMM strings.
// See "4.2.1. Text information frames - details" in https://id3.org/id3v2.3.0.
// Empty or invalid strings are omitted from the returned Time object.
// If none of the strings can be parsed, an empty Time is returned.
func ParseID3v23Time(yearStr, dateStr, timeStr string) Time {
	var parts timePart
	year, month, day := 1, 1, 1
	var hour, min, sec int

	// TYER
	//  The 'Year' frame is a numeric string with a year of the recording. This frames is always
	//  four characters long (until the year 10000)."
	if len(yearStr) == 4 {
		if y, err := strconv.Atoi(yearStr); err == nil && y > 0 {
			year = y
			parts |= yearPart
		}
	}
	// TDAT
	//  The 'Date' frame is a numeric string in the DDMM format containing the
	//  date for the recording. This field is always four characters long.
	if len(dateStr) == 4 {
		if m, err := strconv.Atoi(dateStr[:2]); err == nil && m >= 1 && m <= 12 {
			if d, err := strconv.Atoi(dateStr[2:]); err == nil && d >= 1 && d <= 31 {
				month, day = m, d
				parts |= monthPart | dayPart
			}
		}
	}
	// TIME
	//  The 'Time' frame is a numeric string in the HHMM format containing the time for the
	//  recording. This field is always four characters long.
	if len(timeStr) == 4 {
		if h, err := strconv.Atoi(timeStr[:2]); err == nil && h >= 0 && h <= 23 {
			if m, err := strconv.Atoi(timeStr[2:]); err == nil && m >= 0 && m <= 59 {
				hour, min = h, m
				parts |= hourPart | minPart
			}
		}
	}

	if parts == 0 {
		return Time{}
	}
	return Time{time.Date(year, time.Month(month), day, hour, min, sec, 0, time.UTC), parts}
}
