package solidity

import "testing"

// Unit test for the text-shape parser used by V2.5 / V6 V4. Keeps
// regression guards on the brace / for-clause / trailing-qualifier
// handling and the leading / tail split independent of the AST
// recovery path.
func TestParseFileLevelOperatorForm(t *testing.T) {
	cases := []struct {
		name        string
		input       string
		wantEntries []usingBindingEntry
		wantType    string
		wantOk      bool
	}{
		{
			name:        "single free function global",
			input:       "using {mul as *} for uint256 global;",
			wantEntries: []usingBindingEntry{{leading: "mul"}},
			wantType:    "uint256",
			wantOk:      true,
		},
		{
			name:  "library method form",
			input: "using {Math.add as +} for uint256;",
			wantEntries: []usingBindingEntry{
				{leading: "Math", tail: "add"},
			},
			wantType: "uint256",
			wantOk:   true,
		},
		{
			name:  "multi-function dedup",
			input: "using {add as +, sub as -, add as +} for uint256 global;",
			wantEntries: []usingBindingEntry{
				{leading: "add"},
				{leading: "sub"},
			},
			wantType: "uint256",
			wantOk:   true,
		},
		{
			name:  "namespace-aliased free function",
			input: "using {M.mul as +} for uint256 global;",
			wantEntries: []usingBindingEntry{
				{leading: "M", tail: "mul"},
			},
			wantType: "uint256",
			wantOk:   true,
		},
		{
			name:        "no braces",
			input:       "using SafeMath for uint256;",
			wantEntries: nil,
			wantType:    "",
			wantOk:      false,
		},
		{
			name:        "no for clause",
			input:       "using {mul as *};",
			wantEntries: nil,
			wantType:    "",
			wantOk:      false,
		},
		{
			name:        "empty body",
			input:       "using {} for uint256;",
			wantEntries: nil,
			wantType:    "",
			wantOk:      false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entries, typeName, ok := parseFileLevelOperatorForm(tc.input)
			if ok != tc.wantOk {
				t.Fatalf("ok: got %v want %v", ok, tc.wantOk)
			}
			if !tc.wantOk {
				return
			}
			if typeName != tc.wantType {
				t.Errorf("type: got %q want %q", typeName, tc.wantType)
			}
			if len(entries) != len(tc.wantEntries) {
				t.Fatalf("entries: got %v want %v", entries, tc.wantEntries)
			}
			for i := range entries {
				if entries[i] != tc.wantEntries[i] {
					t.Errorf("entries[%d]: got %+v want %+v", i, entries[i], tc.wantEntries[i])
				}
			}
		})
	}
}
