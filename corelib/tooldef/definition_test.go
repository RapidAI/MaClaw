package tooldef

import "testing"

func TestNameSupportsOpenAIAndLegacyShapes(t *testing.T) {
	tests := []struct {
		name string
		def  map[string]interface{}
		want string
	}{
		{
			name: "openai function",
			def: map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name": "ssh",
				},
			},
			want: "ssh",
		},
		{
			name: "legacy flat",
			def:  map[string]interface{}{"name": "bash"},
			want: "bash",
		},
		{
			name: "missing",
			def:  map[string]interface{}{"function": map[string]interface{}{}},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Name(tt.def); got != tt.want {
				t.Fatalf("Name() = %q, want %q", got, tt.want)
			}
		})
	}
}
