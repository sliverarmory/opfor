//go:build ignore

// regex_java_tablegen.go regenerates the fixed Unicode tables used by the
// java.util.regex and portable java.lang.String compatibility layers. It is
// intentionally excluded from ordinary builds. Every input is pinned to the
// final Unicode 17.0.0 files; the generator refuses data whose SHA-256 digest
// does not match.
//
// Run from the repository root:
//
//	go run ./internal/opfor/regex_java_tablegen.go \
//	  -out internal/opfor/regex_java_unicode_tables.go \
//	  -case-out internal/regexp2/syntax/java_unicode17_case.go \
//	  -unicode-data /path/to/UnicodeData.txt \
//	  -blocks /path/to/Blocks.txt \
//	  -scripts /path/to/Scripts.txt \
//	  -prop-list /path/to/PropList.txt \
//	  -property-value-aliases /path/to/PropertyValueAliases.txt \
//	  -special-casing /path/to/SpecialCasing.txt \
//	  -derived-core-properties /path/to/DerivedCoreProperties.txt \
//	  -emoji-data /path/to/emoji/emoji-data.txt
//
// Add -check to verify that both checked-in generated files are current.
package main

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"flag"
	"fmt"
	"go/format"
	"os"
	"sort"
	"strconv"
	"strings"
)

const (
	unicodeVersion = "17.0.0"

	unicodeDataSHA256           = "2e1efc1dcb59c575eedf5ccae60f95229f706ee6d031835247d843c11d96470c"
	blocksSHA256                = "c0edefaf1a19771e830a82735472716af6bf3c3975f6c2a23ffbe2580fbbcb15"
	scriptsSHA256               = "9f5e50d3abaee7d6ce09480f325c706f485ae3240912527e651954d2d6b035bf"
	propListSHA256              = "130dcddcaadaf071008bdfce1e7743e04fdfbc910886f017d9f9ac931d8c64dd"
	propertyValueAliasesSHA256  = "64e9a5f76f7a1e8b5a47d6a1f9a26522a251208f5276bdfa1559dac7cf2e827a"
	specialCasingSHA256         = "efc25faf19de21b92c1194c111c932e03d2a5eaf18194e33f1156e96de4c9588"
	derivedCorePropertiesSHA256 = "24c7fed1195c482faaefd5c1e7eb821c5ee1fb6de07ecdbaa64b56a99da22c08"
	emojiDataSHA256             = "2cb2bb9455cda83e8481541ecf5b6dfda66a3bb89efa3fa7c5297eccf607b72b"
)

type runeRange struct {
	lo rune
	hi rune
}

type unicodeBlock struct {
	lo       rune
	hi       rune
	javaName string
}

type nameRecord struct {
	cp   rune
	name string
}

type unicodeDataTables struct {
	mirrored   []runeRange
	assigned   []runeRange
	categories map[string][]runeRange
	names      []nameRecord
	upper      map[rune]rune
	lower      map[rune]rune
}

type generatorInputs struct {
	unicodeData           []byte
	blocks                []byte
	scripts               []byte
	propList              []byte
	propertyValueAliases  []byte
	specialCasing         []byte
	derivedCoreProperties []byte
	emojiData             []byte
}

