package main

import "testing"

func TestExposedWithoutAuth(t *testing.T) {
	open := Settings{}
	withProxy := Settings{ProxyToken: "t"}
	withAdmin := Settings{AdminToken: "t"}

	cases := []struct {
		addr string
		st   Settings
		want bool
	}{
		{"127.0.0.1:8787", open, false},
		{"[::1]:8787", open, false},
		{"localhost:8787", open, false},
		{"0.0.0.0:8787", open, true},
		{"192.168.1.20:8787", open, true},
		{"0.0.0.0:8787", withProxy, false},
		{"0.0.0.0:8787", withAdmin, false},
	}
	for _, c := range cases {
		if got := exposedWithoutAuth(c.addr, c.st); got != c.want {
			t.Errorf("exposedWithoutAuth(%q, %+v) = %v, want %v", c.addr, c.st, got, c.want)
		}
	}
}
