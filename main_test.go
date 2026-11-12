package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	chclient "github.com/jpillora/chisel/client"
	chserver "github.com/jpillora/chisel/server"
	"github.com/jpillora/opts"
)

// helpText renders a command's help at a fixed total line width, bypassing
// terminal detection so the wrapping is deterministic under `go test`.
func helpText(t *testing.T, cmd opts.Opts, width int) string {
	t.Helper()
	_, err := cmd.SetLineWidth(width).ParseArgsError([]string{"chisel", "--help"})
	if err == nil {
		t.Fatal("help did not stop parsing")
	}
	return err.Error()
}

// optionLines returns the flag list, which is the section opts wraps. The
// surrounding blocks (remotes, signals) are rendered verbatim by design.
func optionLines(help string) []string {
	lines := strings.Split(help, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "Options:" {
			continue
		}
		for j, line := range lines[i+1:] {
			if strings.TrimSpace(line) == "" {
				return lines[i+1 : i+1+j]
			}
		}
		return lines[i+1:]
	}
	return nil
}

func helpCommands(t *testing.T) map[string]func() opts.Opts {
	t.Helper()
	return map[string]func() opts.Opts{
		"server": func() opts.Opts { return newServerCLI(newServerOptions()) },
		"client": func() opts.Opts { return newClientCLI(newClientOptions()) },
	}
}

// The help tags hold unwrapped prose, so opts is free to wrap them to whatever
// width it detects. Words longer than the width are left to overflow.
func TestFlagHelpWrapsToLineWidth(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	const padWidth = 2
	for name, newCmd := range helpCommands(t) {
		for _, width := range []int{60, 80, 96} {
			t.Run(fmt.Sprintf("%s/%d", name, width), func(t *testing.T) {
				lines := optionLines(helpText(t, newCmd(), width))
				if len(lines) == 0 {
					t.Fatal("no option lines rendered")
				}
				for _, line := range lines {
					if len(line) <= width-padWidth {
						continue
					}
					if fields := strings.Fields(line); len(fields) == 1 {
						continue //a single unbreakable word
					}
					t.Errorf("line exceeds width %d (%d chars): %q", width, len(line), line)
				}
			})
		}
	}
}

func TestFlagHelpReflowsWithWidth(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for name, newCmd := range helpCommands(t) {
		t.Run(name, func(t *testing.T) {
			narrow := optionLines(helpText(t, newCmd(), 60))
			wide := optionLines(helpText(t, newCmd(), 96))
			if len(narrow) <= len(wide) {
				t.Fatalf("help did not reflow: %d lines at width 60, %d at width 96", len(narrow), len(wide))
			}
		})
	}
}

// Every flag carrying a help tag must survive into the rendered help.
func TestHelpListsEveryFlag(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	want := map[string][]string{
		"server": {
			"--host", "--port", "--keygen", "--key", "--keyfile", "--authfile",
			"--auth", "--backend", "--socks5", "--reverse", "--keepalive",
			"--tls-key", "--tls-cert", "--tls-domain", "--tls-ca", "--proxy",
			"--pid", "--verbose",
		},
		"client": {
			"--fingerprint", "--auth", "--keepalive", "--max-retry-count",
			"--min-retry-interval", "--max-retry-interval", "--proxy",
			"--tls-skip-verify", "--tls-ca", "--tls-cert", "--tls-key",
			"--verbose", "--header", "--hostname", "--sni", "--pid",
		},
	}
	for name, newCmd := range helpCommands(t) {
		t.Run(name, func(t *testing.T) {
			help := strings.Join(optionLines(helpText(t, newCmd(), 96)), "\n")
			for _, flag := range want[name] {
				//a flag with a short name renders as "--verbose, -v"
				if !strings.Contains(help, flag+" ") && !strings.Contains(help, flag+",") {
					t.Errorf("%s missing from help", flag)
				}
			}
		})
	}
}

func TestAllFlagsHaveHelpTags(t *testing.T) {
	for _, config := range []any{serverOptions{}, clientOptions{}} {
		checkFlagHelpTags(t, reflect.TypeOf(config))
	}
}

