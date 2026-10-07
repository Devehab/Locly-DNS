package hosts

import (
	"net/netip"
	"strings"
	"testing"
)

const stock = "127.0.0.1\tlocalhost\n255.255.255.255\tbroadcasthost\n::1             localhost\n\n# my stuff\n10.0.0.5 nas.lan nas\n"

func ip(s string) netip.Addr { return netip.MustParseAddr(s) }

func TestParseNoSection(t *testing.T) {
	d := Parse([]byte(stock))
	if d.Err() != nil {
		t.Fatalf("unexpected error: %v", d.Err())
	}
	if d.HasSection() || len(d.Entries()) != 0 {
		t.Fatalf("expected no section")
	}
	if got := string(d.Bytes()); got != stock {
		t.Fatalf("round trip changed the file:\n%q\n%q", stock, got)
	}
	if m, ok := d.Lookup("nas"); !ok || m.IP != ip("10.0.0.5") || m.Managed {
		t.Fatalf("Lookup(nas) = %+v, %v", m, ok)
	}
}

func TestAddSectionPreservesEverythingElse(t *testing.T) {
	d := Parse([]byte(stock))
	if err := d.SetEntries([]Entry{{ip("127.0.0.1"), "app.local"}, {ip("192.168.1.60"), "ha.local"}}); err != nil {
		t.Fatal(err)
	}
	want := stock + "# BEGIN LOCALDNS\n127.0.0.1 app.local\n192.168.1.60 ha.local\n# END LOCALDNS\n"
	if got := string(d.Bytes()); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}

	// Parse again: the section is recognized and the user's lines are intact.
	d2 := Parse([]byte(want))
	if d2.Err() != nil || !d2.HasSection() || len(d2.Entries()) != 2 {
		t.Fatalf("reparse failed: %v %v %d", d2.Err(), d2.HasSection(), len(d2.Entries()))
	}
	if !strings.HasPrefix(string(d2.Bytes()), stock) {
		t.Fatal("content before the section changed")
	}
}

func TestRemovingAllEntriesRestoresOriginalBytes(t *testing.T) {
	d := Parse([]byte(stock))
	_ = d.SetEntries([]Entry{{ip("127.0.0.1"), "app.local"}})
	d = Parse(d.Bytes())
	if err := d.SetEntries(nil); err != nil {
		t.Fatal(err)
	}
	if got := string(d.Bytes()); got != stock {
		t.Fatalf("expected original bytes after removing the section:\n%q\ngot:\n%q", stock, got)
	}
}

