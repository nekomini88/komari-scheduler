package main

import (
	"testing"
)

func TestB2i(t *testing.T) {
	cases := []struct {
		in   bool
		want int64
	}{
		{true, 1},
		{false, 0},
	}
	for _, c := range cases {
		got := b2i(c.in)
		if got != c.want {
			t.Errorf("b2i(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestEnv(t *testing.T) {
	t.Setenv("KOMARI_TEST_VAR", "hello")
	got := env("KOMARI_TEST_VAR", "default")
	if got != "hello" {
		t.Errorf("env() = %q, want %q", got, "hello")
	}
}

func TestEnvDefault(t *testing.T) {
	got := env("KOMARI_TEST_VAR_NOT_SET_AT_ALL", "fallback")
	if got != "fallback" {
		t.Errorf("env() = %q, want %q", got, "fallback")
	}
}
