package main

import (
	"reflect"
	"testing"
)

func Test_wildcardMatches(t *testing.T) {
	type args struct {
		wildcard string
		domain   string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{"non-matching", args{"example.com", "example.org"}, false},
		{"same level domain", args{"example.com", "example.com"}, false},
		{"single level sub domain", args{"example.com", "foo.example.com"}, true},
		{"multi level sub domain", args{"example.com", "bar.foo.example.com"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wildcardMatches(tt.args.wildcard, tt.args.domain); got != tt.want {
				t.Errorf("wildcardMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_applyWildcards(t *testing.T) {
	type args struct {
		domains   []string
		wildcards []string
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{"no wildcards", args{[]string{"example.com", "example.org"}, []string{}}, []string{"example.com", "example.org"}},
		{"non-matching wildcards", args{[]string{"example.com", "example.org"}, []string{"example.net"}}, []string{"example.com", "example.org"}},
		{"single match", args{[]string{"foo.example.com", "example.org"}, []string{"example.com"}}, []string{"*.example.com", "example.org"}},
		{"multiple matches", args{[]string{"foo.example.com", "example.org", "bar.example.com"}, []string{"example.com"}}, []string{"*.example.com", "example.org"}},
		{"multiple wildcards", args{[]string{"foo.example.com", "baz.example.org", "bar.example.com"}, []string{"example.com", "example.org"}}, []string{"*.example.com", "*.example.org"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := applyWildcards(tt.args.domains, tt.args.wildcards); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("applyWildcards() = %v, want %v", got, tt.want)
			}
		})
	}
}
