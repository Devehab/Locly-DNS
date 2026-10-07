// Package validate parses and checks the hostnames and addresses LocalDNS
// accepts. It enforces the product's safety policy: LocalDNS only maps
// local-only hostnames (never public internet domains) to local addresses.
package validate

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

// LocalSuffixes are the domain suffixes LocalDNS is allowed to manage. All of
// them are reserved for local or private use and are never delegated in the
// public DNS, so mapping them can never shadow an internet domain.
var LocalSuffixes = []string{
	"local",       // RFC 6762 (multicast DNS / local network)
	"localhost",   // RFC 6761
	"test",        // RFC 6761
	"example",     // RFC 6761
	"internal",    // ICANN reserved for private use (2024)
	"home.arpa",   // RFC 8375
	"lan",         // common private-network convention, not delegated
	"localdomain", // common private-network convention, not delegated
}

// reservedNames are system names LocalDNS must never take over.
var reservedNames = map[string]bool{
	"localhost":             true,
	"localhost.localdomain": true,
	"broadcasthost":         true,
	"ip6-localhost":         true,
	"ip6-loopback":          true,
	"ip6-localnet":          true,
	"ip6-mcastprefix":       true,
	"ip6-allnodes":          true,
	"ip6-allrouters":        true,
	"ip6-allhosts":          true,
}

// Error is a validation failure with an optional hint for fixing it.
type Error struct {
	Message string
	Hint    string
}

func (e *Error) Error() string { return e.Message }

func invalid(hint, format string, args ...any) error {
	return &Error{Message: fmt.Sprintf(format, args...), Hint: hint}
}

// Hostname checks the syntax of a hostname (RFC 1123) and returns it in
// canonical lower-case form. It does not apply the local-suffix policy; see
// LocalHostname for that.
func Hostname(name string) (string, error) {
	raw := name
	name = TrimURL(name)
	name = strings.TrimSuffix(name, ".")
	name = strings.ToLower(name)

	if name == "" {
		return "", invalid("Example: localdns add app.local 127.0.0.1:3000", "hostname is empty")
	}
	if strings.Contains(name, "://") || strings.Contains(name, "/") {
		return "", invalid("Use just the hostname, e.g. app.local", "invalid hostname %q: remove the path", raw)
	}
	if strings.Contains(name, ":") {
		return "", invalid("The port belongs to the address: localdns add app.local 127.0.0.1:3000",
			"invalid hostname %q: hostnames cannot contain a port", raw)
	}
	if len(name) > 253 {
		return "", invalid("", "invalid hostname %q: longer than 253 characters", raw)
	}
	for _, label := range strings.Split(name, ".") {
		if err := checkLabel(label); err != nil {
			return "", invalid("Hostnames may contain letters, digits, hyphens and dots, e.g. my-app.local",
				"invalid hostname %q: %v", raw, err)
		}
	}
	return name, nil
}

func checkLabel(label string) error {
	if label == "" {
		return errors.New("empty label")
	}
	if len(label) > 63 {
		return fmt.Errorf("label %q is longer than 63 characters", label)
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return fmt.Errorf("label %q starts or ends with a hyphen", label)
	}
	for _, c := range label {
		ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-'
		if !ok {
			return fmt.Errorf("character %q is not allowed", c)
		}
	}
	return nil
}

// LocalHostname validates syntax and the local-only policy: the name must end
// in one of LocalSuffixes, must have a label before the suffix, and must not be
// a reserved system name. It returns the canonical hostname.
func LocalHostname(name string) (string, error) {
	h, err := Hostname(name)
	if err != nil {
		return "", err
	}
	if reservedNames[h] {
		return "", invalid("Pick a name like app.local or api.test",
			"%s is a system hostname; LocalDNS does not manage it", h)
	}
	for _, suffix := range LocalSuffixes {
		if h == suffix {
			return "", invalid(fmt.Sprintf("Add a name in front of it, e.g. app.%s", suffix),
				"%s is a top-level suffix, not a hostname", h)
		}
		if strings.HasSuffix(h, "."+suffix) {
			return h, nil
		}
	}
	return "", invalid(
		"LocalDNS only manages local hostnames ending in "+SuffixList()+
			". Internet domains always use your normal DNS.",
		"%s is not a local hostname", h)
}

// SuffixList renders LocalSuffixes for messages, e.g. ".local, .test, ...".
func SuffixList() string {
	parts := make([]string, len(LocalSuffixes))
	for i, s := range LocalSuffixes {
		parts[i] = "." + s
	}
	return strings.Join(parts, ", ")
}

// TrimURL accepts a pasted link: it removes surrounding spaces, an http:// or
// https:// scheme and trailing slashes, so "http://127.0.0.1:3000/" becomes
// "127.0.0.1:3000". Anything else, such as a path, is left for the caller to
// reject.
func TrimURL(s string) string {
	s = strings.TrimSpace(s)
	lower := strings.ToLower(s)
	for _, scheme := range []string{"http://", "https://"} {
		if strings.HasPrefix(lower, scheme) {
			s = s[len(scheme):]
			break
		}
	}
	return strings.TrimRight(s, "/")
}

