package update

import "testing"

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    Version
		wantErr string
	}{
		{name: "v prefix", raw: "v1.2.3", want: Version{1, 2, 3}},
		{name: "no prefix", raw: "0.3.1", want: Version{0, 3, 1}},
		{name: "build metadata ignored", raw: "v1.0.0+build", want: Version{1, 0, 0}},
		{name: "build metadata dotted", raw: "v1.0.0+abc.def", want: Version{1, 0, 0}},
		{name: "empty build rejected", raw: "v1.2.3+", wantErr: "invalid version"},
		{name: "malformed build char", raw: "v1.2.3+bad!", wantErr: "invalid version"},
		{name: "dotted empty build", raw: "v1.2.3+abc.", wantErr: "invalid version"},
		{name: "dev rejected", raw: "dev", wantErr: "development build"},
		{name: "empty rejected", raw: "", wantErr: "empty"},
		{name: "prerelease rejected", raw: "v1.0.0-beta", wantErr: "prerelease"},
		{name: "invalid parts", raw: "v1.2", wantErr: "invalid version"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseVersion(tt.raw)
			if tt.wantErr != "" {
				if err == nil || !contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want contains %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseVersion: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestVersionCompareAndIsNewer(t *testing.T) {
	local := Version{0, 3, 1}
	remote := Version{0, 3, 2}
	if !IsNewer(local, remote) {
		t.Fatal("expected remote newer")
	}
	if IsNewer(remote, local) {
		t.Fatal("expected local not newer than remote")
	}
	if IsNewer(local, local) {
		t.Fatal("equal versions are not newer")
	}
	if got := remote.String(); got != "v0.3.2" {
		t.Fatalf("String = %q", got)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (s == sub || len(s) > len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()))
}