func main() {
	unicodeDataPath := flag.String("unicode-data", "", "path to Unicode 17.0.0 UnicodeData.txt")
	blocksPath := flag.String("blocks", "", "path to Unicode 17.0.0 Blocks.txt")
	scriptsPath := flag.String("scripts", "", "path to Unicode 17.0.0 Scripts.txt")
	propListPath := flag.String("prop-list", "", "path to Unicode 17.0.0 PropList.txt")
	aliasesPath := flag.String("property-value-aliases", "", "path to Unicode 17.0.0 PropertyValueAliases.txt")
	specialCasingPath := flag.String("special-casing", "", "path to Unicode 17.0.0 SpecialCasing.txt")
	derivedCorePropertiesPath := flag.String("derived-core-properties", "", "path to Unicode 17.0.0 DerivedCoreProperties.txt")
	emojiDataPath := flag.String("emoji-data", "", "path to Unicode 17.0.0 emoji/emoji-data.txt")
	outputPath := flag.String("out", "internal/opfor/regex_java_unicode_tables.go", "generated OPFOR table output")
	caseOutputPath := flag.String("case-out", "internal/regexp2/syntax/java_unicode17_case.go", "generated regexp2 Java case table output")
	check := flag.Bool("check", false, "verify generated outputs instead of writing them")
	flag.Parse()
	for _, required := range []*string{unicodeDataPath, blocksPath, scriptsPath, propListPath, aliasesPath, specialCasingPath, derivedCorePropertiesPath, emojiDataPath} {
		if *required == "" {
			flag.Usage()
			os.Exit(2)
		}
	}

	inputs := generatorInputs{
		unicodeData:           mustReadPinned(*unicodeDataPath, "UnicodeData.txt", unicodeDataSHA256),
		blocks:                mustReadPinned(*blocksPath, "Blocks.txt", blocksSHA256),
		scripts:               mustReadPinned(*scriptsPath, "Scripts.txt", scriptsSHA256),
		propList:              mustReadPinned(*propListPath, "PropList.txt", propListSHA256),
		propertyValueAliases:  mustReadPinned(*aliasesPath, "PropertyValueAliases.txt", propertyValueAliasesSHA256),
		specialCasing:         mustReadPinned(*specialCasingPath, "SpecialCasing.txt", specialCasingSHA256),
		derivedCoreProperties: mustReadPinned(*derivedCorePropertiesPath, "DerivedCoreProperties.txt", derivedCorePropertiesSHA256),
		emojiData:             mustReadPinned(*emojiDataPath, "emoji-data.txt", emojiDataSHA256),
	}
	unicodeData := parseUnicodeData(inputs.unicodeData)
	blocks, blockAliases := parseBlocks(inputs.blocks)
	scripts, scriptAliases := parseScripts(inputs.scripts, inputs.propertyValueAliases)
	properties := parsePropertyRanges(inputs.propList)
	emoji := parseEmojiData(inputs.emojiData)
	for name, ranges := range emoji {
		properties[name] = ranges
	}
	properties = buildCompositeProperties(unicodeData.categories, unicodeData.assigned, properties)
	fullUpper := parseFullUpperMappings(inputs.specialCasing, unicodeData.upper)
	fullLower := parseFullLowerMappings(inputs.specialCasing, unicodeData.lower)
	caseContext := parseCaseContextProperties(inputs.derivedCoreProperties)

	mainSource := generateMainSource(unicodeData, blocks, blockAliases, scripts, scriptAliases, properties, fullUpper, fullLower, caseContext)
	caseSource := generateCaseSource(unicodeData.upper, unicodeData.lower)
	if *check {
		checkGenerated(*outputPath, mainSource)
		checkGenerated(*caseOutputPath, caseSource)
		return
	}
	mustWrite(*outputPath, mainSource)
	mustWrite(*caseOutputPath, caseSource)
}

