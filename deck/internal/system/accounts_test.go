package system

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The account files of the test Pi before the update that added the user deck: the copy in
// /data/etc had the users spotify and camilla at the ids that the new image gave to deck.
var (
	upperGroup  = "root:x:0:\ndialout:x:18:printer\nvideo:x:28:spotify\naudio:x:29:spotify,camilla\nwheel:x:10:printdeck\nprinter:x:111:\nspotify:x:112:\ncamilla:x:113:\nprintdeck:x:1000:\n"
	upperPasswd = "root:x:0:0:root:/root:/bin/sh\nprinter:x:104:111:Klipper host:/home/printer:/bin/false\nspotify:x:105:112:Spotify:/home/spotify:/bin/false\ncamilla:x:106:113:CamillaDSP:/home/camilla:/bin/false\nprintdeck:x:1000:1000::/home/printdeck:/bin/sh\n"
	upperShadow = "root:*:::::::\nprinter:*:::::::\nspotify:*:::::::\ncamilla:*:::::::\nprintdeck:$6$abc:20000:0:99999:7:::\n"
	lowerGroup  = "root:x:0:\ndialout:x:18:printer,deck\nvideo:x:28:deck\naudio:x:29:deck\nwheel:x:10:\ninput:x:108:deck\ntty:x:5:printer,deck\nprinter:x:111:deck\ndeck:x:112:\n"
	lowerPasswd = "root:x:0:0:root:/root:/bin/sh\nprinter:x:104:111:Klipper host:/home/printer:/bin/false\ndeck:x:105:112:Printdeck program:/home/deck:/bin/false\n"
	lowerShadow = "root:*:::::::\nprinter:*:::::::\ndeck:*:::::::\n"
)

func writeAccounts(t *testing.T, dir, group, passwd, shadow string) {
	t.Helper()
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "group"), []byte(group), 0o644)
	os.WriteFile(filepath.Join(dir, "passwd"), []byte(passwd), 0o644)
	os.WriteFile(filepath.Join(dir, "shadow"), []byte(shadow), 0o600)
}

func TestMergeAccounts(t *testing.T) {
	dir := t.TempDir()
	goUp, shUp, low := filepath.Join(dir, "go"), filepath.Join(dir, "sh"), filepath.Join(dir, "lower")
	writeAccounts(t, goUp, upperGroup, upperPasswd, upperShadow)
	writeAccounts(t, shUp, upperGroup, upperPasswd, upperShadow)
	writeAccounts(t, low, lowerGroup, lowerPasswd, lowerShadow)

	if err := MergeAccounts(goUp, low); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("sh", "testdata/merge_accounts.sh", shUp, low).CombinedOutput(); err != nil {
		t.Fatalf("reference: %v %s", err, out)
	}
	for _, f := range []string{"group", "passwd", "shadow"} {
		got, _ := os.ReadFile(filepath.Join(goUp, f))
		want, _ := os.ReadFile(filepath.Join(shUp, f))
		if string(got) != string(want) {
			t.Errorf("%s:\n%s\nthe shell version:\n%s", f, got, want)
		}
	}
	// deck got the free ids 100, and its group by the name.
	passwd, _ := os.ReadFile(filepath.Join(goUp, "passwd"))
	if want := "deck:x:100:100:Printdeck program:/home/deck:/bin/false\n"; !strings.Contains(string(passwd), want) {
		t.Errorf("passwd has no %q:\n%s", want, passwd)
	}

	// A second merge changes nothing.
	before, _ := os.ReadFile(filepath.Join(goUp, "group"))
	MergeAccounts(goUp, low)
	after, _ := os.ReadFile(filepath.Join(goUp, "group"))
	if string(before) != string(after) {
		t.Error("a second merge changed the group file")
	}
}

// With no copies, the image files are in use: nothing is written.
func TestMergeAccountsNoCopy(t *testing.T) {
	dir := t.TempDir()
	low := filepath.Join(dir, "lower")
	writeAccounts(t, low, lowerGroup, lowerPasswd, lowerShadow)
	up := filepath.Join(dir, "upper")
	os.MkdirAll(up, 0o755)
	if err := MergeAccounts(up, low); err != nil {
		t.Fatal(err)
	}
	if m, _ := filepath.Glob(filepath.Join(up, "*")); len(m) != 0 {
		t.Errorf("files written: %v", m)
	}
}
