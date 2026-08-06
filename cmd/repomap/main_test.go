package main

import (
	"reflect"
	"testing"
)

func TestDefaultToScan(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"bare invocation runs scan", nil, []string{"scan"}},
		{"leading scan flag prepends scan", []string{"-n", "ns"}, []string{"scan", "-n", "ns"}},
		{"bare path prepends scan", []string{"some/path"}, []string{"scan", "some/path"}},
		{"long help reaches root", []string{"--help"}, []string{"--help"}},
		{"short help reaches root", []string{"-h"}, []string{"-h"}},
		{"help command untouched", []string{"help"}, []string{"help"}},
		{"completion untouched", []string{"completion", "zsh"}, []string{"completion", "zsh"}},
		{"deps subcommand untouched", []string{"deps", "."}, []string{"deps", "."}},
		{"deps help untouched", []string{"deps", "-h"}, []string{"deps", "-h"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := defaultToScan(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("defaultToScan(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