func mustReadPinned(path, label, wantHash string) []byte {
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

func mustWrite(path string, source []byte) {
	if err := os.WriteFile(path, source, 0o644); err != nil {
		panic(err)
	}
}

func checkGenerated(path string, want []byte) {
	got, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	if !bytes.Equal(got, want) {
		panic(fmt.Sprintf("generated file %s is stale; rerun regex_java_tablegen.go without -check", path))
	}
}

func parseUnicodeData(source []byte) unicodeDataTables {
	result := unicodeDataTables{
		categories: make(map[string][]runeRange),
		upper:      make(map[rune]rune),
		lower:      make(map[rune]rune),
	}
	scanner := bufio.NewScanner(bytes.NewReader(source))
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	var rangeStart rune = -1
	var rangeCategory string
	var rangeMirrored bool
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ";")
		if len(fields) < 14 {
			panic("malformed UnicodeData record")
		}
		value, err := strconv.ParseInt(fields[0], 16, 32)
		if err != nil {
			panic(err)
		}
		cp := rune(value)
		name := fields[1]
		category := fields[2]
		mirrored := fields[9] == "Y"
		switch {
		case strings.HasSuffix(name, ", First>"):
			if rangeStart >= 0 {
				panic("nested UnicodeData range")
			}
			rangeStart, rangeCategory, rangeMirrored = cp, category, mirrored
		case strings.HasSuffix(name, ", Last>"):
			if rangeStart < 0 || rangeCategory != category || rangeMirrored != mirrored {
				panic("mismatched UnicodeData range")
			}
			current := runeRange{lo: rangeStart, hi: cp}
			result.assigned = append(result.assigned, current)
			result.categories[category] = append(result.categories[category], current)
			if mirrored {
				result.mirrored = append(result.mirrored, current)
			}
			rangeStart = -1
		default:
			current := runeRange{lo: cp, hi: cp}
			result.assigned = append(result.assigned, current)
			result.categories[category] = append(result.categories[category], current)
			if mirrored {
				result.mirrored = append(result.mirrored, current)
			}
		}

		if fields[12] != "" {
			result.upper[cp] = parseCodePoint(fields[12])
		}
		if fields[13] != "" {
			result.lower[cp] = parseCodePoint(fields[13])
		}
		switch {
		case name == "<control>":
			name = fields[10]
			switch cp {
			case 0x7:
				name = "BEL"
			case 0x80:
				if name == "" {
					name = "PADDING CHARACTER"
				}
			case 0x81:
				if name == "" {
					name = "HIGH OCTET PRESET"
				}
			case 0x99:
				if name == "" {
					name = "SINGLE GRAPHIC CHARACTER INTRODUCER"
				}
			}
		case strings.HasPrefix(name, "<"):
			name = ""
		}
		if name != "" {
			result.names = append(result.names, nameRecord{cp: cp, name: name})
		}
	}
	if err := scanner.Err(); err != nil {
		panic(err)
	}
	if rangeStart >= 0 {
		panic("unterminated UnicodeData range")
	}
	result.mirrored = mergeRanges(result.mirrored)
	result.assigned = mergeRanges(result.assigned)
	for name, ranges := range result.categories {
		result.categories[name] = mergeRanges(ranges)
	}
	result.categories["Cn"] = complementRanges(result.assigned)
	addCategoryAggregates(result.categories)
	return result
}

func addCategoryAggregates(categories map[string][]runeRange) {
	groups := map[string][]string{
		"LC": {"Lu", "Ll", "Lt"},
		"L":  {"Lu", "Ll", "Lt", "Lm", "Lo"},
		"LD": {"Lu", "Ll", "Lt", "Lm", "Lo", "Nd"},
		"M":  {"Mn", "Me", "Mc"},
		"N":  {"Nd", "Nl", "No"},
		"Z":  {"Zs", "Zl", "Zp"},
		"C":  {"Cc", "Cf", "Co", "Cs", "Cn"},
		"P":  {"Pd", "Ps", "Pe", "Pc", "Po", "Pi", "Pf"},
		"S":  {"Sm", "Sc", "Sk", "So"},
	}
	for name, members := range groups {
		categories[name] = unionNamedRanges(categories, members...)
	}
}

