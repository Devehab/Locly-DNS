// Package hosts reads and edits the operating system hosts file.
//
// LocalDNS owns exactly one region of the file, delimited by marker lines:
//
//	# BEGIN LOCALDNS
//	127.0.0.1 app.local
//	# END LOCALDNS
//
// Everything outside that region is preserved byte for byte. The package
// never resolves names over the network; it only parses and renders text.
package hosts

import (
	"bytes"
	"fmt"
	"net/netip"
	"strings"

	"github.com/devehab/locly-dns/internal/validate"
)

// Marker lines delimiting the LocalDNS-managed section.
const (
	BeginMarker = "# BEGIN LOCALDNS"
	EndMarker   = "# END LOCALDNS"
)

// Entry is one hostname mapping inside the managed section.
type Entry struct {
	IP       netip.Addr
	Hostname string
}

// Mapping is a hostname mapping found anywhere in the hosts file.
type Mapping struct {
	Line     int // 1-based line number
	IP       netip.Addr
	Hostname string
	Managed  bool // inside the LocalDNS section
}

// SectionError reports a problem with the LocalDNS section.
type SectionError struct {
	Line   int    // 1-based line number of the problem (0 if not tied to a line)
	Reason string // human-readable description
	// Markers is true when the BEGIN/END markers themselves are broken, so the
	// section cannot be located at all. When false, the markers are fine but a
	// line inside the section is invalid.
	Markers bool
}

func (e *SectionError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("LocalDNS section is invalid (line %d): %s", e.Line, e.Reason)
	}
	return "LocalDNS section is invalid: " + e.Reason
}

// Document is a parsed hosts file.
type Document struct {
	head       []byte // bytes before the section (the whole file when there is no section)
	tail       []byte // bytes after the section
	hasSection bool
	beginLine  int // 1-based line of BEGIN marker
	entries    []Entry
	mappings   []Mapping
	eol        string
	err        *SectionError
}

// Parse parses hosts file content. It never fails: problems with the LocalDNS
// section are reported by Err, and content outside the section is kept
// verbatim whether or not it is well formed.
func Parse(data []byte) *Document {
	d := &Document{eol: "\n"}
	if bytes.Contains(data, []byte("\r\n")) {
		d.eol = "\r\n"
	}

	type span struct{ start, end int } // end includes the line terminator
	var lines []span
	for start := 0; start < len(data); {
		i := bytes.IndexByte(data[start:], '\n')
		if i < 0 {
			lines = append(lines, span{start, len(data)})
			break
		}
		lines = append(lines, span{start, start + i + 1})
		start += i + 1
	}

	begin, end := -1, -1
	seen := map[string]bool{}
	for i, ln := range lines {
		text := strings.TrimRight(string(data[ln.start:ln.end]), "\r\n")
		switch markerKind(text) {
		case "begin":
			if begin >= 0 {
				d.setErr(&SectionError{Line: i + 1, Reason: "found a second '" + BeginMarker + "' line", Markers: true})
				continue
			}
			begin = i
			continue
		case "end":
			if begin < 0 || end >= 0 {
				d.setErr(&SectionError{Line: i + 1, Reason: "found '" + EndMarker + "' without a matching '" + BeginMarker + "'", Markers: true})
				continue
			}
			end = i
			continue
		}

		managed := begin >= 0 && end < 0
		ip, names, perr := parseLine(text)
		if perr != nil {
			if managed {
				d.setErr(&SectionError{Line: i + 1, Reason: perr.Error()})
			}
			continue // unmanaged lines we cannot parse are not our business
		}
		for _, name := range names {
			if managed {
				h, herr := validate.Hostname(name)
				if herr != nil {
					d.setErr(&SectionError{Line: i + 1, Reason: herr.Error()})
					continue
				}
				if seen[h] {
					d.setErr(&SectionError{Line: i + 1, Reason: fmt.Sprintf("hostname %s is listed more than once", h)})
					continue
				}
				seen[h] = true
				d.entries = append(d.entries, Entry{IP: ip, Hostname: h})
				name = h
			}
			d.mappings = append(d.mappings, Mapping{Line: i + 1, IP: ip, Hostname: strings.ToLower(name), Managed: managed})
		}
	}
	if begin >= 0 && end < 0 {
		d.setErr(&SectionError{Line: begin + 1, Reason: "'" + BeginMarker + "' has no matching '" + EndMarker + "'", Markers: true})
	}

	switch {
	case d.err != nil && d.err.Markers:
		// The section cannot be located safely; treat the whole file as foreign.
		d.head = data
	case begin >= 0:
		d.hasSection = true
		d.beginLine = begin + 1
		d.head = data[:lines[begin].start]
		d.tail = data[lines[end].end:]
	default:
		d.head = data
	}
	return d
}

