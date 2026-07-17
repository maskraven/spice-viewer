package connector

import (
	"crypto/x509/pkix"
	"strings"
)

// expectedDN holds RDN values parsed from an OpenSSL-style host-subject string.
type expectedDN struct {
	CN string
	O  []string
	OU []string
	C  []string
	ST []string
	L  []string
}

// parseDN parses an OpenSSL-style DN such as
//
//	"OU=PVE Cluster Node,O=Proxmox Virtual Environment,CN=pve.example.com"
//
// Commas separate RDNs; "\," embeds a literal comma in a value.
// Do not use pkix.Name.String() equality against Proxmox host-subject strings.
func parseDN(hostSubject string) expectedDN {
	var out expectedDN
	for _, part := range splitDN(hostSubject) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = unescapeDNValue(strings.TrimSpace(val))
		switch strings.ToUpper(key) {
		case "CN":
			out.CN = val
		case "O":
			out.O = append(out.O, val)
		case "OU":
			out.OU = append(out.OU, val)
		case "C":
			out.C = append(out.C, val)
		case "ST", "S":
			out.ST = append(out.ST, val)
		case "L":
			out.L = append(out.L, val)
		}
	}
	return out
}

// splitDN splits on unescaped commas.
func splitDN(s string) []string {
	var parts []string
	var b strings.Builder
	escaped := false
	for _, r := range s {
		if escaped {
			// Keep the escape so unescapeDNValue can process it, or just the char.
			// We re-emit backslash+char so unescape sees the sequence.
			b.WriteByte('\\')
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == ',' {
			parts = append(parts, b.String())
			b.Reset()
			continue
		}
		b.WriteRune(r)
	}
	if escaped {
		// Trailing lone backslash: keep as literal.
		b.WriteByte('\\')
	}
	parts = append(parts, b.String())
	return parts
}

func unescapeDNValue(s string) string {
	var b strings.Builder
	escaped := false
	for _, r := range s {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		b.WriteRune(r)
	}
	if escaped {
		b.WriteByte('\\')
	}
	return b.String()
}

// subjectMatches compares a certificate subject to an OpenSSL-style host-subject.
//
// Comparison is order-independent for multi-valued O/OU (and optional C/ST/L
// when present in the expected DN). CN is compared case-insensitively.
// Does not use pkix.Name.String() equality.
func subjectMatches(certSubject pkix.Name, hostSubject string) bool {
	expected := parseDN(hostSubject)
	if !strings.EqualFold(certSubject.CommonName, expected.CN) {
		return false
	}
	if !stringSliceEqualUnordered(certSubject.Organization, expected.O) {
		return false
	}
	if !stringSliceEqualUnordered(certSubject.OrganizationalUnit, expected.OU) {
		return false
	}
	// Optional RDNs: only enforced when present in expected.
	if len(expected.C) > 0 && !stringSliceEqualUnordered(certSubject.Country, expected.C) {
		return false
	}
	if len(expected.ST) > 0 && !stringSliceEqualUnordered(certSubject.Province, expected.ST) {
		return false
	}
	if len(expected.L) > 0 && !stringSliceEqualUnordered(certSubject.Locality, expected.L) {
		return false
	}
	return true
}

// stringSliceEqualUnordered compares multi-valued RDNs order-independently
// (case-sensitive values, as OpenSSL DNs typically are for O/OU).
func stringSliceEqualUnordered(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	if len(want) == 0 {
		return true
	}
	used := make([]bool, len(got))
	for _, w := range want {
		found := false
		for i, g := range got {
			if used[i] {
				continue
			}
			if g == w {
				used[i] = true
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
