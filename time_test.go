// Copyright 2022 Daniel Erat.
// All rights reserved.

package mpeg

import (
	"testing"
	"time"
)

func TestTime_Parts(t *testing.T) {
	tm := time.Date(2021, 4, 2, 23, 15, 57, 0, time.UTC)
	for _, tc := range []struct {
		parts timePart
		want  [6]int
	}{
		{yearPart, [6]int{2021, -1, -1, -1, -1, -1}},
		{yearPart | monthPart, [6]int{2021, 4, -1, -1, -1, -1}},
		{yearPart | monthPart | dayPart, [6]int{2021, 4, 2, -1, -1, -1}},
		{yearPart | monthPart | dayPart | hourPart, [6]int{2021, 4, 2, 23, -1, -1}},
		{yearPart | monthPart | dayPart | hourPart | minPart, [6]int{2021, 4, 2, 23, 15, -1}},
		{yearPart | monthPart | dayPart | hourPart | minPart | secPart, [6]int{2021, 4, 2, 23, 15, 57}},
		{0, [6]int{-1, -1, -1, -1, -1, -1}},
	} {
		tt := Time{tm, tc.parts}
		got := [6]int{tt.Year(), tt.Month(), tt.Day(), tt.Hour(), tt.Minute(), tt.Second()}
		if got != tc.want {
			t.Errorf("%v gave parts %v; want %v", tt.String(), got, tc.want)
		}
	}
}

func TestGetTimeInternal(t *testing.T) {
	const (
		rec  = RecordingTime
		orel = OriginalReleaseTime
		rel  = ReleaseTime

		v23rec  = "1992-03-23T14:35"
		v23orel = "1993"
		v24rec  = "1994-03-15"
		v24orel = "1994-04-01"
		v24rel  = "2004-12-13"
	)

	v23Frames := map[string]string{
		"TORY": v23orel,
		"TYER": "1992",
		"TDAT": "0323",
		"TIME": "1435",
	}
	v24Frames := map[string]string{
		"TDRC": v24rec,
		"TDOR": v24orel,
		"TDRL": v24rel,
	}

	allFrames := make(map[string]string, len(v24Frames)+len(v23Frames))
	for k, v := range v24Frames {
		allFrames[k] = v
	}
	for k, v := range v23Frames {
		allFrames[k] = v
	}

	for _, tc := range []struct {
		frames map[string]string
		typ    TimeType
		want   string
	}{
		{v23Frames, RecordingTime, v23rec},
		{v23Frames, OriginalReleaseTime, v23orel},
		{v23Frames, ReleaseTime, ""}, // unsupported by v2.3
		{v24Frames, RecordingTime, v24rec},
		{v24Frames, OriginalReleaseTime, v24orel},
		{v24Frames, ReleaseTime, v24rel},
		{allFrames, RecordingTime, v24rec},        // prefer v2.4
		{allFrames, OriginalReleaseTime, v24orel}, // prefer v2.4
		{allFrames, ReleaseTime, v24rel},          // prefer v2.4
		{nil, RecordingTime, ""},
	} {
		fn := func(id string) (string, error) { return tc.frames[id], nil }
		if tm, err := getTimeInternal(fn, tc.typ); err != nil {
			t.Errorf("getTimeInternal(%v, %v) failed: %v", tc.frames, tc.typ, err)
		} else if got := tm.String(); got != tc.want {
			t.Errorf("getTimeInternal(%v, %v) = %q; want %q", tc.frames, tc.typ, got, tc.want)
		}
	}
}

func TestParseID3v24Time(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"2022-04-25T23:14:06", "2022-04-25T23:14:06"},
		{"2022-04-25T23:14", "2022-04-25T23:14"},
		{"2022-04-25T23", "2022-04-25T23"},
		{"2022-04-25", "2022-04-25"},
		{"2022-04", "2022-04"},
		{"2022", "2022"},
		{"", ""},
		{"bogus", ""},
	} {
		if got := ParseID3v24Time(tc.in); got.String() != tc.want {
			t.Errorf("ParseID3v24Time(%q) = %q; want %q", tc.in, got.String(), tc.want)
		}
	}
}

func TestParseID3v23Time(t *testing.T) {
	for _, tc := range []struct{ year, date, time, want string }{
		{"2022", "0425", "2314", "2022-04-25T23:14"},
		{"2022", "0425", "", "2022-04-25"},
		{"2022", "", "", "2022"},
		{"", "", "2314", "????-??-??T23:14"},
		{"", "0425", "", "????-04-25"},
		{"2022", "", "2314", "2022-??-??T23:14"},
		{"", "", "", ""},
		{"bogus", "bogus", "bogus", ""},
	} {
		if got := ParseID3v23Time(tc.year, tc.date, tc.time); got.String() != tc.want {
			t.Errorf("ParseID3v23Time(%q, %q, %q) = %q; want %q",
				tc.year, tc.date, tc.time, got.String(), tc.want)
		}
	}
}
