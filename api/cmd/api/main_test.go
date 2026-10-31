package main

import "testing"

func TestCheckDevAuth(t *testing.T) {
	cases := []struct {
		addr   string
		secure bool
		ok     bool
	}{
		{"127.0.0.1:8080", false, true},
		{"localhost:8080", false, true},
		{"[::1]:8080", false, true},
		{"127.0.0.1:8080", true, false},
		{":8080", false, false},
		{"0.0.0.0:8080", false, false},
		{"[::]:8080", false, false},
		{"10.0.0.5:8080", false, false},
		{"example.com:8080", false, false},
		{"8080", false, false},
	}
	for _, c := range cases {
		err := checkDevAuth(c.addr, c.secure)
		if (err == nil) != c.ok {
			t.Errorf("checkDevAuth(%q, %v) = %v, want ok=%v", c.addr, c.secure, err, c.ok)
		}
	}
}
