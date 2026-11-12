package preview

import "testing"

func TestParseLsof(t *testing.T) {
	raw := []byte(`p101
cnode
n127.0.0.1:3000
p202
cgo
n*:8080
p202
cgo
n[::1]:8080
p303
cpython3
n127.0.0.1:5173
`)
	servers := Parse(raw)
	if len(servers) != 3 {
		t.Fatalf("servers = %#v", servers)
	}
	if servers[0].Port != 3000 || servers[0].Process != "node" || servers[0].URL != "http://localhost:3000" {
		t.Fatalf("first = %#v", servers[0])
	}
	if servers[1].Port != 5173 || servers[1].PID != 303 {
		t.Fatalf("second = %#v", servers[1])
	}
	if servers[2].Port != 8080 {
		t.Fatalf("third = %#v", servers[2])
	}
}
