package system

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// The name of a deck: the name that slicers show (the DNS-SD service of systemd-resolved), and the
// host name for <host>.local, made from the name. printdeck.txt keeps both (name= and
// hostname=), and Settings applies them at each boot. The image has the service with the name
// "Printdeck" in /usr/lib/systemd/dnssd. A service file of the same name in /run replaces it.

// MaxName is the longest name of a deck, in characters.
const MaxName = 40

// HostLabel returns the host name for the name of a deck: the letters and the digits in lower
// case, with "-" for the other characters, at most 63 characters. "Workhorse 2" is
// "workhorse-2". It is "" for a name with no letter or digit.
func HostLabel(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		default:
			dash = true
		}
	}
	s := b.String()
	if len(s) > 63 {
		s = strings.TrimRight(s[:63], "-")
	}
	return s
}

// CheckName returns the name with no spaces at its ends, or an error for a name that the deck
// cannot use.
func CheckName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case len([]rune(name)) > MaxName:
		return "", fmt.Errorf("A name has %d characters at most.", MaxName)
	case strings.IndexFunc(name, func(r rune) bool { return !unicode.IsPrint(r) }) >= 0:
		return "", errors.New("The name has a character that the deck cannot use.")
	case HostLabel(name) == "":
		return "", errors.New("A name needs a letter or a digit.")
	}
	return name, nil
}

// setKey sets key=value in the text of printdeck.txt: in the place of the line of the key, or at
// the end.
func setKey(text, key, value string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, key+"=") {
			lines[i] = key + "=" + value
			return strings.Join(lines, "\n")
		}
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text + key + "=" + value + "\n"
}