// parseBlocks derives Java's three documented UnicodeBlock.forName spellings
// from Blocks.txt. The three historical identifiers below are the complete
// manually audited Java-only exception list in the Java 26 behavioral oracle.
// No OpenJDK source table is parsed or copied.
func parseBlocks(source []byte) ([]unicodeBlock, map[string]int) {
	blocks := make([]unicodeBlock, 0, 360)
	aliases := make(map[string]int, 820)
	scanner := bufio.NewScanner(bytes.NewReader(source))
	for scanner.Scan() {
		line := dataLine(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Split(line, ";")
		if len(fields) != 2 {
			panic("malformed Blocks record")
		}
		current := parseRange(strings.TrimSpace(fields[0]))
		canonical := strings.ToUpper(strings.TrimSpace(fields[1]))
		identifier := strings.NewReplacer(" ", "_", "-", "_").Replace(canonical)
		extra := []string(nil)
		switch canonical {
		case "GREEK AND COPTIC":
			identifier = "GREEK"
		case "COMBINING DIACRITICAL MARKS FOR SYMBOLS":
			identifier = "COMBINING_MARKS_FOR_SYMBOLS"
			extra = []string{"COMBINING MARKS FOR SYMBOLS", "COMBININGMARKSFORSYMBOLS"}
		case "CYRILLIC SUPPLEMENT":
			identifier = "CYRILLIC_SUPPLEMENTARY"
			extra = []string{"CYRILLIC SUPPLEMENTARY", "CYRILLICSUPPLEMENTARY"}
		}
		index := len(blocks)
		blocks = append(blocks, unicodeBlock{lo: current.lo, hi: current.hi, javaName: identifier})
		for _, alias := range append([]string{identifier, canonical, strings.ReplaceAll(canonical, " ", "")}, extra...) {
			aliases[alias] = index
		}
	}
	if err := scanner.Err(); err != nil {
		panic(err)
	}
	// Java retains this removed aggregate identifier, but UnicodeBlock.of
	// never returns it. Its Pattern predicate is therefore an empty class.
	aliases["SURROGATES_AREA"] = -1
	return blocks, aliases
}

func parseScripts(source, aliasesSource []byte) (map[string][]runeRange, map[string]string) {
	scripts := parseSemicolonRanges(source, nil)
	var covered []runeRange
	for _, ranges := range scripts {
		covered = append(covered, ranges...)
	}
	scripts["Unknown"] = complementRanges(mergeRanges(covered))

	aliases := make(map[string]string, len(scripts)*2)
	for name := range scripts {
		aliases[strings.ToUpper(name)] = name
	}
	scanner := bufio.NewScanner(bytes.NewReader(aliasesSource))
	for scanner.Scan() {
		line := dataLine(scanner.Text())
		if line == "" {
			continue
		}
		fields := splitTrimmed(line, ";")
		if len(fields) < 3 || fields[0] != "sc" {
			continue
		}
		canonical := fields[2]
		if _, ok := scripts[canonical]; !ok {
			// PropertyValueAliases retains collective ISO script aliases such
			// as Hrkt (Katakana_Or_Hiragana) that are not Script property
			// values and therefore are not Character.UnicodeScript constants.
			continue
		}
		// Character.UnicodeScript.forName accepts enum identifiers and the
		// ISO 15924 alias, not UCD loose matching.
		aliases[strings.ToUpper(fields[1])] = canonical
		aliases[strings.ToUpper(canonical)] = canonical
	}
	if err := scanner.Err(); err != nil {
		panic(err)
	}
	return scripts, aliases
}

func parsePropertyRanges(source []byte) map[string][]runeRange {
	wanted := map[string]bool{
		"White_Space": true, "Hex_Digit": true, "Join_Control": true,
		"Noncharacter_Code_Point": true, "Ideographic": true,
		"Other_Alphabetic": true, "Other_Lowercase": true, "Other_Uppercase": true,
		"Other_ID_Start": true, "Other_ID_Continue": true,
	}
	return parseSemicolonRanges(source, wanted)
}

func parseCaseContextProperties(source []byte) map[string][]runeRange {
	wanted := map[string]bool{
		"Cased":          true,
		"Case_Ignorable": true,
	}
	result := make(map[string][]runeRange, len(wanted))
	scanner := bufio.NewScanner(bytes.NewReader(source))
	for scanner.Scan() {
		line := dataLine(scanner.Text())
		if line == "" || strings.HasPrefix(line, "@missing:") {
			continue
		}
		fields := splitTrimmed(line, ";")
		if len(fields) < 2 {
			panic("malformed DerivedCoreProperties record")
		}
		if wanted[fields[1]] {
			result[fields[1]] = append(result[fields[1]], parseRange(fields[0]))
		}
	}
	if err := scanner.Err(); err != nil {
		panic(err)
	}
	for name, ranges := range result {
		result[name] = mergeRanges(ranges)
	}
	return result
}

func parseEmojiData(source []byte) map[string][]runeRange {
	wanted := map[string]bool{
		"Emoji": true, "Emoji_Presentation": true, "Emoji_Modifier": true,
		"Emoji_Modifier_Base": true, "Emoji_Component": true, "Extended_Pictographic": true,
	}
	return parseSemicolonRanges(source, wanted)
}

func parseSemicolonRanges(source []byte, wanted map[string]bool) map[string][]runeRange {
	result := make(map[string][]runeRange)
	scanner := bufio.NewScanner(bytes.NewReader(source))
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		line := dataLine(scanner.Text())
		if line == "" || strings.HasPrefix(line, "@missing:") {
			continue
		}
		fields := splitTrimmed(line, ";")
		if len(fields) != 2 {
			panic("malformed Unicode range-property record")
		}
		if wanted != nil && !wanted[fields[1]] {
			continue
		}
		result[fields[1]] = append(result[fields[1]], parseRange(fields[0]))
	}
	if err := scanner.Err(); err != nil {
		panic(err)
	}
	for name, ranges := range result {
		result[name] = mergeRanges(ranges)
	}
	return result
}

