package util

import (
	"net"
	"strconv"
	"testing"
)

func TestValidBYOKUpstreamAllowsLoopbackHTTPOnly(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	listener.Close()
	for _, upstream := range []string{
		"http://127.0.0.1:" + port + "/v1",
		"http://localhost:" + port + "/v1",
		"http://[::1]:" + port + "/v1",
		"https://api.example.com/v1",
		"https://api.example.com:443/v1",
		"https://api.example.com:8443/v1",
	} {
		if !ValidBYOKUpstream(upstream) {
			t.Errorf("ValidBYOKUpstream(%q) = false", upstream)
		}
	}
	for _, bad := range []string{
		"http://192.0.2.1:" + port + "/v1",
		"https://api.example.com:8080/v1",
		"https://api.example.com:22/v1",
		"https://api.example.com:9200/v1",
	} {
		if ValidBYOKUpstream(bad) {
			t.Fatalf("ValidBYOKUpstream(%q) want false, got true", bad)
		}
	}
}
