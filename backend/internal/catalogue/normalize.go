package catalogue

import "strings"

// variantCanonical is the small, hand-reviewed, EXACT map of raw rank-name
// spellings (as they appear in the source CSV, after only trimming +
// uppercasing + whitespace-collapsing — see Normalize) to one canonical
// form.
//
// How this list was built (fully reproducible, see docs/PROGRESS.md's "Real
// route catalogue import" entry): every unique rank name in the source CSV
// was grouped by a punctuation/whitespace-stripped key (uppercase, strip
// every non-alphanumeric character, keep word order). Exactly 24 groups
// came out with more than one raw spelling. Every one of them was hand
// -reviewed and confirmed to be a TRUE punctuation/spacing/apostrophe
// variant of the SAME physical rank, in the SAME word order — never two
// genuinely different ranks, and never a reordering of words (which would
// be a much bigger, unverifiable leap). This map folds exactly those 24
// groups and nothing else.
//
// Deliberately NOT folded (kept as distinct ranks, per the conservative
// rule "when in doubt, keep separate"):
//   - "KHAYELITSHA", "KHAYELITSHA SITE C", "KHAYELITSHA (SITE B)",
//     "KHAYELITSHA (MAKHAZA)", "KHAYELITSHA (HARARE)", "KHAYELITSHA
//     (KUWAIT)", "KHAYELITSHA (TOWN 2)", "KHAYELITSHA (LUZUKO RANK)",
//     "KHAYELITSHA (VIA MAITLAND & GOODWOOD)", "KHAYELITSHA (VIA
//     MELKBOSSTRAND)" — these share a prefix but name distinct ranks/areas
//     within Khayelitsha, not spelling variants of one place.
//   - "MITCHELLS PLAIN (TOWN CENTRE)" vs "TOWN CENTRE, MITCHELLS PLAIN" —
//     same two words in reversed order across different source rows; folding
//     across word order was judged too big a leap for an auditable,
//     mechanical rule, so these remain two distinct canonical names (each
//     internally folded for its own punctuation variants only).
//   - "PAROW SANLAM CENTRE" vs "SANLAM CENTRE, PAROW" — same reasoning,
//     reversed word order, kept distinct.
var variantCanonical = map[string]string{
	// Apostrophe variants (the brief's own example).
	"MITCHELL'S PLAIN": "MITCHELLS PLAIN",
	"SIR LOWRY'S PASS": "SIR LOWRYS PASS",

	// Pure spacing/joining variants of one place name.
	"BLOUBERG STRAND":    "BLOUBERGSTRAND",
	"EERSTERIVER":        "EERSTE RIVER",
	"HOUTBAY":            "HOUT BAY",
	"LOWER CROSSROADS":   "LOWER CROSS ROADS",
	"MELKBOS STRAND":     "MELKBOSSTRAND",
	"SAXON SEA ATLANTIS": "SAXONSEA ATLANTIS",
	"SUMMERGREENS":       "SUMMER GREENS",
	"TABLEVIEW":          "TABLE VIEW",

	// Parenthetical/comma qualifier formatting variants — same rank + same
	// sub-location qualifier + same word order, just punctuated differently
	// across source rows.
	"CAPE TOWN STATION DECK":          "CAPE TOWN (STATION DECK)",
	"CAPE TOWN(STATION DECK)":         "CAPE TOWN (STATION DECK)",
	"CAPE TOWN (VIA MELKBOS STRAND)":  "CAPE TOWN (VIA MELKBOSSTRAND)",
	"CLAREMONT(VIA WYNBERG)":          "CLAREMONT (VIA WYNBERG)",
	"CROSS ROADS(JO-BURG STORES)":     "CROSS ROADS (JO-BURG STORES)",
	"EINDHOVEN DELFT":                 "EINDHOVEN, DELFT",
	"EINDHOVEN,DELFT":                 "EINDHOVEN, DELFT",
	"FABRIEKS AREA ATLANTIS":          "FABRIEKS AREA, ATLANTIS",
	"FABRIEKS AREA,ATLANTIS":          "FABRIEKS AREA, ATLANTIS",
	"KRAMAT WAY(MACASSAR)":            "KRAMAT WAY (MACASSAR)",
	"KRAMAT WAY,MACASSAR":             "KRAMAT WAY (MACASSAR)",
	"LEIDEN DELFT":                    "LEIDEN, DELFT",
	"LEIDEN,DELFT":                    "LEIDEN, DELFT",
	"LOURENSFORD(SOMERSET WEST)":      "LOURENSFORD (SOMERSET WEST)",
	"LOURENSFORD,SOMERSET WEST":       "LOURENSFORD (SOMERSET WEST)",
	"METRO INDUSTRY(PAARDEN EILAND)":  "METRO INDUSTRY (PAARDEN EILAND)",
	"MITCHELLS PLAIN(CALYPSO SQUARE)": "MITCHELLS PLAIN (CALYPSO SQUARE)",
	"MITCHELLS PLAIN - TOWN CENTRE":   "MITCHELLS PLAIN (TOWN CENTRE)",
	"MITCHELLS PLAIN(TOWN CENTRE)":    "MITCHELLS PLAIN (TOWN CENTRE)",
	"SANLAM CENTRE ,PAROW":            "SANLAM CENTRE, PAROW",
	"SANLAM CENTRE PAROW":             "SANLAM CENTRE, PAROW",
	"SANLAM CENTRE,PAROW":             "SANLAM CENTRE, PAROW",
	"TOWN CENTRE MITCHELLS PLAIN":     "TOWN CENTRE, MITCHELLS PLAIN",
	"TOWN CENTRE(MITCHELLS PLAIN)":    "TOWN CENTRE, MITCHELLS PLAIN",
	"TOWN CENTRE,MITCHELLS PLAIN":     "TOWN CENTRE, MITCHELLS PLAIN",
}

// Normalize canonicalizes one raw rank name: trim, uppercase (source data is
// already all-caps; this just guarantees it), collapse any run of internal
// whitespace to a single space, then fold through variantCanonical if the
// resulting exact spelling is a known variant. Anything not an exact match
// in variantCanonical passes through completely unchanged — an unfamiliar
// rank name is never guessed at or merged with another.
func Normalize(raw string) string {
	s := strings.ToUpper(strings.Join(strings.Fields(raw), " "))
	if canonical, ok := variantCanonical[s]; ok {
		return canonical
	}
	return s
}