func buildCompositeProperties(categories map[string][]runeRange, assigned []runeRange, source map[string][]runeRange) map[string][]runeRange {
	result := make(map[string][]runeRange, len(source)+24)
	for name, ranges := range source {
		result[strings.ToUpper(name)] = ranges
	}
	result["ASSIGNED"] = assigned
	result["CONTROL"] = categories["Cc"]
	result["LETTER"] = categories["L"]
	result["TITLECASE"] = categories["Lt"]
	result["PUNCTUATION"] = categories["P"]
	result["DIGIT"] = categories["Nd"]
	result["ALPHABETIC"] = unionRanges(categories["L"], categories["Nl"], source["Other_Alphabetic"])
	result["LOWERCASE"] = unionRanges(categories["Ll"], source["Other_Lowercase"])
	result["UPPERCASE"] = unionRanges(categories["Lu"], source["Other_Uppercase"])
	result["CASED"] = unionRanges(result["LOWERCASE"], result["UPPERCASE"], categories["Lt"])
	result["HEXDIGIT"] = unionRanges(categories["Nd"], source["Hex_Digit"])
	result["HEX_DIGIT"] = result["HEXDIGIT"]
	result["WORD"] = unionRanges(result["ALPHABETIC"], categories["M"], categories["Nd"], categories["Pc"], source["Join_Control"])
	result["ALNUM"] = unionRanges(result["ALPHABETIC"], categories["Nd"])
	result["BLANK"] = unionRanges(categories["Zs"], []runeRange{{lo: '\t', hi: '\t'}})
	result["GRAPH"] = complementRanges(unionRanges(categories["Z"], categories["Cc"], categories["Cs"], categories["Cn"]))
	result["PRINT"] = complementRanges(unionRanges(categories["Cc"], categories["Cs"], categories["Cn"], categories["Zl"], categories["Zp"]))
	result["JAVA_IDENTIFIER_START"] = unionRanges(categories["L"], categories["Nl"], categories["Sc"], categories["Pc"])
	result["IDENTIFIER_IGNORABLE"] = unionRanges(categories["Cf"], []runeRange{{lo: 0, hi: 8}, {lo: 0xe, hi: 0x1b}, {lo: 0x7f, hi: 0x9f}})
	result["JAVA_IDENTIFIER_PART"] = unionRanges(result["JAVA_IDENTIFIER_START"], categories["Nd"], categories["Mc"], categories["Mn"], result["IDENTIFIER_IGNORABLE"])
	result["UNICODE_IDENTIFIER_START"] = unionRanges(categories["L"], categories["Nl"], source["Other_ID_Start"])
	result["UNICODE_IDENTIFIER_PART"] = unionRanges(result["UNICODE_IDENTIFIER_START"], categories["Pc"], categories["Nd"], categories["Mc"], categories["Mn"], result["IDENTIFIER_IGNORABLE"], source["Other_ID_Continue"])
	result["SPACECHAR"] = categories["Z"]
	result["ISOCONTROL"] = []runeRange{{lo: 0, hi: 0x1f}, {lo: 0x7f, hi: 0x9f}}
	result["JAVA_WHITESPACE"] = []runeRange{{lo: 0x9, hi: 0xd}, {lo: 0x1c, hi: 0x20}, {lo: 0x1680, hi: 0x1680}, {lo: 0x2000, hi: 0x2006}, {lo: 0x2008, hi: 0x200a}, {lo: 0x2028, hi: 0x2029}, {lo: 0x205f, hi: 0x205f}, {lo: 0x3000, hi: 0x3000}}
	return result
}

func parseFullUpperMappings(source []byte, simpleUpper map[rune]rune) map[rune]string {
	return parseFullCaseMappings(source, simpleUpper, 3)
}

func parseFullLowerMappings(source []byte, simpleLower map[rune]rune) map[rune]string {
	return parseFullCaseMappings(source, simpleLower, 1)
}