func checkFlagHelpTags(t *testing.T, typeOf reflect.Type) {
	t.Helper()
	for i := 0; i < typeOf.NumField(); i++ {
		field := typeOf.Field(i)
		if field.PkgPath != "" {
			continue
		}
		optsTag := field.Tag.Get("opts")
		if optsTagValue(optsTag, "mode") == "arg" {
			continue
		}
		if field.Tag.Get("help") != "" {
			continue
		}
		if field.Type.Kind() == reflect.Struct && optsTag != "-" {
			checkFlagHelpTags(t, field.Type)
			continue
		}
		if optsTag != "-" {
			t.Errorf("%s.%s has no help tag", typeOf.Name(), field.Name)
		}
	}
}

func TestServerOptions(t *testing.T) {
	server := newServerOptions()
	cli := newCLI(server, newClientOptions())
	_, err := cli.ParseArgsError([]string{
		"chisel", "server",
		"--host", "127.0.0.1",
		"-p", "9090",
		"--key", "seed",
		"--keyfile", "key.pem",
		"--authfile", "users.json",
		"--auth", "user:pass",
		"--keepalive", "30s",
		"--backend", "http://first",
		"--proxy", "http://last",
		"--socks5",
		"--reverse",
		"--tls-key", "tls-key.pem",
		"--tls-cert", "tls-cert.pem",
		"--tls-domain", "one.example",
		"--tls-domain", "two.example",
		"--tls-ca", "ca.pem",
		"--pid",
		"-v",
	})
	if err != nil {
		t.Fatal(err)
	}
	if server.Host != "127.0.0.1" || server.Port != "9090" {
		t.Fatalf("listen options not parsed: host=%q port=%q", server.Host, server.Port)
	}
	if server.Config.Proxy != "http://last" {
		t.Fatalf("proxy alias order not preserved: %q", server.Config.Proxy)
	}
	if server.KeepAlive != 30*time.Second || !server.Socks5 || !server.Reverse || !server.PID || !server.Verbose {
		t.Fatalf("server options not parsed: %+v", server)
	}
	if want := []string{"one.example", "two.example"}; !reflect.DeepEqual(server.Config.TLS.Domains, want) {
		t.Fatalf("TLS domains: got %q, want %q", server.Config.TLS.Domains, want)
	}
}

