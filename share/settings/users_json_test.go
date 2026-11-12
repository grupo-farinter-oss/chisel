package settings

import (
	"regexp"
	"testing"

	"github.com/jpillora/chisel/share/cio"
)

func TestLoadUsersJSONLoadsACLWithoutFileWatcher(t *testing.T) {
	index := NewUserIndex(cio.NewLogger("test"))
	index.PinUser(&User{Name: "pinned", Pass: "full-access", Addrs: []*regexp.Regexp{UserAllowAll}})

	data := []byte(`{"alice:example-password":["^127\\.0\\.0\\.1:80$"]}`)
	if err := index.LoadUsersJSON(data); err != nil {
		t.Fatal(err)
	}
	user, found := index.Get("alice")
	if !found || user.Pass != "example-password" {
		t.Fatalf("loaded user = %+v, found=%t", user, found)
	}
	if !user.HasAccess("127.0.0.1:80") || user.HasAccess("127.0.0.1:443") {
		t.Fatal("loaded ACL does not match expected allowed/denied destinations")
	}
	if _, found := index.Get("pinned"); !found {
		t.Fatal("pinned AUTH user was lost while loading JSON ACL")
	}
	if index.configFile != "" {
		t.Fatalf("JSON source unexpectedly configured file watcher for %q", index.configFile)
	}
}

func TestLoadUsersJSONRejectsInvalidConfigs(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "empty", data: ""},
		{name: "malformed", data: `{"alice:pw":`},
		{name: "wrong shape", data: `[]`},
		{name: "invalid user", data: `{"no-colon":["^"]}`},
		{name: "invalid pattern", data: `{"alice:pw":["["]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			index := NewUserIndex(cio.NewLogger("test"))
			index.PinUser(&User{Name: "pinned", Pass: "pw", Addrs: []*regexp.Regexp{UserAllowAll}})
			if err := index.LoadUsersJSON([]byte(tt.data)); err == nil {
				t.Fatal("LoadUsersJSON accepted invalid or empty configuration")
			}
			if _, found := index.Get("pinned"); !found {
				t.Fatal("failed JSON load changed existing users")
			}
		})
	}
}

func TestLoadUsersJSONPreservesEmptyObjectSemantics(t *testing.T) {
	for _, data := range []string{`{}`, `null`} {
		t.Run(data, func(t *testing.T) {
			index := NewUserIndex(cio.NewLogger("test"))
			if err := index.LoadUsersJSON([]byte(data)); err != nil {
				t.Fatalf("LoadUsersJSON(%s): %v", data, err)
			}
			if index.Len() != 0 {
				t.Fatalf("users after loading %s: got %d, want 0", data, index.Len())
			}
		})
	}
}