func parseFullCaseMappings(source []byte, simple map[rune]rune, field int) map[rune]string {
	result := make(map[rune]string, len(simple)+128)
	for from, to := range simple {
		result[from] = string(to)
	}
	scanner := bufio.NewScanner(bytes.NewReader(source))
	for scanner.Scan() {
		line := dataLine(scanner.Text())
		if line == "" {
			continue
		}
		fields := splitTrimmed(line, ";")
		if len(fields) < 5 {
			panic("malformed SpecialCasing record")
		}
		if fields[4] != "" {
			// Locale- and context-sensitive entries do not apply to the
			// unconditional Locale.ROOT per-code-point mapping.
			continue
		}
		from := parseCodePoint(fields[0])
		mapped := parseCodePointSequence(fields[field])
		if mapped != string(from) {
			result[from] = mapped
		}
	}
	if err := scanner.Err(); err != nil {
		panic(err)
	}
	return result
}

func generateMainSource(data unicodeDataTables, blocks []unicodeBlock, blockAliases map[string]int, scripts map[string][]runeRange, scriptAliases map[string]string, properties map[string][]runeRange, fullUpper, fullLower map[rune]string, caseContext map[string][]runeRange) []byte {
	var out bytes.Buffer
	emitHeader(&out, "opfor")
	fmt.Fprintf(&out, "const javaRegexUnicodeVersion = %q\n", unicodeVersion)
	fmt.Fprintf(&out, "const javaRegexUnicodeDataSHA256 = %q\n", unicodeDataSHA256)
	fmt.Fprintf(&out, "const javaRegexBlocksSHA256 = %q\n", blocksSHA256)
	fmt.Fprintf(&out, "const javaRegexScriptsSHA256 = %q\n", scriptsSHA256)
	fmt.Fprintf(&out, "const javaRegexPropListSHA256 = %q\n", propListSHA256)
	fmt.Fprintf(&out, "const javaRegexPropertyValueAliasesSHA256 = %q\n", propertyValueAliasesSHA256)
	fmt.Fprintf(&out, "const javaRegexSpecialCasingSHA256 = %q\n", specialCasingSHA256)
	fmt.Fprintf(&out, "const javaStringDerivedCorePropertiesSHA256 = %q\n", derivedCorePropertiesSHA256)
	fmt.Fprintf(&out, "const javaRegexEmojiDataSHA256 = %q\n\n", emojiDataSHA256)
	emitRanges(&out, "javaRegexMirroredRanges", data.mirrored)
	emitRanges(&out, "javaRegexAssignedRanges", data.assigned)
	emitRangeMap(&out, "javaRegexCategoryRanges", data.categories)
	emitRangeMap(&out, "javaRegexScriptRanges", scripts)
	emitStringMap(&out, "javaRegexScriptAliases", scriptAliases)
	emitRangeMap(&out, "javaRegexPropertyRanges", properties)

	fmt.Fprintln(&out, "var javaRegexUnicodeBlocks = []javaRegexUnicodeBlock{")
	for _, block := range blocks {
		fmt.Fprintf(&out, "{lo: 0x%x, hi: 0x%x, javaName: %q},\n", block.lo, block.hi, block.javaName)
	}
	fmt.Fprintln(&out, "}")
	emitIntMap(&out, "javaRegexUnicodeBlockAliases", blockAliases)

	fmt.Fprintln(&out, "var javaRegexRootUpperMappings = []javaRegexFullCaseMapping{")
	upperKeys := sortedRuneKeys(fullUpper)
	for _, cp := range upperKeys {
		fmt.Fprintf(&out, "{from: 0x%x, to: %q},\n", cp, fullUpper[cp])
	}
	fmt.Fprintln(&out, "}")
	fmt.Fprintln(&out, "var javaStringRootLowerMappings = []javaRegexFullCaseMapping{")
	lowerKeys := sortedRuneKeys(fullLower)
	for _, cp := range lowerKeys {
		fmt.Fprintf(&out, "{from: 0x%x, to: %q},\n", cp, fullLower[cp])
	}
	fmt.Fprintln(&out, "}")
	emitSimpleCaseMap(&out, "javaStringSimpleUpperMappings", data.upper)
	emitSimpleCaseMap(&out, "javaStringSimpleLowerMappings", data.lower)
	emitRanges(&out, "javaStringCasedRanges", caseContext["Cased"])
	emitRanges(&out, "javaStringCaseIgnorableRanges", caseContext["Case_Ignorable"])

	compressed := compressNames(data.names)
	fmt.Fprintln(&out, "const javaRegexCharacterNamesZlibBase64 = `")
	encoded := base64.StdEncoding.EncodeToString(compressed)
	for len(encoded) > 0 {
		width := min(100, len(encoded))
		fmt.Fprintln(&out, encoded[:width])
		encoded = encoded[width:]
	}
	fmt.Fprintln(&out, "`")
	return mustFormat(out.Bytes())
}

