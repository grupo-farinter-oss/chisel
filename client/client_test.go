package chclient

import (
	"crypto/elliptic"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jpillora/chisel/share/ccrypto"
	"golang.org/x/crypto/ssh"
)

func TestNewClientParsesRemotesJSONAtRemoteConfigBoundary(t *testing.T) {
	config := Config{Server: "http://localhost"}
	if _, err := NewClientWithRemotesJSON(&config, []byte(`["9000","R:2222:localhost:22"]`)); err != nil {
		t.Fatal(err)
	}
	want := []string{"9000", "R:2222:localhost:22"}
	if !reflect.DeepEqual(config.Remotes, want) {
		t.Fatalf("parsed remotes = %q, want %q", config.Remotes, want)
	}
}

func TestNewClientRemotesJSONErrorsAndPositionalPrecedence(t *testing.T) {
	for _, value := range []string{"", `[{`, `[]`, `null`, `[3000]`, `[" "]`} {
		t.Run(value, func(t *testing.T) {
			_, err := NewClientWithRemotesJSON(&Config{Server: "http://localhost"}, []byte(value))
			if err == nil {
				t.Fatalf("NewClient accepted invalid remotes JSON %q", value)
			}
		})
	}

	config := Config{
		Server:  "http://localhost",
		Remotes: []string{"9000"},
	}
	if _, err := NewClientWithRemotesJSON(&config, []byte(`invalid`)); err != nil {
		t.Fatalf("positional remotes should take precedence over JSON: %v", err)
	}
	if !reflect.DeepEqual(config.Remotes, []string{"9000"}) {
		t.Fatalf("positional remotes changed: %q", config.Remotes)
	}
}

func TestCustomHeaders(t *testing.T) {
	//fake server, records the header for the main goroutine to
	//assert (t.Fatal must not be called from the handler goroutine)
	wg := sync.WaitGroup{}
	wg.Add(1)
	var gotFoo string
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		gotFoo = req.Header.Get("Foo")
		wg.Done()
	}))
	defer server.Close()
	//client
	headers := http.Header{}
	headers.Set("Foo", "Bar")
	config := Config{
		KeepAlive:        time.Second,
		MaxRetryInterval: time.Second,
		Server:           server.URL,
		Remotes:          []string{"9000"},
		Headers:          headers,
	}
	c, err := NewClient(&config)
	if err != nil {
		t.Fatal(err)
	}
	go c.Run()
	//wait for the fake server to receive the request
	wg.Wait()
	c.Close()
	if gotFoo != "Bar" {
		t.Fatalf("expected header Foo to be 'Bar', got %q", gotFoo)
	}
}

func TestFallbackLegacyFingerprint(t *testing.T) {
	config := Config{
		Fingerprint: "a5:32:92:c6:56:7a:9e:61:26:74:1b:81:a6:f5:1b:44",
	}
	c, err := NewClient(&config)
	if err != nil {
		t.Fatal(err)
	}
	r := ccrypto.NewDetermRand([]byte("test123"))
	priv, err := ccrypto.GenerateKeyGo119(elliptic.P256(), r)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := ssh.NewPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	err = c.verifyServer("", nil, pub)
	if err != nil {
		t.Fatal(err)
	}
}

func TestVerifyLegacyFingerprint(t *testing.T) {
	config := Config{
		Fingerprint: "a5:32:92:c6:56:7a:9e:61:26:74:1b:81:a6:f5:1b:44",
	}
	c, err := NewClient(&config)
	if err != nil {
		t.Fatal(err)
	}
	r := ccrypto.NewDetermRand([]byte("test123"))
	priv, err := ccrypto.GenerateKeyGo119(elliptic.P256(), r)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := ssh.NewPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	err = c.verifyLegacyFingerprint(pub)
	if err != nil {
		t.Fatal(err)
	}
}

func TestVerifyFingerprint(t *testing.T) {
	config := Config{
		Fingerprint: "qmrRoo8MIqePv3jC8+wv49gU6uaFgD3FASQx9V8KdmY=",
	}
	c, err := NewClient(&config)
	if err != nil {
		t.Fatal(err)
	}
	r := ccrypto.NewDetermRand([]byte("test123"))
	priv, err := ccrypto.GenerateKeyGo119(elliptic.P256(), r)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := ssh.NewPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	err = c.verifyServer("", nil, pub)
	if err != nil {
		t.Fatal(err)
	}
}
