package clone

import (
	"strings"
	"testing"
)

func TestValidateCloneName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		{name: "valid simple", input: "myclone", wantErr: false},
		{name: "valid with underscore", input: "my_clone", wantErr: false},
		{name: "valid with digits", input: "clone123", wantErr: false},
		{name: "valid single char", input: "a", wantErr: false},
		{name: "empty", input: "", wantErr: true, errMsg: "required"},
		{name: "SQL injection dash", input: "prod;drop", wantErr: true, errMsg: "letters, digits"},
		{name: "with hyphen", input: "prod-copy", wantErr: true, errMsg: "letters, digits"},
		{name: "unicode", input: "pród", wantErr: true, errMsg: "letters, digits"},
		{name: "spaces", input: "my clone", wantErr: true, errMsg: "letters, digits"},
		{name: "dot", input: "prod.copy", wantErr: true, errMsg: "letters, digits"},
		{name: "slash", input: "prod/copy", wantErr: true, errMsg: "letters, digits"},
		{name: "lone open brace", input: "clone{", wantErr: true, errMsg: "letters, digits"},
		{name: "lone close brace", input: "clone}", wantErr: true, errMsg: "letters, digits"},
		{name: "template placeholder raw", input: "db_a_dolly_{n}", wantErr: true, errMsg: "letters, digits"},
		{name: "template db placeholder", input: "{db}_kloned", wantErr: true, errMsg: "letters, digits"},
		{name: "resolved template", input: "kloned_1", wantErr: false},
		{name: "resolved db clone", input: "db_clone_42", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCloneName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateCloneName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestResolveTemplateName(t *testing.T) {
	tests := []struct {
		name     string
		template string
		n        int
		want     string
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "basic resolution",
			template: "kloned_{n}",
			n:        1,
			want:     "kloned_1",
		},
		{
			name:     "db source template",
			template: "db_a_dolly_{n}",
			n:        5,
			want:     "db_a_dolly_5",
		},
		{
			name:     "multiple n counters error",
			template: "kloned_{n}_{n}",
			n:        1,
			wantErr:  true,
			errMsg:   "exactly one",
		},
		{
			name:     "non-n brace pattern error",
			template: "{db}_x",
			n:        1,
			wantErr:  true,
			errMsg:   "unsupported brace",
		},
		{
			name:     "no placeholder error",
			template: "kloned_",
			n:        1,
			wantErr:  true,
			errMsg:   "missing",
		},
		{
			name:     "empty template error",
			template: "",
			n:        1,
			wantErr:  true,
			errMsg:   "required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveTemplateName(tt.template, tt.n)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ResolveTemplateName(%q, %d) = %q, want error", tt.template, tt.n, got)
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.errMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveTemplateName(%q, %d) unexpected error: %v", tt.template, tt.n, err)
			}
			if got != tt.want {
				t.Fatalf("ResolveTemplateName(%q, %d) = %q, want %q", tt.template, tt.n, got, tt.want)
			}
		})
	}
}
