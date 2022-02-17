package main

import (
	"reflect"
	"testing"
)

func Test_splitList(t *testing.T) {
	type args struct {
		input string
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{"empty string", args{""}, []string{}},
		{"single item", args{"test"}, []string{"test"}},
		{"spaces", args{"test1 test2 test3"}, []string{"test1", "test2", "test3"}},
		{"commas", args{"test1,test2,test3"}, []string{"test1", "test2", "test3"}},
		{"gaps", args{"test1   test2  test3 "}, []string{"test1", "test2", "test3"}},
		{"mixed", args{"  test1, test2 test3,,,"}, []string{"test1", "test2", "test3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := splitList(tt.args.input); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitList() = %v, want %v", got, tt.want)
			}
		})
	}
}
