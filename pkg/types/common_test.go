package types

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHash_IsValid(t *testing.T) {
	tests := []struct {
		name  string
		input Hash
		want  bool
	}{
		{
			name:  "Valid Hash (64 chars)",
			input: Hash(strings.Repeat("a", 64)),
			want:  true,
		},
		{
			name:  "Too Short",
			input: Hash("abc"),
			want:  false,
		},
		{
			name:  "Empty",
			input: Hash(""),
			want:  false,
		},
		{
			name:  "Too Long",
			input: Hash(strings.Repeat("a", 65)),
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.input.IsValid())
		})
	}
}

func TestHash_String(t *testing.T) {
	s := "aabbcc"
	h := Hash(s)
	assert.Equal(t, s, h.String())
	assert.False(t, h.IsZero())

	var zero Hash
	assert.True(t, zero.IsZero())
}

func TestHashPrefix_String(t *testing.T) {
	p := HashPrefix("aa")
	assert.Equal(t, "aa", p.String())
}
