package api

import "testing"

func TestEgressDNSNameRequiresExactQualifiedName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		valid bool
	}{
		{name: "api.example.com", valid: true},
		{name: "API.Example.COM.", valid: true},
		{name: "*.example.com", valid: false},
		{name: "example", valid: false},
		{name: "metadata.google.internal", valid: true},
		{name: "-invalid.example.com", valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := validEgressDNSName(test.name); actual != test.valid {
				t.Fatalf("validEgressDNSName(%q)=%v, want %v", test.name, actual, test.valid)
			}
		})
	}
}
