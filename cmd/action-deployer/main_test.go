package main

import (
	"reflect"
	"testing"
)

func TestSplitLines(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"whitespace only", "   \n\t\n", nil},
		{"single", "foo.yaml", []string{"foo.yaml"}},
		{"single with trailing newline", "foo.yaml\n", []string{"foo.yaml"}},
		{"multiple", "a.yaml\nb.yaml\nc.yaml", []string{"a.yaml", "b.yaml", "c.yaml"}},
		{"with blank lines", "a.yaml\n\nb.yaml\n", []string{"a.yaml", "b.yaml"}},
		{"trims whitespace", "  a.yaml  \n\tb.yaml\t", []string{"a.yaml", "b.yaml"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := splitLines(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}
