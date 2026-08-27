package opfor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"testing"
	"time"
	"unicode/utf8"
)

const simpleDateFormatProbeSource = `
println(formatDate(0L, "G y yy Y YY M MM MMM MMMM L LL LLL LLLL w W D d F E EEEE u a H HH k kk K KK h hh m mm s ss S SS SSS SSSS z Z X XX XXX"));
println(formatDate(1514678400000L, "yyyy-YYYY-w-u"));
println(formatDate(123L, "S SS SSS SSSS"));
println(formatDate(0L, "yyyy\u0027Q\u0027MM \u0027at\u0027 HH:mm"));
println(parseDate("yyyy-MM-dd", "2024-02-30"));
println(parseDate("yyyy-MM-dd", "2024-01-01suffix"));
println(parseDate("MM-dd", "02-03"));
println(parseDate("D", "60"));
println(parseDate("yyyy-MM-dd HH:mm:ss.SSS Z", "1970-01-01 24:00:00.000 +0000"));
println(parseDate("yy-MM-dd", "24-01-01"));
println(parseDate("dd MMMM yyyy", "3 February 2024tail"));
println(parseDate("YYYY-w-u", "2018-1-7"));
println(parseDate("yyyy-MM-dd h:mm a", "1970-01-01 12:05 PM"));
println(parseDate("yyyy-MM-dd HH:mm X", "1970-01-01 00:00 +02"));
println(parseDate("yyyy-MM-dd HH:mm XXX", "1970-01-01 00:00 -02:30"));
println(formatDate(1546214400000L, "yyyy-YYYY-w-W-D-F-u"));
`

const simpleDateFormatProbeOutput = "AD 1970 70 1970 70 1 01 Jan January 1 01 Jan January 1 1 1 1 1 Thu Thursday 4 AM 0 00 24 24 0 00 12 12 0 00 0 00 0 00 000 0000 UTC +0000 Z Z Z\n" +
	"2017-2018-1-7\n" +
	"123 123 123 0123\n" +
	"1970Q01 at 00:00\n" +
	"1709251200000\n" +
	"1704067200000\n" +
	"2851200000\n" +
	"5097600000\n" +
	"86400000\n" +
	"1704067200000\n" +
	"1706918400000\n" +
	"1514678400000\n" +
	"43500000\n" +
	"-7200000\n" +
	"9000000\n" +
	"2018-2019-1-6-365-5-1\n"

func TestSimpleDateFormatFieldsLeniencyAndPrefixExactOutput(t *testing.T) {
	got := runSimpleDateFormatProbe(t)
	if !bytes.Equal(got, []byte(simpleDateFormatProbeOutput)) {
		t.Fatalf("SimpleDateFormat output mismatch\nwant:\n%sgot:\n%s", simpleDateFormatProbeOutput, got)
	}
}

// TestSimpleDateFormatOfficialJARDifferential is opt-in because the official
// BSD Sleep JAR is supplied separately. Locale and time zone are pinned so
// SimpleDateFormat is a deterministic oracle rather than a machine setting.
func TestSimpleDateFormatOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	want, err := osexec.Command(
		java,
		"-Duser.timezone=UTC", "-Duser.language=en", "-Duser.country=US",
		"-jar", jar, "-e", simpleDateFormatProbeSource,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep date probe: %v\n%s", err, want)
	}
	got := runSimpleDateFormatProbe(t)
	if !bytes.Equal(got, want) {
		t.Fatalf("official Sleep date output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runSimpleDateFormatProbe(t *testing.T) []byte {
	t.Helper()
	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	return runSimpleDateFormatSource(t, "simple-date-format.sl", simpleDateFormatProbeSource, now)
}

func runSimpleDateFormatSource(t *testing.T, name, source string, now time.Time) []byte {
	t.Helper()
	var output bytes.Buffer
	runtime, err := New(
		WithClock(ClockFunc(func() time.Time { return now })),
		WithStdout(&output), WithStderr(&output),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	if _, err := runtime.Eval(context.Background(), name, source); err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), output.Bytes()...)
}

