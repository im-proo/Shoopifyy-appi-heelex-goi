package main

import (
	"strings"
	"testing"
)

// Every country carries exactly 3 coherent rows: no "N/A" leaking into
// postal, country code embedded in the row matches the map key, and no
// empty street/city/zone.
func TestBookMultiShape(t *testing.T) {
	if len(BookMulti) != 196 {
		t.Fatalf("BookMulti countries = %d, want 196", len(BookMulti))
	}
	total := 0
	for cc, rows := range BookMulti {
		if len(rows) != 3 {
			t.Errorf("%s: %d rows, want 3", cc, len(rows))
		}
		for i, a := range rows {
			total++
			if a.Address1 == "" || a.City == "" || a.ZoneCode == "" {
				t.Errorf("%s row %d: empty field: %+v", cc, i, a)
			}
			if a.PostalCode == "N/A" {
				t.Errorf("%s row %d: literal N/A postal leaked in", cc, i)
			}
			if a.CountryCode != cc {
				t.Errorf("%s row %d: CountryCode = %s", cc, i, a.CountryCode)
			}
		}
	}
	if total != 588 {
		t.Fatalf("total rows = %d, want 588", total)
	}
}

// Rotation must cycle the 3 rows and stay in-country.
func TestAddrForRotation(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 6; i++ {
		a, ok := addrFor("DE")
		if !ok {
			t.Fatal("DE miss")
		}
		if a.CountryCode != "DE" {
			t.Fatalf("DE rotation returned %s", a.CountryCode)
		}
		seen[a.Address1] = true
	}
	if len(seen) != 3 {
		t.Errorf("rotation saw %d distinct DE streets in 6 picks, want 3", len(seen))
	}
	// Legacy-only country falls back to the single-row Book.
	if a, ok := addrFor("HK"); !ok || a.CountryCode != "HK" {
		t.Errorf("HK fallback failed: %+v ok=%v", a, ok)
	}
	// Unknown country signals a miss (caller uses DEFAULT).
	if _, ok := addrFor("ZZ"); ok {
		t.Error("ZZ should miss")
	}
}

// All 196 book countries must be phone-capable: dial code, national
// length and carrier prefixes present, and randomizedPhones must return
// a non-empty E.164 number (rows ship with an empty Phone field).
func TestPhoneCoverageAllBookCountries(t *testing.T) {
	for cc := range BookMulti {
		if _, ok := dialCodes[cc]; !ok {
			t.Errorf("%s: dialCodes missing", cc)
			continue
		}
		if _, ok := nationalLengths[cc]; !ok {
			t.Errorf("%s: nationalLengths missing", cc)
		}
		if _, ok := mobilePrefixList[cc]; !ok {
			t.Errorf("%s: mobilePrefixList missing", cc)
		}
		// Use a real rotating row (empty Phone field).
		rows := BookMulti[cc]
		e164, local, raw := randomizedPhones(rows[0])
		if e164 == "" || local == "" || raw == "" {
			t.Errorf("%s: randomizedPhones returned empty (%q %q %q)", cc, e164, local, raw)
			continue
		}
		if !strings.HasPrefix(e164, "+") || len(e164) < 4 {
			t.Errorf("%s: bad e164 %q", cc, e164)
		}
		if want := dialCodes[cc]; !strings.HasPrefix(e164[1:], want) {
			t.Errorf("%s: e164 %q does not start with dial %s", cc, e164, want)
		}
	}
}

// Selection entry points must resolve real countries and keep the
// DEFAULT fallback behavior for unknown inputs.
func TestPickFunctions(t *testing.T) {
	if a, ok := pickAddrByCurrency("EUR"); !ok || a.CountryCode != "DE" {
		t.Errorf("EUR -> %+v ok=%v, want DE", a, ok)
	}
	if a, ok := pickAddrByCurrency("XYZ"); ok || a.CountryCode != "US" {
		t.Errorf("XYZ -> %+v ok=%v, want DEFAULT US with ok=false", a, ok)
	}
	if a, ok := pickAddrByURL("https://shop.example.co.uk"); !ok || a.CountryCode != "GB" {
		t.Errorf("co.uk -> %+v ok=%v, want GB", a, ok)
	}
	if a, ok := pickAddrByURL("https://shop.example.de"); !ok || a.CountryCode != "DE" {
		t.Errorf(".de -> %+v ok=%v, want DE", a, ok)
	}
	if a, ok := pickAddrByURL("https://x.myshopify.com"); ok || a.CountryCode != "US" {
		t.Errorf("myshopify -> %+v ok=%v, want DEFAULT US with ok=false", a, ok)
	}
	// Postal-N/A country must round-trip blank, not "N/A".
	if a, ok := pickAddrByCurrency("AED"); !ok || a.PostalCode != "" {
		t.Errorf("AED postal = %q ok=%v, want blank", a.PostalCode, ok)
	}
}
