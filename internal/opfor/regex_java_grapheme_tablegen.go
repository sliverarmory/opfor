//go:build ignore

// regex_java_grapheme_tablegen.go regenerates the fixed Unicode tables used
// by the embedded regexp2 grapheme engine. Every input is pinned to the final
// Unicode 17.0.0 data; the generator refuses mismatched bytes.
//
// Run from the repository root:
//
//	go run ./internal/opfor/regex_java_grapheme_tablegen.go \
//	  -out internal/regexp2/java_unicode17_grapheme_tables.go \
//	  -unicode-data /path/to/UnicodeData.txt \
//	  -grapheme-break-property /path/to/auxiliary/GraphemeBreakProperty.txt \
//	  -derived-core-properties /path/to/DerivedCoreProperties.txt \
//	  -emoji-data /path/to/emoji/emoji-data.txt
//
// Add -check to compare the generated output byte-for-byte.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"flag"
	"fmt"
	"go/format"
	"os"
	"strconv"
	"strings"
)

const (
	graphemeUnicodeVersion = "17.0.0"

	graphemeUnicodeDataSHA256           = "2e1efc1dcb59c575eedf5ccae60f95229f706ee6d031835247d843c11d96470c"
	graphemeBreakPropertySHA256         = "d6b51d1d2ae5c33b451b7ed994b48f1f4dc62b2272a5831e7fd418514a6bae89"
	graphemeDerivedCorePropertiesSHA256 = "24c7fed1195c482faaefd5c1e7eb821c5ee1fb6de07ecdbaa64b56a99da22c08"
	graphemeEmojiDataSHA256             = "2cb2bb9455cda83e8481541ecf5b6dfda66a3bb89efa3fa7c5297eccf607b72b"

	graphemeRuneLimit = 0x110000
)

type graphemeRuneRange struct {
	lo    rune
	hi    rune
	value string
}

func main() {
	unicodeDataPath := flag.String("unicode-data", "", "path to Unicode 17.0.0 UnicodeData.txt")
	graphemeBreakPath := flag.String("grapheme-break-property", "", "path to Unicode 17.0.0 GraphemeBreakProperty.txt")
	derivedCorePath := flag.String("derived-core-properties", "", "path to Unicode 17.0.0 DerivedCoreProperties.txt")
	emojiDataPath := flag.String("emoji-data", "", "path to Unicode 17.0.0 emoji-data.txt")
	outputPath := flag.String("out", "internal/regexp2/java_unicode17_grapheme_tables.go", "generated output path")
	check := flag.Bool("check", false, "verify generated output instead of writing it")
	flag.Parse()

	for _, required := range []*string{unicodeDataPath, graphemeBreakPath, derivedCorePath, emojiDataPath} {
		if *required == "" {
			flag.Usage()
			os.Exit(2)
		}
	}

	unicodeData := mustReadGraphemeInput(*unicodeDataPath, "UnicodeData.txt", graphemeUnicodeDataSHA256)
	graphemeBreak := mustReadGraphemeInput(*graphemeBreakPath, "GraphemeBreakProperty.txt", graphemeBreakPropertySHA256)
	derivedCore := mustReadGraphemeInput(*derivedCorePath, "DerivedCoreProperties.txt", graphemeDerivedCorePropertiesSHA256)
	emojiData := mustReadGraphemeInput(*emojiDataPath, "emoji-data.txt", graphemeEmojiDataSHA256)

	categories := parseGraphemeUnicodeCategories(unicodeData)
	gcb := parseGraphemeProperties(graphemeBreak, 1, "")
	extendedPictographic := parseGraphemeProperties(emojiData, 1, "Extended_Pictographic")
	indic := parseGraphemeProperties(derivedCore, 2, "InCB")

	classByRune := make([]string, graphemeRuneLimit)
	for cp := 0; cp < graphemeRuneLimit; cp++ {
		classByRune[cp] = openJDKGraphemeClass(rune(cp), categories[cp], gcb[cp], extendedPictographic[cp] != "")
	}
	verifyGraphemeBreakProperties(classByRune, categories, gcb, extendedPictographic)

	classRanges := coalesceGraphemeValues(classByRune, "graphemeOther")
	indicRanges := coalesceIndicValues(indic)
	source := generateGraphemeSource(classRanges, indicRanges)

	if *check {
		current, err := os.ReadFile(*outputPath)
		if err != nil {
			panic(err)
		}
		if !bytes.Equal(current, source) {
			panic(fmt.Sprintf("%s is not current", *outputPath))
		}
		return
	}
	if err := os.WriteFile(*outputPath, source, 0o644); err != nil {
		panic(err)
	}
}