func TestSimpleDateFormatRejectsInvalidPatternAndInput(t *testing.T) {
	if _, err := formatJavaDate(time.Unix(0, 0).UTC(), "yyyy-QQ"); err == nil {
		t.Fatal("formatJavaDate accepted unsupported quarter field")
	}
	if _, err := parseJavaDate("not-a-date", "yyyy-MM-dd", time.Unix(0, 0).UTC()); err == nil {
		t.Fatal("parseJavaDate accepted a non-date")
	}
}

func TestSimpleDateFormatTwoDigitYearUsesExactRollingWindow(t *testing.T) {
	now := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		pattern string
		input   string
		want    time.Time
	}{
		{
			name:    "previous date rolls forward",
			pattern: "yy-MM-dd",
			input:   "46-08-24",
			want:    time.Date(2046, time.August, 24, 0, 0, 0, 0, time.UTC),
		},
		{
			name:    "following date stays in pivot century",
			pattern: "yy-MM-dd",
			input:   "46-08-26",
			want:    time.Date(1946, time.August, 26, 0, 0, 0, 0, time.UTC),
		},
		{
			name:    "instant immediately before pivot rolls forward",
			pattern: "yy-MM-dd HH:mm:ss.SSS",
			input:   "46-08-25 11:59:59.999",
			want:    time.Date(2046, time.August, 25, 11, 59, 59, 999*int(time.Millisecond), time.UTC),
		},
		{
			name:    "instant equal to pivot stays in pivot century",
			pattern: "yy-MM-dd HH:mm:ss.SSS",
			input:   "46-08-25 12:00:00.000",
			want:    time.Date(1946, time.August, 25, 12, 0, 0, 0, time.UTC),
		},
		{
			name:    "explicit zone compares absolute instants",
			pattern: "yy-MM-dd HH:mm XXX",
			input:   "46-08-25 14:00 +02:00",
			want:    time.Date(1946, time.August, 25, 12, 0, 0, 0, time.UTC),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseJavaDate(test.input, test.pattern, now)
			if err != nil {
				t.Fatal(err)
			}
			if !got.Equal(test.want) {
				t.Fatalf("parseJavaDate(%q, %q) = %s, want %s", test.input, test.pattern, got, test.want)
			}
		})
	}
}

func TestSimpleDateFormatNumericTimeZonePatternWidths(t *testing.T) {
	now := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	const inputPrefix = "1970-01-01 00:00 "
	const patternPrefix = "yyyy-MM-dd HH:mm "
	accepted := []struct {
		pattern string
		zone    string
		millis  int64
	}{
		{pattern: "X", zone: "+02", millis: -7_200_000},
		{pattern: "X", zone: "+0230", millis: -7_200_000},
		{pattern: "X", zone: "+02:30", millis: -7_200_000},
		{pattern: "XX", zone: "+0230", millis: -9_000_000},
		{pattern: "XXX", zone: "+02:30", millis: -9_000_000},
		{pattern: "Z", zone: "+0230", millis: -9_000_000},
	}
	for _, test := range accepted {
		t.Run(test.pattern+"/"+test.zone, func(t *testing.T) {
			got, err := parseJavaDate(inputPrefix+test.zone, patternPrefix+test.pattern, now)
			if err != nil {
				t.Fatal(err)
			}
			if got.UnixMilli() != test.millis {
				t.Fatalf("UnixMilli = %d, want %d", got.UnixMilli(), test.millis)
			}
		})
	}

	rejected := []struct {
		pattern string
		zone    string
	}{
		{pattern: "XX", zone: "+02"},
		{pattern: "XX", zone: "+02:30"},
		{pattern: "XXX", zone: "+02"},
		{pattern: "XXX", zone: "+0230"},
		{pattern: "Z", zone: "+02"},
		{pattern: "Z", zone: "+02:30"},
	}
	for _, test := range rejected {
		t.Run(test.pattern+"/"+test.zone, func(t *testing.T) {
			if _, err := parseJavaDate(inputPrefix+test.zone, patternPrefix+test.pattern, now); err == nil {
				t.Fatalf("parseJavaDate accepted %s offset %q", test.pattern, test.zone)
			}
		})
	}
}