func TestClientOptions(t *testing.T) {
	client := newClientOptions()
	cli := newCLI(newServerOptions(), client)
	_, err := cli.ParseArgsError([]string{
		"chisel", "client",
		"--fingerprint", "fingerprint",
		"--auth", "user:pass",
		"--keepalive", "15s",
		"--max-retry-count", "7",
		"--min-retry-interval", "2s",
		"--max-retry-interval", "1m",
		"--proxy", "socks5://localhost:1080",
		"--header", "Foo: one",
		"--header", "Bar: two",
		"--hostname", "host.example",
		"--sni", "sni.example",
		"--tls-ca", "ca.pem",
		"--tls-skip-verify",
		"--tls-key", "key.pem",
		"--tls-cert", "cert.pem",
		"--pid",
		"-v",
		"wss://server.example",
		"3000",
		"R:2222:localhost:22",
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.Server != "wss://server.example" {
		t.Fatalf("server: %q", client.Server)
	}
	if want := []string{"3000", "R:2222:localhost:22"}; !reflect.DeepEqual(client.Remotes, want) {
		t.Fatalf("remotes: got %q, want %q", client.Remotes, want)
	}
	if want := []string{"Foo: one", "Bar: two"}; !reflect.DeepEqual(client.Header, want) {
		t.Fatalf("headers: got %q, want %q", client.Header, want)
	}
	if client.KeepAlive != 15*time.Second || client.MaxRetryCount != 7 || !client.Config.TLS.SkipVerify || !client.PID || !client.Verbose {
		t.Fatalf("client options not parsed: %+v", client)
	}
}

func TestClientOptionsAllowRemotesFromEnvironment(t *testing.T) {
	client := newClientOptions()
	cli := newCLI(newServerOptions(), client)
	_, err := cli.ParseArgsError([]string{"chisel", "client", "https://server.example"})
	if err != nil {
		t.Fatalf("client command without positional remotes should parse for env fallback: %v", err)
	}
	if len(client.Remotes) != 0 {
		t.Fatalf("unexpected positional remotes: %q", client.Remotes)
	}
	remoteConfig := base64.StdEncoding.EncodeToString([]byte(`["R:0.0.0.0:4000:code-server:4000","0.0.0.0:15432:postgres:5432"]`))
	t.Setenv("CHISEL_REMOTES_JSON_B64", remoteConfig)
	if err := client.resolveRemotes(); err != nil {
		t.Fatalf("resolveRemotes() error = %v", err)
	}
	if len(client.Remotes) != 0 {
		t.Fatalf("env JSON parsed in CLI instead of being passed to client: %q", client.Remotes)
	}
	if _, err := chclient.NewClientWithRemotesJSON(&client.Config, client.RemotesJSON); err != nil {
		t.Fatalf("NewClient() with env JSON: %v", err)
	}
	want := []string{"R:0.0.0.0:4000:code-server:4000", "0.0.0.0:15432:postgres:5432"}
	if !reflect.DeepEqual(client.Remotes, want) {
		t.Fatalf("client remotes after NewClient() = %q, want %q", client.Remotes, want)
	}
}

func TestResolveServerAuthJSONSourceAndPrecedence(t *testing.T) {
	rawJSON := `{"alice:example-password":["^127\\.0\\.0\\.1:80$"]}`
	wantB64 := `{"bob:example-password":["^127\\.0\\.0\\.1:443$"]}`
	for name, encoded := range map[string]string{
		"padded":   base64.StdEncoding.EncodeToString([]byte(wantB64)),
		"unpadded": base64.RawStdEncoding.EncodeToString([]byte(wantB64)),
	} {
		t.Run(name, func(t *testing.T) {
			got, set, err := resolveServerAuthJSON(rawJSON, true, encoded, true)
			if err != nil || !set || string(got) != wantB64 {
				t.Fatalf("B64 auth JSON = %q, %t, %v; want %q", got, set, err, wantB64)
			}
		})
	}

	got, set, err := resolveServerAuthJSON(rawJSON, true, "", false)
	if err != nil || !set || string(got) != rawJSON {
		t.Fatalf("raw auth JSON = %q, %t, %v", got, set, err)
	}

}

func TestResolveServerAuthJSONInvalidB64DoesNotFallBack(t *testing.T) {
	if _, set, err := resolveServerAuthJSON(`{"alice:pw":[""]}`, true, `%bad`, true); err == nil || set {
		t.Fatalf("invalid B64 must error without selecting raw JSON; set=%t err=%v", set, err)
	}
}

func TestServerOptionsResolveAuthJSONEnvironment(t *testing.T) {
	option := newServerOptions()
	rawJSON := `{"alice:example-password":["^127\\.0\\.0\\.1:80$"]}`
	b64JSON := base64.StdEncoding.EncodeToString([]byte(`{"bob:example-password":["^127\\.0\\.0\\.1:443$"]}`))
	t.Setenv("CHISEL_AUTH_JSON", rawJSON)
	t.Setenv("CHISEL_AUTH_JSON_B64", b64JSON)
	if err := option.resolveAuthJSON(); err != nil {
		t.Fatal(err)
	}
	if got, want := string(option.AuthJSON), `{"bob:example-password":["^127\\.0\\.0\\.1:443$"]}`; got != want {
		t.Fatalf("resolved AuthJSON = %q, want B64 value %q", got, want)
	}
}

func TestEmptyServerAuthJSONB64FailsWithoutFallingBack(t *testing.T) {
	authFile := filepath.Join(t.TempDir(), "users.json")
	if err := os.WriteFile(authFile, []byte(`{"alice:example-password":[""]}`), 0600); err != nil {
		t.Fatal(err)
	}
	option := newServerOptions()
	option.Config.KeySeed = "empty-auth-json-b64-test"
	option.Config.AuthFile = authFile
	t.Setenv("CHISEL_AUTH_JSON", `{"bob:example-password":[""]}`)
	t.Setenv("CHISEL_AUTH_JSON_B64", "")
	if err := option.resolveAuthJSON(); err != nil {
		t.Fatalf("empty but set B64 should select an env source, then fail JSON loading: %v", err)
	}
	if option.AuthJSON == nil {
		t.Fatal("empty but set B64 was treated as unset instead of taking precedence")
	}
	if _, err := chserver.NewServerWithAuthJSON(&option.Config, option.AuthJSON); err == nil {
		t.Fatal("server fell back to the valid authfile for empty preferred B64 config")
	}
}

func TestServerOptionsResolveEmptyAuthJSONB64WithExistingSemantics(t *testing.T) {
	for _, value := range []string{`{}`, `null`} {
		t.Run(value, func(t *testing.T) {
			option := newServerOptions()
			option.Config.KeySeed = "empty-auth-json-semantics-test"
			option.Config.AuthFile = filepath.Join(t.TempDir(), "missing-users.json")
			t.Setenv("CHISEL_AUTH_JSON", `{"alice:example-password":[""]}`)
			t.Setenv("CHISEL_AUTH_JSON_B64", base64.StdEncoding.EncodeToString([]byte(value)))
			if err := option.resolveAuthJSON(); err != nil {
				t.Fatalf("resolveAuthJSON(): %v", err)
			}
			if got := string(option.AuthJSON); got != value {
				t.Fatalf("selected AuthJSON = %q, want B64 value %q", got, value)
			}
			_, err := chserver.NewServerWithAuthJSON(&option.Config, option.AuthJSON)
			if err != nil {
				t.Fatalf("NewServer(%s): %v", value, err)
			}
		})
	}
}

func TestResolveClientRemotesPrefersPositionalArguments(t *testing.T) {
	cliRemotes := []string{"3000", "R:2222:localhost:22"}
	got, set, err := resolveClientRemotes(cliRemotes, `not JSON`, true, `not base64`, true)
	if err != nil {
		t.Fatal(err)
	}
	if set || got != nil {
		t.Fatalf("positional remotes should bypass env JSON: got=%q set=%t", got, set)
	}
}

func TestResolveClientRemotesAcceptsPaddedAndUnpaddedBase64(t *testing.T) {
	config := []byte(`["3000","R:2222:localhost:22"]`)
	want := string(config)
	for name, encoded := range map[string]string{
		"padded":   base64.StdEncoding.EncodeToString(config),
		"unpadded": base64.RawStdEncoding.EncodeToString(config),
	} {
		t.Run(name, func(t *testing.T) {
			got, set, err := resolveClientRemotes(nil, "", false, encoded, true)
			if err != nil || !set || string(got) != want {
				t.Fatalf("resolveClientRemotes() = %q, %t, %v; want %q", got, set, err, want)
			}
		})
	}
}

func TestResolveClientRemotesAcceptsRawJSON(t *testing.T) {
	want := `["3000","R:2222:localhost:22"]`
	got, set, err := resolveClientRemotes(nil, want, true, "", false)
	if err != nil || !set || string(got) != want {
		t.Fatalf("resolveClientRemotes() = %q, %t, %v; want %q", got, set, err, want)
	}
}

func TestResolveClientRemotesPrefersBase64Environment(t *testing.T) {
	rawJSON := `["3000"]`
	encodedJSON := base64.StdEncoding.EncodeToString([]byte(`["4000"]`))
	got, set, err := resolveClientRemotes(nil, rawJSON, true, encodedJSON, true)
	if err != nil || !set || string(got) != `["4000"]` {
		t.Fatalf("resolveClientRemotes() = %q, %t, %v; want B64 remotes", got, set, err)
	}
	if _, _, err := resolveClientRemotes(nil, rawJSON, true, `invalid`, true); err == nil {
		t.Fatal("invalid preferred B64 config must not fall back to raw JSON")
	}
}

func TestResolveClientRemotesRejectsMissingOrInvalidBase64(t *testing.T) {
	tests := []struct {
		name        string
		rawJSON     string
		rawJSONSet  bool
		encodedJSON string
		encodedSet  bool
	}{
		{name: "unset"},
		{name: "malformed Base64", encodedJSON: `%%%`, encodedSet: true},
		{name: "noncanonical Base64 pad bits", encodedJSON: `Zh==`, encodedSet: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := resolveClientRemotes(nil, tt.rawJSON, tt.rawJSONSet, tt.encodedJSON, tt.encodedSet); err == nil {
				t.Fatal("resolveClientRemotes() accepted missing or invalid remotes")
			}
		})
	}
}