func mustReadGraphemeInput(path, label, wantHash string) []byte {
	value, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	got := fmt.Sprintf("%x", sha256.Sum256(value))
	if got != wantHash {
		panic(fmt.Sprintf("%s SHA-256 mismatch: got %s, want %s", label, got, wantHash))
	}
	return value
}

func parseGraphemeUnicodeCategories(data []byte) []string {
	categories := make([]string, graphemeRuneLimit)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	var rangeStart rune = -1
	var rangeCategory string
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ";")
		if len(fields) < 3 {
			panic("malformed UnicodeData.txt record")
		}
		cp := mustParseGraphemeRune(fields[0])
		name := fields[1]
		category := fields[2]
		if strings.HasSuffix(name, ", First>") {
			rangeStart = cp
			rangeCategory = category
			continue
		}
		if strings.HasSuffix(name, ", Last>") {
			if rangeStart < 0 || rangeCategory != category {
				panic("malformed UnicodeData.txt range")
			}
			for current := rangeStart; current <= cp; current++ {
				categories[current] = category
			}
			rangeStart = -1
			continue
		}
		categories[cp] = category
	}
	if err := scanner.Err(); err != nil {
		panic(err)
	}
	if rangeStart >= 0 {
		panic("unterminated UnicodeData.txt range")
	}
	return categories
}

// parseGraphemeProperties returns a dense build-time view. valueColumn is the
// zero-based semicolon field holding the value. When propertyName is nonempty,
// field 1 must equal it (DerivedCoreProperties uses "InCB; Value").
func parseGraphemeProperties(data []byte, valueColumn int, propertyName string) []string {
	values := make([]string, graphemeRuneLimit)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(strings.SplitN(scanner.Text(), "#", 2)[0])
		if line == "" {
			continue
		}
		fields := strings.Split(line, ";")
		for index := range fields {
			fields[index] = strings.TrimSpace(fields[index])
		}
		if valueColumn >= len(fields) || (propertyName != "" && (len(fields) < 2 || fields[1] != propertyName)) {
			continue
		}
		lo, hi := parseGraphemeRange(fields[0])
		for cp := lo; cp <= hi; cp++ {
			values[cp] = fields[valueColumn]
		}
	}
	if err := scanner.Err(); err != nil {
		panic(err)
	}
	return values
}

func openJDKGraphemeClass(cp rune, category, graphemeBreak string, extendedPictographic bool) string {
	if extendedPictographic {
		return "graphemeExtendedPictographic"
	}
	// Grapheme.java uses Character.getType rather than consuming the UCD's
	// Grapheme_Cluster_Break table directly. These are its only observable
	// classification differences from the authenticated table.
	switch {
	case category == "": // UnicodeData's omitted code points are UNASSIGNED.
		if cp == 0x0378 {
			return "graphemeOther"
		}
		return "graphemeControl"
	case category == "Cs": // Lone UTF-16 surrogate code units.
		return "graphemeControl"
	case category == "Mc" && !isOpenJDKExcludedSpacingMark(cp):
		return "graphemeSpacingMark"
	}
	return graphemeClassName(graphemeBreak)
}

func isOpenJDKExcludedSpacingMark(cp rune) bool {
	return cp == 0x102b || cp == 0x102c || cp == 0x1038 ||
		cp >= 0x1062 && cp <= 0x1064 || cp >= 0x1067 && cp <= 0x106d ||
		cp == 0x1083 || cp >= 0x1087 && cp <= 0x108c || cp == 0x108f ||
		cp >= 0x109a && cp <= 0x109c || cp == 0x1a61 || cp == 0x1a63 ||
		cp == 0x1a64 || cp == 0xaa7b || cp == 0xaa7d
}

func verifyGraphemeBreakProperties(classes, categories, gcb, extendedPictographic []string) {
	spacingExtend := 0
	spacingOther := 0
	for cp := 0; cp < graphemeRuneLimit; cp++ {
		want := graphemeClassName(gcb[cp])
		if extendedPictographic[cp] != "" {
			want = "graphemeExtendedPictographic"
		}
		if classes[cp] == want {
			continue
		}
		// Grapheme.java deliberately treats unassigned code points and lone
		// UTF-16 surrogates as Control. GraphemeBreakProperty defaults those
		// to Other unless a particular unassigned range is listed.
		if classes[cp] == "graphemeControl" && want == "graphemeOther" &&
			(categories[cp] == "" || categories[cp] == "Cs") {
			continue
		}
		// Grapheme.java intentionally maps the remaining Mc characters to
		// SpacingMark using General_Category. The UCD assigns a small audited
		// subset to Extend or Other; GB9 and GB9a have the same break result.
		if categories[cp] == "Mc" && classes[cp] == "graphemeSpacingMark" {
			switch want {
			case "graphemeExtend":
				spacingExtend++
				continue
			case "graphemeOther":
				spacingOther++
				continue
			}
		}
		panic(fmt.Sprintf("OpenJDK/UCD grapheme class mismatch at U+%04X: got %s, UCD %s", cp, classes[cp], want))
	}
	if spacingExtend != 61 || spacingOther != 2 {
		panic(fmt.Sprintf("unexpected audited Mc differences: Extend=%d Other=%d", spacingExtend, spacingOther))
	}
}