func (d *Document) setErr(e *SectionError) {
	if d.err == nil || (e.Markers && !d.err.Markers) {
		d.err = e
	}
}

// markerKind classifies a line as "begin", "end" or "" (not a marker). It is
// tolerant of spacing and case so hand-edited markers are still recognized.
func markerKind(line string) string {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "#") {
		return ""
	}
	norm := strings.ToUpper(strings.Join(strings.Fields(strings.TrimPrefix(t, "#")), " "))
	switch norm {
	case "BEGIN LOCALDNS":
		return "begin"
	case "END LOCALDNS":
		return "end"
	}
	return ""
}

// parseLine parses "IP name [name...] [# comment]". Blank and comment-only
// lines return a zero IP and no names, with no error.
func parseLine(line string) (netip.Addr, []string, error) {
	if i := strings.IndexByte(line, '#'); i >= 0 {
		line = line[:i]
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return netip.Addr{}, nil, nil
	}
	ip, err := netip.ParseAddr(fields[0])
	if err != nil {
		return netip.Addr{}, nil, fmt.Errorf("%q is not a valid IP address", fields[0])
	}
	if len(fields) < 2 {
		return netip.Addr{}, nil, fmt.Errorf("IP %s has no hostname", fields[0])
	}
	return ip, fields[1:], nil
}

// Err returns the problem with the LocalDNS section, or nil if it is valid
// (or absent).
func (d *Document) Err() *SectionError { return d.err }

// HasSection reports whether the file contains a LocalDNS section.
func (d *Document) HasSection() bool { return d.hasSection }

// Entries returns the mappings inside the LocalDNS section, in file order.
func (d *Document) Entries() []Entry {
	return append([]Entry(nil), d.entries...)
}

// Entry returns the managed entry for hostname.
func (d *Document) Entry(hostname string) (Entry, bool) {
	for _, e := range d.entries {
		if e.Hostname == hostname {
			return e, true
		}
	}
	return Entry{}, false
}

// Lookup returns the mapping the operating system will use for hostname: the
// first line in the file that lists it. ok is false when the hosts file does
// not mention hostname at all, meaning the system resolves it through its
// normal DNS configuration.
func (d *Document) Lookup(hostname string) (m Mapping, ok bool) {
	hostname = strings.ToLower(hostname)
	for _, m := range d.mappings {
		if m.Hostname == hostname {
			return m, true
		}
	}
	return Mapping{}, false
}

// Unmanaged returns the mappings for hostname that live outside the LocalDNS
// section (entries LocalDNS does not own and must never modify).
func (d *Document) Unmanaged(hostname string) []Mapping {
	var out []Mapping
	for _, m := range d.mappings {
		if !m.Managed && m.Hostname == hostname {
			out = append(out, m)
		}
	}
	return out
}

// SetEntries replaces the contents of the LocalDNS section. An empty slice
// removes the section entirely. It fails if the current section is invalid,
// because rewriting it could destroy lines the user typed by hand.
func (d *Document) SetEntries(entries []Entry) error {
	if d.err != nil {
		return d.err
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if !e.IP.IsValid() {
			return fmt.Errorf("entry %s has no IP address", e.Hostname)
		}
		if _, err := validate.Hostname(e.Hostname); err != nil {
			return err
		}
		if seen[e.Hostname] {
			return fmt.Errorf("hostname %s is listed more than once", e.Hostname)
		}
		seen[e.Hostname] = true
	}
	d.entries = append([]Entry(nil), entries...)
	d.hasSection = len(entries) > 0
	return nil
}

// RemoveSection drops the LocalDNS section, including any invalid lines inside
// it. It only fails when the markers are broken and the section's boundaries
// are unknown.
func (d *Document) RemoveSection() error {
	if d.err != nil && d.err.Markers {
		return d.err
	}
	d.err = nil
	d.entries = nil
	d.hasSection = false
	return nil
}

// Bytes renders the file. Content outside the section is returned unchanged.
// A new section is appended at the end of the file.
func (d *Document) Bytes() []byte {
	var b bytes.Buffer
	b.Write(d.head)
	if d.hasSection && len(d.entries) > 0 {
		if b.Len() > 0 && !bytes.HasSuffix(d.head, []byte("\n")) {
			b.WriteString(d.eol)
		}
		b.WriteString(BeginMarker + d.eol)
		for _, e := range d.entries {
			b.WriteString(e.IP.String() + " " + e.Hostname + d.eol)
		}
		b.WriteString(EndMarker + d.eol)
	}
	b.Write(d.tail)
	return b.Bytes()
}