func TestSectionInTheMiddleKeepsPosition(t *testing.T) {
	in := "127.0.0.1 localhost\n# BEGIN LOCALDNS\n127.0.0.1 old.local\n# END LOCALDNS\n10.0.0.1 after.lan\n"
	d := Parse([]byte(in))
	_ = d.SetEntries([]Entry{{ip("127.0.0.1"), "new.local"}})
	want := "127.0.0.1 localhost\n# BEGIN LOCALDNS\n127.0.0.1 new.local\n# END LOCALDNS\n10.0.0.1 after.lan\n"
	if got := string(d.Bytes()); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCRLFIsPreserved(t *testing.T) {
	in := "# Copyright (c) 1993-2009 Microsoft Corp.\r\n127.0.0.1       localhost\r\n::1             localhost\r\n"
	d := Parse([]byte(in))
	_ = d.SetEntries([]Entry{{ip("127.0.0.1"), "app.local"}})
	want := in + "# BEGIN LOCALDNS\r\n127.0.0.1 app.local\r\n# END LOCALDNS\r\n"
	if got := string(d.Bytes()); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	d2 := Parse([]byte(want))
	if d2.Err() != nil || len(d2.Entries()) != 1 {
		t.Fatalf("reparse CRLF: %v", d2.Err())
	}
}

func TestMissingTrailingNewline(t *testing.T) {
	in := "127.0.0.1 localhost"
	d := Parse([]byte(in))
	_ = d.SetEntries([]Entry{{ip("127.0.0.1"), "app.local"}})
	want := "127.0.0.1 localhost\n# BEGIN LOCALDNS\n127.0.0.1 app.local\n# END LOCALDNS\n"
	if got := string(d.Bytes()); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestEmptyFile(t *testing.T) {
	d := Parse(nil)
	_ = d.SetEntries([]Entry{{ip("127.0.0.1"), "app.local"}})
	want := "# BEGIN LOCALDNS\n127.0.0.1 app.local\n# END LOCALDNS\n"
	if got := string(d.Bytes()); got != want {
		t.Fatalf("got %q", got)
	}
}

func TestTolerantMarkersAndComments(t *testing.T) {
	in := "127.0.0.1 localhost\n#begin   localdns\n\n# a note\n127.0.0.1 app.local # inline\n::1 v6.local\n  # End LocalDNS  \n"
	d := Parse([]byte(in))
	if d.Err() != nil {
		t.Fatal(d.Err())
	}
	got := d.Entries()
	if len(got) != 2 || got[0].Hostname != "app.local" || got[1].IP != ip("::1") {
		t.Fatalf("entries = %+v", got)
	}
}

func TestMultipleNamesPerLineInSection(t *testing.T) {
	d := Parse([]byte("# BEGIN LOCALDNS\n127.0.0.1 a.local B.local\n# END LOCALDNS\n"))
	if d.Err() != nil {
		t.Fatal(d.Err())
	}
	if e, ok := d.Entry("b.local"); !ok || e.IP != ip("127.0.0.1") {
		t.Fatalf("b.local not parsed: %+v", d.Entries())
	}
}

func TestMalformedSections(t *testing.T) {
	tests := map[string]struct {
		in      string
		markers bool
	}{
		"begin without end":  {"# BEGIN LOCALDNS\n127.0.0.1 a.local\n", true},
		"end without begin":  {"127.0.0.1 a.local\n# END LOCALDNS\n", true},
		"two sections":       {"# BEGIN LOCALDNS\n# END LOCALDNS\n# BEGIN LOCALDNS\n# END LOCALDNS\n", true},
		"nested begin":       {"# BEGIN LOCALDNS\n# BEGIN LOCALDNS\n# END LOCALDNS\n", true},
		"bad ip":             {"# BEGIN LOCALDNS\nnot-an-ip a.local\n# END LOCALDNS\n", false},
		"ip without name":    {"# BEGIN LOCALDNS\n127.0.0.1\n# END LOCALDNS\n", false},
		"bad hostname":       {"# BEGIN LOCALDNS\n127.0.0.1 bad_name.local\n# END LOCALDNS\n", false},
		"duplicate hostname": {"# BEGIN LOCALDNS\n127.0.0.1 a.local\n10.0.0.1 a.local\n# END LOCALDNS\n", false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			d := Parse([]byte(tt.in))
			err := d.Err()
			if err == nil {
				t.Fatal("expected an error")
			}
			if err.Markers != tt.markers {
				t.Fatalf("Markers = %v, want %v (%v)", err.Markers, tt.markers, err)
			}
			if err.Line == 0 {
				t.Errorf("error should point at a line: %v", err)
			}
			// A malformed section must never be rewritten...
			if serr := d.SetEntries([]Entry{{ip("127.0.0.1"), "x.local"}}); serr == nil {
				t.Fatal("SetEntries should refuse a malformed section")
			}
			// ...and when the markers are broken the content is untouched.
			if tt.markers {
				if got := string(d.Bytes()); got != tt.in {
					t.Fatalf("Bytes changed a file with broken markers: %q", got)
				}
				if d.RemoveSection() == nil {
					t.Fatal("RemoveSection must refuse broken markers")
				}
			}
		})
	}
}

func TestRemoveSectionWithInvalidContent(t *testing.T) {
	in := "127.0.0.1 localhost\n# BEGIN LOCALDNS\ngarbage line here\n# END LOCALDNS\n10.0.0.1 keep.lan\n"
	d := Parse([]byte(in))
	if d.Err() == nil || d.Err().Markers {
		t.Fatalf("expected content error, got %v", d.Err())
	}
	if err := d.RemoveSection(); err != nil {
		t.Fatal(err)
	}
	want := "127.0.0.1 localhost\n10.0.0.1 keep.lan\n"
	if got := string(d.Bytes()); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestUnmanagedLinesAreIgnoredEvenIfOdd(t *testing.T) {
	// Garbage outside the section is the user's business, not an error.
	in := "this is not a hosts line\n127.0.0.1 localhost\n"
	d := Parse([]byte(in))
	if d.Err() != nil {
		t.Fatal(d.Err())
	}
	_ = d.SetEntries([]Entry{{ip("127.0.0.1"), "a.local"}})
	if !strings.HasPrefix(string(d.Bytes()), in) {
		t.Fatal("unmanaged content changed")
	}
}

func TestLookupFirstMatchWinsAndUnmanaged(t *testing.T) {
	in := "10.0.0.9 app.local\n# BEGIN LOCALDNS\n127.0.0.1 app.local\n# END LOCALDNS\n"
	d := Parse([]byte(in))
	m, ok := d.Lookup("APP.local")
	if !ok || m.Managed || m.IP != ip("10.0.0.9") || m.Line != 1 {
		t.Fatalf("Lookup = %+v", m)
	}
	if un := d.Unmanaged("app.local"); len(un) != 1 || un[0].Line != 1 {
		t.Fatalf("Unmanaged = %+v", un)
	}
	if _, ok := d.Lookup("google.com"); ok {
		t.Fatal("google.com must not be found in the hosts file")
	}
}

func TestSetEntriesValidation(t *testing.T) {
	d := Parse(nil)
	if err := d.SetEntries([]Entry{{ip("127.0.0.1"), "a.local"}, {ip("127.0.0.1"), "a.local"}}); err == nil {
		t.Fatal("duplicates must be refused")
	}
	if err := d.SetEntries([]Entry{{netip.Addr{}, "a.local"}}); err == nil {
		t.Fatal("missing IP must be refused")
	}
	if err := d.SetEntries([]Entry{{ip("127.0.0.1"), "bad name"}}); err == nil {
		t.Fatal("bad hostname must be refused")
	}
}
