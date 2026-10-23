//go:build linux

package uverbs

import (
	"testing"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name  string
		attrs []Attribute
		ok    bool
	}{
		{"valid", []Attribute{{ID: 1, Data: make([]byte, 8)}, {ID: 2, Type: AttributeNewObject}}, true},
		{"empty", nil, true},
		{"duplicate", []Attribute{{ID: 1}, {ID: 1, Type: AttributeOut}}, false},
		{"too long", []Attribute{{ID: 1, Data: make([]byte, MaxAttributeLen+1)}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validate(tt.attrs); (err == nil) != tt.ok {
				t.Fatalf("validate: %v, want ok %v", err, tt.ok)
			}
		})
	}
}