func generateCaseSource(upper, lower map[rune]rune) []byte {
	var out bytes.Buffer
	emitHeader(&out, "syntax")
	fmt.Fprintf(&out, "const javaUnicodeDataVersion = %q\n", unicodeVersion)
	fmt.Fprintf(&out, "const javaUnicodeDataSHA256 = %q\n\n", unicodeDataSHA256)
	emitCaseMap(&out, "javaUnicode17UpperMappings", upper)
	emitCaseMap(&out, "javaUnicode17LowerMappings", lower)
	return mustFormat(out.Bytes())
}

func emitHeader(out *bytes.Buffer, packageName string) {
	fmt.Fprintln(out, "// Code generated by regex_java_tablegen.go; DO NOT EDIT.")
	fmt.Fprintln(out, "//")
	fmt.Fprintln(out, "// Unicode data copyright © 1991-2025 Unicode, Inc. and is")
	fmt.Fprintln(out, "// distributed under the Unicode License v3. See")
	fmt.Fprintln(out, "// https://www.unicode.org/license.txt.")
	fmt.Fprintln(out, "//")
	fmt.Fprintln(out, "// Generated exclusively from authenticated Unicode 17.0.0 UCD inputs.")
	fmt.Fprintln(out, "// OpenJDK is a behavioral oracle only; no OpenJDK table is an input.")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "package %s\n\n", packageName)
}

func emitSimpleCaseMap(out *bytes.Buffer, name string, values map[rune]rune) {
	fmt.Fprintf(out, "var %s = []javaStringSimpleCaseMapping{\n", name)
	for _, from := range sortedRuneKeys(values) {
		fmt.Fprintf(out, "{from: 0x%x, to: 0x%x},\n", from, values[from])
	}
	fmt.Fprintln(out, "}")
}

func emitRanges(out *bytes.Buffer, name string, ranges []runeRange) {
	fmt.Fprintf(out, "var %s = []javaRegexRuneRange{\n", name)
	for _, current := range ranges {
		fmt.Fprintf(out, "{lo: 0x%x, hi: 0x%x},\n", current.lo, current.hi)
	}
	fmt.Fprintln(out, "}")
}

func emitRangeMap(out *bytes.Buffer, name string, values map[string][]runeRange) {
	fmt.Fprintf(out, "var %s = map[string][]javaRegexRuneRange{\n", name)
	for _, key := range sortedStringKeys(values) {
		fmt.Fprintf(out, "%q: {\n", key)
		for _, current := range values[key] {
			fmt.Fprintf(out, "{lo: 0x%x, hi: 0x%x},\n", current.lo, current.hi)
		}
		fmt.Fprintln(out, "},")
	}
	fmt.Fprintln(out, "}")
}

func emitStringMap(out *bytes.Buffer, name string, values map[string]string) {
	fmt.Fprintf(out, "var %s = map[string]string{\n", name)
	for _, key := range sortedStringKeys(values) {
		fmt.Fprintf(out, "%q: %q,\n", key, values[key])
	}
	fmt.Fprintln(out, "}")
}

func emitIntMap(out *bytes.Buffer, name string, values map[string]int) {
	fmt.Fprintf(out, "var %s = map[string]int{\n", name)
	for _, key := range sortedStringKeys(values) {
		fmt.Fprintf(out, "%q: %d,\n", key, values[key])
	}
	fmt.Fprintln(out, "}")
}

func emitCaseMap(out *bytes.Buffer, name string, values map[rune]rune) {
	fmt.Fprintf(out, "var %s = []javaUnicode17CaseMapping{\n", name)
	for _, key := range sortedRuneKeys(values) {
		fmt.Fprintf(out, "{from: 0x%x, to: 0x%x},\n", key, values[key])
	}
	fmt.Fprintln(out, "}")
}

