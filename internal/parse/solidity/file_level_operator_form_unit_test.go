package solidity

import "testing"

// Unit test for the text-shape parser used by V2.5. Keeps regression
// guards on the brace / for-clause / trailing-qualifier handling
// independent of the AST recovery path.
func TestParseFileLevelOperatorForm(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantLibs  []string
		wantType  string
		wantOk    bool
	}{
		{
			name:     "single free function global",
			input:    "using {mul as *} for uint256 global;",
			wantLibs: []string{"mul"},
			wantType: "uint256",
			wantOk:   true,
		},
		{
			name:     "library method form",
			input:    "using {Math.add as +} for uint256;",
			wantLibs: []string{"Math"},
			wantType: "uint256",
			wantOk:   true,
		},
		{
			name:     "multi-function dedup",
			input:    "using {add as +, sub as -, add as +} for uint256 global;",
			wantLibs: []string{"add", "sub"},
			wantType: "uint256",
			wantOk:   true,
		},
		{
			name:     "no braces",
			input:    "using SafeMath for uint256;",
			wantLibs: nil,
			wantType: "",
			wantOk:   false,
		},
		{
			name:     "no for clause",
			input:    "using {mul as *};",
			wantLibs: nil,
			wantType: "",
			wantOk:   false,
		},
		{
			name:     "empty body",
			input:    "using {} for uint256;",
			wantLibs: nil,
			wantType: "",
			wantOk:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			libs, typeName, ok := parseFileLevelOperatorForm(tc.input)
			if ok != tc.wantOk {
				t.Fatalf("ok: got %v want %v", ok, tc.wantOk)
			}
			if !tc.wantOk {
				return
			}
			if typeName != tc.wantType {
				t.Errorf("type: got %q want %q", typeName, tc.wantType)
			}
			if len(libs) != len(tc.wantLibs) {
				t.Fatalf("libs: got %v want %v", libs, tc.wantLibs)
			}
			for i := range libs {
				if libs[i] != tc.wantLibs[i] {
					t.Errorf("libs[%d]: got %q want %q", i, libs[i], tc.wantLibs[i])
				}
			}
		})
	}
}
