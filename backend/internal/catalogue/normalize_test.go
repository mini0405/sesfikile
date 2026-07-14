package catalogue

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestNormalize_FoldsKnownVariants confirms every entry in variantCanonical
// actually folds to its documented canonical form.
func TestNormalize_FoldsKnownVariants(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"MITCHELL'S PLAIN", "MITCHELLS PLAIN"},
		{"Mitchell's Plain", "MITCHELLS PLAIN"}, // case-insensitivity
		{"SIR LOWRY'S PASS", "SIR LOWRYS PASS"},
		{"BLOUBERG STRAND", "BLOUBERGSTRAND"},
		{"HOUTBAY", "HOUT BAY"},
		{"TABLEVIEW", "TABLE VIEW"},
		{"EINDHOVEN,DELFT", "EINDHOVEN, DELFT"},
		{"EINDHOVEN DELFT", "EINDHOVEN, DELFT"},
		{"SANLAM CENTRE ,PAROW", "SANLAM CENTRE, PAROW"},
		{"SANLAM CENTRE,PAROW", "SANLAM CENTRE, PAROW"},
		{"CAPE TOWN(STATION DECK)", "CAPE TOWN (STATION DECK)"},
	}
	for _, c := range cases {
		if got := Normalize(c.raw); got != c.want {
			t.Errorf("Normalize(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

// TestNormalize_KeepsDistinctPlacesDistinct is the conservative-folding
// guarantee the brief calls out explicitly: ranks that merely share a
// prefix or substring must NOT be merged.
func TestNormalize_KeepsDistinctPlacesDistinct(t *testing.T) {
	distinctGroups := [][]string{
		{"KHAYELITSHA", "KHAYELITSHA SITE C", "KHAYELITSHA (MAKHAZA)", "KHAYELITSHA (SITE B)"},
		{"MITCHELLS PLAIN (TOWN CENTRE)", "TOWN CENTRE, MITCHELLS PLAIN"}, // reversed word order, not folded
		{"CAPE TOWN", "CAPE TOWN (STATION DECK)", "CAPE TOWN (VIA MELKBOSSTRAND)"},
	}
	for _, group := range distinctGroups {
		seen := map[string]string{} // normalized -> first raw that produced it
		for _, raw := range group {
			norm := Normalize(raw)
			if firstRaw, ok := seen[norm]; ok {
				t.Errorf("expected %q and %q to normalize to DIFFERENT canonical names, both got %q", firstRaw, raw, norm)
			}
			seen[norm] = raw
		}
	}
}

// TestNormalize_UnknownNamePassesThroughUnchanged confirms a rank not in
// variantCanonical is only trimmed/uppercased/whitespace-collapsed, never
// guessed at.
func TestNormalize_UnknownNamePassesThroughUnchanged(t *testing.T) {
	if got := Normalize("  Some   Brand New Rank  "); got != "SOME BRAND NEW RANK" {
		t.Errorf("expected pass-through normalization, got %q", got)
	}
}

// TestNormalize_AgainstRealCSV is a live audit of variantCanonical against
// the actual source file: every raw variant spelling in the CSV is grouped
// by a punctuation/whitespace-stripped key (preserving word order), and any
// group with more than one distinct raw spelling must either (a) collapse
// to one canonical name after Normalize, or (b) be a documented,
// deliberately-not-folded exception. This is what keeps the map in
// normalize.go honest against the real data, not just the hand-picked cases
// above.
func TestNormalize_AgainstRealCSV(t *testing.T) {
	f, err := os.Open("../../data/taxi_routes.csv")
	if err != nil {
		t.Skipf("skipping: source CSV not found: %v", err)
	}
	defer f.Close()

	rows, _, err := ParseCSV(f)
	if err != nil {
		t.Fatalf("failed to parse source csv: %v", err)
	}

	raw := map[string]bool{}
	for _, row := range rows {
		raw[strings.ToUpper(strings.Join(strings.Fields(row.Origin), " "))] = true
		raw[strings.ToUpper(strings.Join(strings.Fields(row.Destination), " "))] = true
	}

	// Deliberately-not-folded exceptions — see normalize.go's doc comment.
	// A group whose members are ALL listed here (regardless of order) is
	// allowed to remain unfolded.
	allowedUnfolded := map[string]bool{}
	for _, group := range [][]string{
		{"MITCHELLS PLAIN (TOWN CENTRE)", "TOWN CENTRE, MITCHELLS PLAIN"},
		{"PAROW SANLAM CENTRE", "SANLAM CENTRE, PAROW"},
	} {
		key := strings.Join(group, "|")
		allowedUnfolded[key] = true
	}

	nonAlnum := regexp.MustCompile(`[^A-Z0-9]+`)
	groups := map[string][]string{}
	for name := range raw {
		key := nonAlnum.ReplaceAllString(name, "")
		groups[key] = append(groups[key], name)
	}

	for _, members := range groups {
		if len(members) < 2 {
			continue
		}
		sort.Strings(members)

		canonical := map[string]bool{}
		for _, m := range members {
			canonical[Normalize(m)] = true
		}
		if len(canonical) == 1 {
			continue // this group folds cleanly to one canonical name — good
		}

		key := strings.Join(members, "|")
		if allowedUnfolded[key] {
			continue // documented exception
		}
		t.Errorf("unfolded raw-spelling group %v normalizes to %d different canonical names %v — "+
			"either add a variantCanonical entry or add it to allowedUnfolded above with a reason",
			members, len(canonical), canonical)
	}
}
