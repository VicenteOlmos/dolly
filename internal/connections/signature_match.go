package connections

import (
	"fmt"
	"sort"
	"strings"
)

// ListBySignature returns all profiles with the given connection signature, sorted by name.
func ListBySignature(all []Connection, sig string) []Connection {
	out := make([]Connection, 0)
	for _, c := range all {
		if c.Signature() == sig {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ResolveBySignature picks a single profile when credentials match saved entries.
// preferName disambiguates when multiple profiles share a signature (e.g. save-as aliases).
func ResolveBySignature(all []Connection, sig, preferName string) (Connection, bool, error) {
	matches := ListBySignature(all, sig)
	switch len(matches) {
	case 0:
		return Connection{}, false, nil
	case 1:
		return matches[0], true, nil
	default:
		if preferName != "" {
			for _, m := range matches {
				if m.Name == preferName {
					return m, true, nil
				}
			}
		}
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.Name
		}
		return Connection{}, false, fmt.Errorf(
			"multiple saved profiles match these credentials (%s) — pick one from Saved",
			strings.Join(names, ", "),
		)
	}
}

// OtherNamesWithSignature returns profile names sharing sig, excluding excludeName.
func OtherNamesWithSignature(all []Connection, sig, excludeName string) []string {
	var names []string
	for _, c := range all {
		if c.Name == excludeName {
			continue
		}
		if c.Signature() == sig {
			names = append(names, c.Name)
		}
	}
	sort.Strings(names)
	return names
}
