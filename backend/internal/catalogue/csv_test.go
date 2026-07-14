package catalogue

import (
	"strings"
	"testing"
)

func TestParseCSV_QuotedCommasAndBOM(t *testing.T) {
	// A BOM prefix (matches the real source file — built from raw bytes
	// rather than a literal BOM rune, which Go's parser rejects mid-source),
	// a quoted field containing a literal comma, and one deliberately blank
	// ORGN row.
	bom := string([]byte{0xEF, 0xBB, 0xBF})
	data := bom + "OBJECTID,ORGN,DSTN,SHAPE_Length\n" +
		"1,BELLVILLE,DURBANVILLE,12918.668591668\n" +
		"2,PELLA,\"FABRIEKS AREA,ATLANTIS\",16034.6338158755\n" +
		"3,,SOMEWHERE,999.0\n" +
		"4,SOMEWHERE, ,111.0\n"

	rows, stats, err := ParseCSV(strings.NewReader(data))
	if err != nil {
		t.Fatalf("ParseCSV failed: %v", err)
	}

	if stats.TotalDataRows != 4 {
		t.Fatalf("expected 4 total data rows, got %d", stats.TotalDataRows)
	}
	if stats.BlankDropped != 2 {
		t.Fatalf("expected 2 blank rows dropped, got %d", stats.BlankDropped)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 valid rows, got %d: %+v", len(rows), rows)
	}

	if rows[0].Origin != "BELLVILLE" || rows[0].Destination != "DURBANVILLE" {
		t.Errorf("unexpected row 0: %+v", rows[0])
	}
	if rows[0].DistanceMeters != 12918.668591668 {
		t.Errorf("unexpected distance for row 0: %v", rows[0].DistanceMeters)
	}

	// The quoted-comma field must survive intact, not get split into two
	// columns.
	if rows[1].Origin != "PELLA" || rows[1].Destination != "FABRIEKS AREA,ATLANTIS" {
		t.Fatalf("expected the quoted comma to be preserved as one field, got %+v", rows[1])
	}
}

func TestParseCSV_SameOriginDestinationKept(t *testing.T) {
	// A rank-internal loop route (real origin == real destination) is a
	// genuine route in the source data, not a data error — must not be
	// dropped.
	data := "OBJECTID,ORGN,DSTN,SHAPE_Length\n197,RETREAT,RETREAT,4937.34435514872\n"

	rows, stats, err := ParseCSV(strings.NewReader(data))
	if err != nil {
		t.Fatalf("ParseCSV failed: %v", err)
	}
	if stats.BlankDropped != 0 {
		t.Fatalf("expected no rows dropped, got %d", stats.BlankDropped)
	}
	if len(rows) != 1 {
		t.Fatalf("expected the same-origin/destination row to be kept, got %d rows", len(rows))
	}
	if rows[0].Origin != rows[0].Destination {
		t.Fatalf("expected origin == destination to be preserved, got %+v", rows[0])
	}
}

func TestParseCSV_InvalidObjectIDErrors(t *testing.T) {
	data := "OBJECTID,ORGN,DSTN,SHAPE_Length\nNOTANUMBER,A,B,100.0\n"
	if _, _, err := ParseCSV(strings.NewReader(data)); err == nil {
		t.Fatal("expected an error for a non-numeric OBJECTID")
	}
}

func TestParseCSV_TooFewColumnsErrors(t *testing.T) {
	data := "OBJECTID,ORGN,DSTN,SHAPE_Length\n1,A,B\n"
	if _, _, err := ParseCSV(strings.NewReader(data)); err == nil {
		t.Fatal("expected an error for a row with too few columns")
	}
}