func TestSimpleDateFormatLosAngelesDSTWeeksAndZoneNames(t *testing.T) {
	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	instant := time.UnixMilli(1_719_835_200_000).In(location)
	got, err := formatJavaDate(instant, "yyyy-MM-dd YYYY-w-W D-u z zzzz G GGGG")
	if err != nil {
		t.Fatal(err)
	}
	const want = "2024-07-01 2024-27-1 183-1 PDT Pacific Daylight Time AD AD"
	if got != want {
		t.Fatalf("Los Angeles date fields = %q, want %q", got, want)
	}

	march := time.Date(2024, time.March, 17, 12, 0, 0, 0, location)
	if got := formatJavaDateField(march, 'w', 1) + "-" + formatJavaDateField(march, 'W', 1); got != "12-4" {
		t.Fatalf("March DST-crossing week fields = %q, want %q", got, "12-4")
	}
}

func TestSimpleDateFormatLocalLongTimeZoneNameFallback(t *testing.T) {
	tests := []struct {
		name         string
		abbreviation string
		offset       int
		want         string
	}{
		{
			name:         "Pacific daylight",
			abbreviation: "PDT",
			offset:       -7 * 60 * 60,
			want:         "Pacific Daylight Time",
		},
		{
			name:         "Pacific standard",
			abbreviation: "PST",
			offset:       -8 * 60 * 60,
			want:         "Pacific Standard Time",
		},
		{
			name:         "ambiguous positive-offset CST",
			abbreviation: "CST",
			offset:       8 * 60 * 60,
			want:         "",
		},
		{
			name:         "unexpected PDT offset",
			abbreviation: "PDT",
			offset:       9 * 60 * 60,
			want:         "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := javaLongTimeZoneNameForLocation("Local", test.abbreviation, test.offset); got != test.want {
				t.Fatalf("Local %s at offset %d = %q, want %q", test.abbreviation, test.offset, got, test.want)
			}
		})
	}
}

func TestSimpleDateFormatHostLocalLosAngelesLongName(t *testing.T) {
	instant := time.UnixMilli(1_719_835_200_000).In(time.Local)
	name, offset := instant.Zone()
	if name != "PDT" || offset != -7*60*60 {
		t.Skipf("host local zone at probe instant is %s (%d), not PDT", name, offset)
	}
	got, err := formatJavaDate(instant, "z zzzz")
	if err != nil {
		t.Fatal(err)
	}
	const want = "PDT Pacific Daylight Time"
	if got != want {
		t.Fatalf("host-local zone names = %q, want %q", got, want)
	}
}

func TestSimpleDateFormatStandaloneParseFields(t *testing.T) {
	now := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		pattern string
		input   string
		millis  int64
	}{
		{name: "weekday name", pattern: "E", input: "Sunday", millis: 259_200_000},
		{name: "weekday name leading space", pattern: "EEEE", input: " \tMonday", millis: 345_600_000},
		{name: "ISO weekday", pattern: "u", input: "1", millis: 345_600_000},
		{name: "lenient negative ISO weekday", pattern: "u", input: "-1", millis: 86_400_000},
		{name: "lenient high ISO weekday", pattern: "u", input: "8", millis: 259_200_000},
		{name: "weekday ordinal", pattern: "F", input: "2", millis: 864_000_000},
		{name: "negative weekday ordinal", pattern: "F", input: "-1", millis: 2_073_600_000},
		{name: "week year", pattern: "Y", input: "2024", millis: 1_703_980_800_000},
		{name: "AM", pattern: "a", input: "AM", millis: 0},
		{name: "PM", pattern: "a", input: "PM", millis: 43_200_000},
		{name: "weekday ordinal and name", pattern: "F E", input: "2 Monday", millis: 950_400_000},
		{name: "week year and weekday", pattern: "Y E", input: "2024 Monday", millis: 1_704_067_200_000},
		{name: "later week year wins", pattern: "yyyy Y", input: "2020 2024", millis: 1_703_980_800_000},
		{name: "later calendar year wins", pattern: "Y yyyy", input: "2024 2020", millis: 1_577_836_800_000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseJavaDate(test.input, test.pattern, now)
			if err != nil {
				t.Fatal(err)
			}
			if got.UnixMilli() != test.millis {
				t.Fatalf("parseJavaDate(%q, %q) = %d, want %d", test.input, test.pattern, got.UnixMilli(), test.millis)
			}
		})
	}
}