// Address parses "IP" or "IP:PORT" (IPv6 as "[::1]:3000"). Port is 0 when
// absent. A pasted link such as "http://127.0.0.1:3000/" is accepted, and
// "localhost" means 127.0.0.1. It does not apply the local-IP policy; see
// LocalIP.
func Address(s string) (netip.Addr, uint16, error) {
	raw := s
	s = TrimURL(s)
	if s == "" {
		return netip.Addr{}, 0, invalid("Example: 127.0.0.1:3000 or 192.168.1.60", "address is empty")
	}
	if strings.Contains(s, "://") || strings.Contains(s, "/") || strings.ContainsAny(s, "?#") {
		return netip.Addr{}, 0, invalid("LocalDNS maps a name to an IP and port, not to a page. Use e.g. "+
			"127.0.0.1:3000, then open the page under the name: http://app.local/page",
			"invalid address %q: remove the path", raw)
	}
	if strings.EqualFold(s, "localhost") {
		s = "127.0.0.1"
	} else if len(s) > len("localhost:") && strings.EqualFold(s[:len("localhost:")], "localhost:") {
		s = "127.0.0.1" + s[len("localhost"):]
	}

	if ap, err := netip.ParseAddrPort(s); err == nil {
		if ap.Port() == 0 {
			return netip.Addr{}, 0, invalid("Ports range from 1 to 65535", "invalid address %q: port 0 is not allowed", raw)
		}
		return ap.Addr().Unmap(), ap.Port(), nil
	}
	if ip, err := netip.ParseAddr(s); err == nil {
		return ip.Unmap(), 0, nil
	}

	// Produce a precise message for the common mistakes.
	host, port, hasPort := splitHostPort(s)
	if hasPort {
		if _, err := strconv.ParseUint(port, 10, 16); err != nil {
			return netip.Addr{}, 0, invalid("Ports range from 1 to 65535", "invalid address %q: bad port %q", raw, port)
		}
	}
	if _, err := netip.ParseAddr(host); err != nil {
		if _, herr := Hostname(host); herr == nil && strings.ContainsAny(host, "abcdefghijklmnopqrstuvwxyz") {
			return netip.Addr{}, 0, invalid("Use an IP address such as 127.0.0.1:3000, not a hostname",
				"invalid address %q: expected an IP address", raw)
		}
	}
	return netip.Addr{}, 0, invalid("Expected IP or IP:PORT, e.g. 127.0.0.1:3000, 192.168.1.60 or [::1]:3000",
		"invalid address %q", raw)
}

func splitHostPort(s string) (host, port string, ok bool) {
	if strings.HasPrefix(s, "[") {
		end := strings.Index(s, "]")
		if end < 0 {
			return s, "", false
		}
		rest := s[end+1:]
		if strings.HasPrefix(rest, ":") {
			return s[1:end], rest[1:], true
		}
		return s[1:end], "", false
	}
	if strings.Count(s, ":") == 1 {
		i := strings.Index(s, ":")
		return s[:i], s[i+1:], true
	}
	return s, "", false
}

var (
	cgnat     = netip.MustParsePrefix("100.64.0.0/10")
	broadcast = netip.MustParseAddr("255.255.255.255")
)

// LocalIP checks that ip is an address on this machine or a private network:
// loopback, RFC 1918 / ULA private ranges, link-local, or carrier-grade NAT
// space (used by VPN overlays such as Tailscale). Public addresses are refused.
func LocalIP(ip netip.Addr) error {
	ip = ip.Unmap()
	switch {
	case !ip.IsValid():
		return invalid("", "invalid IP address")
	case ip.IsUnspecified():
		return invalid("Use 127.0.0.1 for services on this machine",
			"%s is not a usable address (LocalDNS is not an ad blocker)", ip)
	case ip.IsMulticast() || ip == broadcast:
		return invalid("Use the IP of a specific machine", "%s is a multicast/broadcast address", ip)
	case ip.IsLoopback(), ip.IsPrivate(), ip.IsLinkLocalUnicast(), ip.Is4() && cgnat.Contains(ip):
		return nil
	}
	return invalid(
		"LocalDNS maps hostnames to local addresses only: 127.0.0.0/8, ::1, 10.0.0.0/8, 172.16.0.0/12, "+
			"192.168.0.0/16, 100.64.0.0/10, 169.254.0.0/16, fc00::/7, fe80::/10",
		"%s is a public internet address", ip)
}

// Mapping checks that hostname may point to ip. Names ending in .localhost
// always mean this computer (RFC 6761): browsers such as Chrome send them to
// the loopback address themselves and never look at the hosts file, so a
// .localhost name for another device would silently go to the wrong place.
func Mapping(hostname string, ip netip.Addr) error {
	if strings.HasSuffix(hostname, ".localhost") && !ip.Unmap().IsLoopback() {
		base, _ := SplitSuffix(hostname)
		return invalid("Use another ending for other devices, e.g. "+base+".local or "+base+".lan",
			"%s must point to this computer (127.0.0.1): browsers always send .localhost names here, not to %s",
			hostname, ip)
	}
	return nil
}

// SplitSuffix splits a hostname into its name and its local ending, e.g.
// "my-app.home.arpa" into "my-app" and "home.arpa". The ending is empty when
// the name has none of LocalSuffixes.
func SplitSuffix(hostname string) (base, suffix string) {
	for _, s := range LocalSuffixes {
		if strings.HasSuffix(hostname, "."+s) && len(s) > len(suffix) {
			suffix = s
		}
	}
	if suffix == "" {
		return hostname, ""
	}
	return strings.TrimSuffix(hostname, "."+suffix), suffix
}

// FormatAddress renders ip and an optional port (0 = none) the way users type
// them: "127.0.0.1:3000", "192.168.1.60", "[::1]:3000".
func FormatAddress(ip netip.Addr, port uint16) string {
	if port == 0 {
		return ip.String()
	}
	return netip.AddrPortFrom(ip, port).String()
}
