package main

import (
	"testing"
)

func init () {
	logger = createLogger()
}

func TestContainer_ShouldProxy(t *testing.T) {
	type fields struct {
		Id     string
		Name   string
		Labels map[string]string
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{"No labels", fields{Labels: map[string]string{}}, false},
		{"Only port", fields{Labels: map[string]string{labelProxy: "8080"}}, false},
		{"Vhost and port", fields{Labels: map[string]string{labelVhost: "example.com", labelProxy: "8080"}}, true},
		{"Vhost and invalid port", fields{Labels: map[string]string{labelVhost: "example.com", labelProxy: "bob"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Container{
				Id:     tt.fields.Id,
				Name:   tt.fields.Name,
				Labels: tt.fields.Labels,
			}
			if got := c.ShouldProxy(); got != tt.want {
				t.Errorf("ShouldProxy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainer_Port(t *testing.T) {
	type fields struct {
		Id     string
		Name   string
		Labels map[string]string
	}
	tests := []struct {
		name   string
		fields fields
		want   int
	}{
		{"No labels", fields{Labels: map[string]string{}}, -1},
		{"Text label", fields{Labels: map[string]string{labelProxy: "bob"}}, -1},
		{"Non-integer label", fields{Labels: map[string]string{labelProxy: "3.14159"}}, -1},
		{"Valid label", fields{Labels: map[string]string{labelProxy: "8080"}}, 8080},
		{"Negative", fields{Labels: map[string]string{labelProxy: "-100"}}, -1},
		{"Zero", fields{Labels: map[string]string{labelProxy: "0"}}, -1},
		{"Minimum", fields{Labels: map[string]string{labelProxy: "1"}}, 1},
		{"Maximum", fields{Labels: map[string]string{labelProxy: "65535"}}, 65535},
		{"Too high", fields{Labels: map[string]string{labelProxy: "65536"}}, -1},
		{"Bigger than int64", fields{Labels: map[string]string{labelProxy: "100000000000000000000"}}, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Container{
				Id:     tt.fields.Id,
				Name:   tt.fields.Name,
				Labels: tt.fields.Labels,
			}
			if got := c.Port(); got != tt.want {
				t.Errorf("Port() = %v, want %v", got, tt.want)
			}
		})
	}
}