func TestSimpleDateFormatJavaNumberForms(t *testing.T) {
	now := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		pattern string
		input   string
		millis  int64
	}{
		{name: "Arabic Indic", pattern: "yyyy-MM-dd", input: "٢٠٢٤-٠٢-٠٣", millis: 1_706_918_400_000},
		{name: "extended Arabic Indic", pattern: "yyyy-MM-dd", input: "۲۰۲۴-۰۲-۰۳", millis: 1_706_918_400_000},
		{name: "Devanagari", pattern: "yyyy-MM-dd", input: "२०२४-०२-०३", millis: 1_706_918_400_000},
		{name: "fullwidth", pattern: "yyyy-MM-dd", input: "２０２４-０２-０３", millis: 1_706_918_400_000},
		{name: "mixed digit blocks", pattern: "yyyy-MM-dd", input: "2٠२４-0۲-０3", millis: 1_706_918_400_000},
		{name: "adjacent Unicode digits", pattern: "yyyyMMdd", input: "٢٠٢٤٠٢٠٣", millis: 1_706_918_400_000},
		{name: "Unicode two digit year", pattern: "yy-MM-dd", input: "٢٤-٠١-٠١", millis: 1_704_067_200_000},
		{name: "negative month", pattern: "yyyy-MM-dd", input: "2024--1-1", millis: 1_698_796_800_000},
		{name: "negative day", pattern: "yyyy-MM-dd", input: "2024-1--1", millis: 1_703_894_400_000},
		{name: "lenient Unicode minus", pattern: "yyyy-MM-dd", input: "2024-−1-1", millis: 1_698_796_800_000},
		{name: "signed time fields", pattern: "yyyy-MM-dd HH:mm:ss.SSS", input: "2024-01-01 -1:-2:-3.-4", millis: 1_704_063_476_996},
		{name: "adjacent signed field obeys count", pattern: "MMdd", input: "-101", millis: -5_270_400_000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseJavaDate(test.input, test.pattern, now)
			if err != nil {
				t.Fatal(err)
			}
			if got.UnixMilli() != test.millis {
				t.Fatalf("parseJavaDate(%q, %q) = %d, want %d", test.input, test.pattern, got.UnixMilli(), test.millis)
			}
		})
	}
	if _, err := parseJavaDate("2024-+1-+1", "yyyy-MM-dd", now); err == nil {
		t.Fatal("parseJavaDate accepted NumberFormat-incompatible plus signs")
	}
}

func TestSimpleDateFormatGregorianCalendarCutover(t *testing.T) {
	now := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		input  string
		millis int64
		date   string
	}{
		{input: "1582-10-01", millis: -12_219_638_400_000, date: "AD 1582-10-01"},
		{input: "1582-10-04", millis: -12_219_379_200_000, date: "AD 1582-10-04"},
		{input: "1582-10-05", millis: -12_219_292_800_000, date: "AD 1582-10-15"},
		{input: "1582-10-10", millis: -12_218_860_800_000, date: "AD 1582-10-20"},
		{input: "1582-10-14", millis: -12_218_515_200_000, date: "AD 1582-10-24"},
		{input: "1582-10-15", millis: -12_219_292_800_000, date: "AD 1582-10-15"},
		{input: "1582-10-16", millis: -12_219_206_400_000, date: "AD 1582-10-16"},
		{input: "1500-01-01", millis: -14_830_992_000_000, date: "AD 1500-01-01"},
		{input: "0001-01-01", millis: -62_135_769_600_000, date: "AD 0001-01-01"},
		{input: "0000-01-01", millis: -62_167_392_000_000, date: "BC 0001-01-01"},
		{input: "-1-01-01", millis: -62_198_928_000_000, date: "BC 0002-01-01"},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			got, err := parseJavaDate(test.input, "yyyy-MM-dd", now)
			if err != nil {
				t.Fatal(err)
			}
			if got.UnixMilli() != test.millis {
				t.Fatalf("parseJavaDate(%q) = %d, want %d", test.input, got.UnixMilli(), test.millis)
			}
			formatted, err := formatJavaDate(got, "G yyyy-MM-dd")
			if err != nil {
				t.Fatal(err)
			}
			if formatted != test.date {
				t.Fatalf("format parsed %q = %q, want %q", test.input, formatted, test.date)
			}
		})
	}

	cutover := time.UnixMilli(-12_219_292_800_000).UTC()
	formatted, err := formatJavaDate(cutover, "yyyy-MM-dd D YYYY-w-W-F-u-E")
	if err != nil {
		t.Fatal(err)
	}
	if want := "1582-10-15 278 1582-40-1-1-5-Fri"; formatted != want {
		t.Fatalf("cutover calendar fields = %q, want %q", formatted, want)
	}

	fieldCases := []struct {
		pattern string
		input   string
		millis  int64
	}{
		{pattern: "yyyy-D", input: "1582-277", millis: -12_219_379_200_000},
		{pattern: "yyyy-D", input: "1582-278", millis: -12_219_292_800_000},
		{pattern: "YYYY-w-u", input: "1582-40-1", millis: -12_219_638_400_000},
		{pattern: "YYYY-w-u", input: "1582-41-1", millis: -12_219_033_600_000},
	}
	for _, test := range fieldCases {
		got, err := parseJavaDate(test.input, test.pattern, now)
		if err != nil {
			t.Fatal(err)
		}
		if got.UnixMilli() != test.millis {
			t.Fatalf("parseJavaDate(%q, %q) = %d, want %d", test.input, test.pattern, got.UnixMilli(), test.millis)
		}
	}
}

