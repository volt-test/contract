package definition_test

import (
	"testing"

	"github.com/volt-test/contract/definition"
)

func TestResolveURL(t *testing.T) {
	cases := []struct{ base, ref, want string }{
		{"https://api.example.com", "/items", "https://api.example.com/items"},
		{"https://api.example.com/", "/items", "https://api.example.com/items"},
		{"https://api.example.com///", "/items", "https://api.example.com/items"},
		{"https://api.example.com/v1", "/items", "https://api.example.com/v1/items"},
		{"https://api.example.com/v1/", "/", "https://api.example.com/v1/"},
		{"https://api.example.com", "/items?page=2&q=a b", "https://api.example.com/items?page=2&q=a b"},
		{"https://api.example.com", "/items/${id}?expand=${field}", "https://api.example.com/items/${id}?expand=${field}"},
		{"${base_url}", "/health", "${base_url}/health"},
		{"https://${host}:${port}/", "/x", "https://${host}:${port}/x"},
		{"https://api.example.com", "/caf%C3%A9", "https://api.example.com/caf%C3%A9"},
		// Not relative: returned unchanged.
		{"https://api.example.com", "https://other.example.com/a", "https://other.example.com/a"},
		{"https://api.example.com", "${url}/a", "${url}/a"},
		{"https://api.example.com", "//cdn.example.com/a", "//cdn.example.com/a"},
		{"https://api.example.com", "items", "items"},
		{"https://api.example.com", "", ""},
		// No base: returned unchanged.
		{"", "/items", "/items"},
	}
	for _, tc := range cases {
		if got := definition.ResolveURL(tc.base, tc.ref); got != tc.want {
			t.Errorf("ResolveURL(%q, %q) = %q, want %q", tc.base, tc.ref, got, tc.want)
		}
	}
}
