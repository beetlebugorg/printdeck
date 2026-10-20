package main

import (
	"crypto/rand"
	_ "embed"
	"fmt"
	"math/big"
	"strings"
)

// words is the EFF short word list 1 (https://www.eff.org/dice, CC BY 3.0 US), without "yo-yo".
// The words have 3 to 5 letters, so they are easy to type on a phone or on the deck.
//
//go:embed words.txt
var words string

// passwordWords is the number of words in a password. 4 words from 1295 give about 41 bits.
const passwordWords = 4

// newPassword makes a random password of words with dashes, for example "stamp-ocean-cider-brick".
func newPassword() (string, error) {
	list := strings.Fields(words)
	parts := make([]string, passwordWords)
	for i := range parts {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(list))))
		if err != nil {
			return "", err
		}
		parts[i] = list[n.Int64()]
	}
	return strings.Join(parts, "-"), nil
}

// password prints a new password. printdeck-credentials uses it at the first start.
func password() error {
	p, err := newPassword()
	if err != nil {
		return err
	}
	fmt.Println(p)
	return nil
}