func TestJavaNumberParserCoversEveryBMPDecimalDigitBlock(t *testing.T) {
	// Keep the test oracle independent of both Go's evolving unicode.Digit
	// table and lexer.JavaDigit's production table. These are the BMP decimal
	// digit blocks in the Unicode 17 Character.digit(char, 10) contract.
	digitRanges := [...][2]rune{
		{0x0030, 0x0039}, {0x0660, 0x0669}, {0x06f0, 0x06f9},
		{0x07c0, 0x07c9}, {0x0966, 0x096f}, {0x09e6, 0x09ef},
		{0x0a66, 0x0a6f}, {0x0ae6, 0x0aef}, {0x0b66, 0x0b6f},
		{0x0be6, 0x0bef}, {0x0c66, 0x0c6f}, {0x0ce6, 0x0cef},
		{0x0d66, 0x0d6f}, {0x0de6, 0x0def}, {0x0e50, 0x0e59},
		{0x0ed0, 0x0ed9}, {0x0f20, 0x0f29}, {0x1040, 0x1049},
		{0x1090, 0x1099}, {0x17e0, 0x17e9}, {0x1810, 0x1819},
		{0x1946, 0x194f}, {0x19d0, 0x19d9}, {0x1a80, 0x1a89},
		{0x1a90, 0x1a99}, {0x1b50, 0x1b59}, {0x1bb0, 0x1bb9},
		{0x1c40, 0x1c49}, {0x1c50, 0x1c59}, {0xa620, 0xa629},
		{0xa8d0, 0xa8d9}, {0xa900, 0xa909}, {0xa9d0, 0xa9d9},
		{0xa9f0, 0xa9f9}, {0xaa50, 0xaa59}, {0xabf0, 0xabf9},
		{0xff10, 0xff19},
	}
	for _, digitRange := range digitRanges {
		for value := digitRange[0]; value <= digitRange[1]; value++ {
			want := int(value - digitRange[0])
			got, width, err := parseJavaNumber(string(value)+"tail", 0)
			if err != nil {
				t.Fatalf("parse U+%04X: %v", value, err)
			}
			if got != want || width != utf8.RuneLen(value) {
				t.Fatalf("parse U+%04X = (%d, %d), want (%d, %d)", value, got, width, want, utf8.RuneLen(value))
			}
		}
	}
	for _, value := range []rune{'²', 'Ⅻ', '𝟙'} {
		if _, _, err := parseJavaNumber(string(value), 0); err == nil {
			t.Fatalf("parseJavaNumber accepted non-BMP-decimal digit %q", value)
		}
	}
}