func mustFormat(source []byte) []byte {
	formatted, err := format.Source(source)
	if err != nil {
		panic(fmt.Sprintf("format generated source: %v\n%s", err, source))
	}
	return formatted
}

func compressNames(names []nameRecord) []byte {
	var plain bytes.Buffer
	if err := binary.Write(&plain, binary.BigEndian, uint32(len(names))); err != nil {
		panic(err)
	}
	for _, current := range names {
		if len(current.name) > 0xffff {
			panic("Unicode character name is too long")
		}
		_ = binary.Write(&plain, binary.BigEndian, uint32(current.cp))
		_ = binary.Write(&plain, binary.BigEndian, uint16(len(current.name)))
		plain.WriteString(current.name)
	}
	var compressed bytes.Buffer
	writer, err := zlib.NewWriterLevel(&compressed, zlib.BestCompression)
	if err != nil {
		panic(err)
	}
	if _, err := writer.Write(plain.Bytes()); err != nil {
		panic(err)
	}
	if err := writer.Close(); err != nil {
		panic(err)
	}
	return compressed.Bytes()
}

func parseCodePoint(value string) rune {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 16, 32)
	if err != nil || parsed < 0 || parsed > 0x10ffff {
		panic(fmt.Sprintf("invalid Unicode code point %q", value))
	}
	return rune(parsed)
}

func parseCodePointSequence(value string) string {
	fields := strings.Fields(value)
	var result strings.Builder
	for _, field := range fields {
		result.WriteRune(parseCodePoint(field))
	}
	return result.String()
}

func parseRange(value string) runeRange {
	parts := strings.Split(value, "..")
	if len(parts) > 2 {
		panic("invalid Unicode range")
	}
	first := parseCodePoint(parts[0])
	last := first
	if len(parts) == 2 {
		last = parseCodePoint(parts[1])
	}
	if first > last {
		panic("reversed Unicode range")
	}
	return runeRange{lo: first, hi: last}
}

func dataLine(value string) string {
	return strings.TrimSpace(strings.SplitN(value, "#", 2)[0])
}

func splitTrimmed(value, separator string) []string {
	fields := strings.Split(value, separator)
	for index := range fields {
		fields[index] = strings.TrimSpace(fields[index])
	}
	return fields
}

func unionNamedRanges(values map[string][]runeRange, names ...string) []runeRange {
	all := make([][]runeRange, 0, len(names))
	for _, name := range names {
		all = append(all, values[name])
	}
	return unionRanges(all...)
}

func unionRanges(groups ...[]runeRange) []runeRange {
	var result []runeRange
	for _, ranges := range groups {
		result = append(result, ranges...)
	}
	return mergeRanges(result)
}

func mergeRanges(ranges []runeRange) []runeRange {
	ranges = append([]runeRange(nil), ranges...)
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].lo != ranges[j].lo {
			return ranges[i].lo < ranges[j].lo
		}
		return ranges[i].hi < ranges[j].hi
	})
	merged := make([]runeRange, 0, len(ranges))
	for _, current := range ranges {
		if current.lo < 0 || current.hi > 0x10ffff || current.lo > current.hi {
			panic("invalid Unicode range")
		}
		if len(merged) > 0 && current.lo <= merged[len(merged)-1].hi+1 {
			if current.hi > merged[len(merged)-1].hi {
				merged[len(merged)-1].hi = current.hi
			}
			continue
		}
		merged = append(merged, current)
	}
	return merged
}

func complementRanges(ranges []runeRange) []runeRange {
	ranges = mergeRanges(ranges)
	result := make([]runeRange, 0, len(ranges)+1)
	next := rune(0)
	for _, current := range ranges {
		if next < current.lo {
			result = append(result, runeRange{lo: next, hi: current.lo - 1})
		}
		if current.hi == 0x10ffff {
			return result
		}
		next = current.hi + 1
	}
	if next <= 0x10ffff {
		result = append(result, runeRange{lo: next, hi: 0x10ffff})
	}
	return result
}

func sortedStringKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedRuneKeys[V any](values map[rune]V) []rune {
	keys := make([]rune, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}