func graphemeClassName(value string) string {
	switch value {
	case "CR":
		return "graphemeCR"
	case "LF":
		return "graphemeLF"
	case "Control":
		return "graphemeControl"
	case "Extend":
		return "graphemeExtend"
	case "ZWJ":
		return "graphemeZWJ"
	case "Regional_Indicator":
		return "graphemeRI"
	case "Prepend":
		return "graphemePrepend"
	case "SpacingMark":
		return "graphemeSpacingMark"
	case "L":
		return "graphemeL"
	case "V":
		return "graphemeV"
	case "T":
		return "graphemeT"
	case "LV":
		return "graphemeLV"
	case "LVT":
		return "graphemeLVT"
	case "":
		return "graphemeOther"
	default:
		panic("unsupported Grapheme_Cluster_Break value " + value)
	}
}

func coalesceGraphemeValues(values []string, omitted string) []graphemeRuneRange {
	ranges := make([]graphemeRuneRange, 0)
	for start := 0; start < len(values); {
		value := values[start]
		end := start
		for end+1 < len(values) && values[end+1] == value {
			end++
		}
		if value != omitted {
			ranges = append(ranges, graphemeRuneRange{lo: rune(start), hi: rune(end), value: value})
		}
		start = end + 1
	}
	return ranges
}

func coalesceIndicValues(values []string) []graphemeRuneRange {
	mapped := make([]string, len(values))
	for cp, value := range values {
		switch value {
		case "":
			mapped[cp] = "indicNone"
		case "Extend":
			mapped[cp] = "indicExtend"
		case "Linker":
			mapped[cp] = "indicLinker"
		case "Consonant":
			mapped[cp] = "indicConsonant"
		default:
			panic("unsupported Indic_Conjunct_Break value " + value)
		}
	}
	return coalesceGraphemeValues(mapped, "indicNone")
}

func generateGraphemeSource(classes, indic []graphemeRuneRange) []byte {
	var result bytes.Buffer
	result.WriteString("// Code generated by regex_java_grapheme_tablegen.go; DO NOT EDIT.\n//\n")
	result.WriteString("// Unicode data copyright © 1991-2025 Unicode, Inc. and is\n")
	result.WriteString("// distributed under the Unicode License v3.\n\n")
	result.WriteString("\n")
	result.WriteString("package regexp2\n\n")
	fmt.Fprintf(&result, "const javaGraphemeUnicodeVersion = %q\n", graphemeUnicodeVersion)
	fmt.Fprintf(&result, "const javaGraphemeUnicodeDataSHA256 = %q\n", graphemeUnicodeDataSHA256)
	fmt.Fprintf(&result, "const javaGraphemeBreakPropertySHA256 = %q\n", graphemeBreakPropertySHA256)
	fmt.Fprintf(&result, "const javaGraphemeDerivedCorePropertiesSHA256 = %q\n", graphemeDerivedCorePropertiesSHA256)
	fmt.Fprintf(&result, "const javaGraphemeEmojiDataSHA256 = %q\n\n", graphemeEmojiDataSHA256)
	writeGraphemeRanges(&result, "javaGraphemeClassRanges", classes)
	writeGraphemeRanges(&result, "javaIndicConjunctRanges", indic)
	formatted, err := format.Source(result.Bytes())
	if err != nil {
		panic(err)
	}
	return formatted
}

func writeGraphemeRanges(result *bytes.Buffer, name string, ranges []graphemeRuneRange) {
	fmt.Fprintf(result, "var %s = []javaGraphemeRange{\n", name)
	for _, current := range ranges {
		fmt.Fprintf(result, "\t{lo: %#x, hi: %#x, value: %s},\n", current.lo, current.hi, current.value)
	}
	result.WriteString("}\n\n")
}

func parseGraphemeRange(value string) (rune, rune) {
	parts := strings.Split(value, "..")
	lo := mustParseGraphemeRune(parts[0])
	hi := lo
	if len(parts) == 2 {
		hi = mustParseGraphemeRune(parts[1])
	} else if len(parts) != 1 {
		panic("malformed code point range " + value)
	}
	return lo, hi
}

func mustParseGraphemeRune(value string) rune {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 16, 32)
	if err != nil || parsed < 0 || parsed >= graphemeRuneLimit {
		panic("malformed code point " + value)
	}
	return rune(parsed)
}