const simpleDateFormatExtendedParseProbeSource = `println(parseDate("E", "Sunday"));
println(parseDate("u", "1"));
println(parseDate("u", "-1"));
println(parseDate("F", "2"));
println(parseDate("F", "-1"));
println(parseDate("Y", "2024"));
println(parseDate("a", "AM"));
println(parseDate("a", "PM"));
println(parseDate("F E", "2 Monday"));
println(parseDate("Y E", "2024 Monday"));
println(parseDate("yyyy-MM-dd", "٢٠٢٤-٠٢-٠٣"));
println(parseDate("yyyy-MM-dd", "2٠२４-0۲-０3"));
println(parseDate("yy-MM-dd", "٢٤-٠١-٠١"));
println(parseDate("yyyy-MM-dd", "2024--1-1"));
println(parseDate("yyyy-MM-dd", "2024-1--1"));
println(parseDate("yyyy-MM-dd", "2024-−1-1"));
println(parseDate("yyyy-MM-dd HH:mm:ss.SSS", "2024-01-01 -1:-2:-3.-4"));
println(parseDate("MMdd", "-101"));
`

const simpleDateFormatExtendedParseProbeOutput = `259200000
345600000
86400000
864000000
2073600000
1703980800000
0
43200000
950400000
1704067200000
1706918400000
1706918400000
1704067200000
1698796800000
1703894400000
1698796800000
1704063476996
-5270400000
`

func TestSimpleDateFormatExtendedParseOfficialOutput(t *testing.T) {
	now := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	got := runSimpleDateFormatSource(t, "simple-date-format-extended-parse.sl", simpleDateFormatExtendedParseProbeSource, now)
	if !bytes.Equal(got, []byte(simpleDateFormatExtendedParseProbeOutput)) {
		t.Fatalf("extended SimpleDateFormat parse output mismatch\nwant:\n%sgot:\n%s", simpleDateFormatExtendedParseProbeOutput, got)
	}
}

func TestSimpleDateFormatExtendedParseOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	want, err := osexec.Command(
		java,
		"-Duser.timezone=UTC", "-Duser.language=en", "-Duser.country=US",
		"-jar", jar, "-e", simpleDateFormatExtendedParseProbeSource,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep extended date parse probe: %v\n%s", err, want)
	}
	now := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	got := runSimpleDateFormatSource(t, "simple-date-format-extended-parse.sl", simpleDateFormatExtendedParseProbeSource, now)
	if !bytes.Equal(got, want) {
		t.Fatalf("official Sleep extended date parse output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

const simpleDateFormatCutoverProbeSource = `println(parseDate("yyyy-MM-dd", "1582-10-01"));
println(parseDate("yyyy-MM-dd", "1582-10-04"));
println(parseDate("yyyy-MM-dd", "1582-10-05"));
println(parseDate("yyyy-MM-dd", "1582-10-10"));
println(parseDate("yyyy-MM-dd", "1582-10-14"));
println(parseDate("yyyy-MM-dd", "1582-10-15"));
println(parseDate("yyyy-MM-dd", "1582-10-16"));
println(parseDate("yyyy-MM-dd", "1500-01-01"));
println(parseDate("yyyy-MM-dd", "0001-01-01"));
println(parseDate("yyyy-MM-dd", "0000-01-01"));
println(parseDate("yyyy-MM-dd", "-1-01-01"));
println(formatDate(parseDate("yyyy-MM-dd", "1582-10-05"), "yyyy-MM-dd D YYYY-w-W-F-u-E"));
println(formatDate(parseDate("yyyy-MM-dd", "-1-01-01"), "G yyyy-MM-dd YYYY"));
println(parseDate("yyyy-D", "1582-277"));
println(parseDate("yyyy-D", "1582-278"));
println(parseDate("YYYY-w-u", "1582-40-1"));
println(parseDate("YYYY-w-u", "1582-41-1"));
`

const simpleDateFormatCutoverProbeOutput = `-12219638400000
-12219379200000
-12219292800000
-12218860800000
-12218515200000
-12219292800000
-12219206400000
-14830992000000
-62135769600000
-62167392000000
-62198928000000
1582-10-15 278 1582-40-1-1-5-Fri
BC 0002-01-01 -0001
-12219379200000
-12219292800000
-12219638400000
-12219033600000
`

func TestSimpleDateFormatGregorianCutoverOfficialOutput(t *testing.T) {
	now := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	got := runSimpleDateFormatSource(t, "simple-date-format-cutover.sl", simpleDateFormatCutoverProbeSource, now)
	if !bytes.Equal(got, []byte(simpleDateFormatCutoverProbeOutput)) {
		t.Fatalf("GregorianCalendar cutover output mismatch\nwant:\n%sgot:\n%s", simpleDateFormatCutoverProbeOutput, got)
	}
}

func TestSimpleDateFormatGregorianCutoverOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	want, err := osexec.Command(
		java,
		"-Duser.timezone=UTC", "-Duser.language=en", "-Duser.country=US",
		"-jar", jar, "-e", simpleDateFormatCutoverProbeSource,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep GregorianCalendar cutover probe: %v\n%s", err, want)
	}
	now := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	got := runSimpleDateFormatSource(t, "simple-date-format-cutover.sl", simpleDateFormatCutoverProbeSource, now)
	if !bytes.Equal(got, want) {
		t.Fatalf("official Sleep GregorianCalendar cutover output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

const simpleDateFormatNumericZoneProbeName = "simple-date-format-numeric-zone.sl"

const simpleDateFormatNumericZoneProbeSource = `println(parseDate("yyyy-MM-dd HH:mm X", "1970-01-01 00:00 +0230"));
println(parseDate("yyyy-MM-dd HH:mm X", "1970-01-01 00:00 +02:30"));
println(parseDate("yyyy-MM-dd HH:mm XX", "1970-01-01 00:00 +0230"));
println(parseDate("yyyy-MM-dd HH:mm XXX", "1970-01-01 00:00 +02:30"));
println(parseDate("yyyy-MM-dd HH:mm Z", "1970-01-01 00:00 +0230"));
sub reject_xx_colon {
    parseDate("yyyy-MM-dd HH:mm XX", "1970-01-01 00:00 +02:30");
    println("reject-xx-colon-tail");
}
reject_xx_colon();
println("reject-xx-colon-resume");
sub reject_xxx_compact {
    parseDate("yyyy-MM-dd HH:mm XXX", "1970-01-01 00:00 +0230");
    println("reject-xxx-compact-tail");
}
reject_xxx_compact();
println("reject-xxx-compact-resume");
sub reject_z_colon {
    parseDate("yyyy-MM-dd HH:mm Z", "1970-01-01 00:00 +02:30");
    println("reject-z-colon-tail");
}
reject_z_colon();
println("reject-z-colon-resume");
`

func TestSimpleDateFormatNumericTimeZoneOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	directory := t.TempDir()
	path := filepath.Join(directory, simpleDateFormatNumericZoneProbeName)
	if err := os.WriteFile(path, []byte(simpleDateFormatNumericZoneProbeSource), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := osexec.Command(
		java,
		"-Duser.timezone=UTC", "-Duser.language=en", "-Duser.country=US",
		"-jar", jar, path,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep numeric time-zone probe: %v\n%s", err, want)
	}
	now := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	got := runSimpleDateFormatSource(t, simpleDateFormatNumericZoneProbeName, simpleDateFormatNumericZoneProbeSource, now)
	if !bytes.Equal(got, want) {
		t.Fatalf("official Sleep numeric time-zone output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

const simpleDateFormatLosAngelesProbeSource = `println(formatDate(1719835200000L, "yyyy-MM-dd YYYY-w-W D-u z zzzz G GGGG"));
`

func TestSimpleDateFormatLosAngelesOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	want, err := osexec.Command(
		java,
		"-Duser.timezone=America/Los_Angeles", "-Duser.language=en", "-Duser.country=US",
		"-jar", jar, "-e", simpleDateFormatLosAngelesProbeSource,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep Los Angeles date probe: %v\n%s", err, want)
	}
	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 25, 12, 0, 0, 0, location)
	got := runSimpleDateFormatSource(t, "simple-date-format-los-angeles.sl", simpleDateFormatLosAngelesProbeSource, now)
	if !bytes.Equal(got, want) {
		t.Fatalf("official Sleep Los Angeles date output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func TestSimpleDateFormatRollingYearOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	now := time.Now().UTC()
	pivot := now.AddDate(-80, 0, 0)
	before := pivot.AddDate(0, 0, -2).Format("06-01-02")
	after := pivot.AddDate(0, 0, 2).Format("06-01-02")
	source := fmt.Sprintf("println(parseDate(\"yy-MM-dd\", %q));\nprintln(parseDate(\"yy-MM-dd\", %q));\n", before, after)
	want, err := osexec.Command(
		java,
		"-Duser.timezone=UTC", "-Duser.language=en", "-Duser.country=US",
		"-jar", jar, "-e", source,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep rolling-year probe: %v\n%s", err, want)
	}
	got := runSimpleDateFormatSource(t, "simple-date-format-rolling-year.sl", source, now)
	if !bytes.Equal(got, want) {
		t.Fatalf("official Sleep rolling-year output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

const sleepTimeDateWarningProbeName = "sleep-time-date-warning.sl"

const sleepTimeDateWarningProbe = `sub format_bad_pattern {
    println("format-bad-before");
    formatDate("Q");
    println("format-bad-tail");
}
format_bad_pattern();
println("format-bad-resume");
setf("&zformat", function("&formatDate"));
sub format_alias_bad {
    println("format-alias-before");
    zformat(0L, "XXXX");
    println("format-alias-tail");
}
format_alias_bad();
println("format-alias-resume");
sub format_quote_bad {
    println("format-quote-before");
    formatDate("yyyy\u0027");
    println("format-quote-tail");
}
format_quote_bad();
println("format-quote-resume");
sub parse_bad_pattern {
    println("parse-pattern-before");
    parseDate("Q", "x");
    println("parse-pattern-tail");
}
parse_bad_pattern();
println("parse-pattern-resume");
setf("&zparse", function("&parseDate"));
sub parse_alias_bad {
    println("parse-alias-before");
    zparse("yyyy-MM-dd", "bad");
    println("parse-alias-tail");
}
parse_alias_bad();
println("parse-alias-resume");
println("valid-format=" . zformat(0L, "yyyy-\u0027Q\u0027MM"));
println("valid-parse=" . zparse("yyyy-MM-dd", "1970-01-01suffix"));
`

const sleepTimeDateWarningOutput = `format-bad-before
Warning: Illegal pattern character 'Q' at sleep-time-date-warning.sl:3
format-bad-resume
format-alias-before
Warning: invalid ISO 8601 format: length=4 at sleep-time-date-warning.sl:11
format-alias-resume
format-quote-before
Warning: Unterminated quote at sleep-time-date-warning.sl:18
format-quote-resume
parse-pattern-before
Warning: Illegal pattern character 'Q' at sleep-time-date-warning.sl:25
parse-pattern-resume
parse-alias-before
Warning: null value error at sleep-time-date-warning.sl:33
parse-alias-resume
valid-format=1970-Q01
valid-parse=0
`

func TestSleepTimeDateBridgeWarningCompatibility(t *testing.T) {
	if got := runSleepTimeDateWarningProbe(t); !bytes.Equal(got, []byte(sleepTimeDateWarningOutput)) {
		t.Fatalf("TimeDateBridge warning output mismatch\nwant:\n%sgot:\n%s", sleepTimeDateWarningOutput, got)
	}
}

func TestSleepTimeDateBridgeWarningOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	directory := t.TempDir()
	path := filepath.Join(directory, sleepTimeDateWarningProbeName)
	if err := os.WriteFile(path, []byte(sleepTimeDateWarningProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	command := osexec.Command(
		java,
		"-Duser.timezone=UTC", "-Duser.language=en", "-Duser.country=US",
		"-jar", jar, path,
	)
	command.Dir = directory
	want, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep TimeDateBridge warning probe: %v\n%s", err, want)
	}
	if got := runSleepTimeDateWarningProbe(t); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep TimeDateBridge warning output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runSleepTimeDateWarningProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	runtimeInstance, err := New(
		WithClock(ClockFunc(func() time.Time { return now })),
		WithStdout(&output), WithStderr(&output),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), sleepTimeDateWarningProbeName, sleepTimeDateWarningProbe); err != nil {
		t.Fatalf("Eval: %v\n%s", err, output.String())
	}
	return append([]byte(nil), output.Bytes()...)
}
