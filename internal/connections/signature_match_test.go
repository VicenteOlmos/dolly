package connections

import "testing"

func sharedSig() string {
	return Connection{Host: "h", Port: "5432", Database: "db", User: "u"}.Signature()
}

func TestResolveBySignatureSingleMatch(t *testing.T) {
	all := []Connection{
		{Name: "staging", Host: "h", Port: "5432", Database: "db", User: "u"},
	}
	got, ok, err := ResolveBySignature(all, sharedSig(), "")
	if err != nil || !ok || got.Name != "staging" {
		t.Fatalf("ResolveBySignature() = (%+v, %v, %v)", got, ok, err)
	}
}

func TestResolveBySignatureAmbiguousWithoutPreferName(t *testing.T) {
	all := []Connection{
		{Name: "alpha", Host: "h", Port: "5432", Database: "db", User: "u"},
		{Name: "beta", Host: "h", Port: "5432", Database: "db", User: "u"},
	}
	_, ok, err := ResolveBySignature(all, sharedSig(), "")
	if ok || err == nil {
		t.Fatalf("expected ambiguity error, got ok=%v err=%v", ok, err)
	}
}

func TestResolveBySignatureAmbiguousWithPreferName(t *testing.T) {
	all := []Connection{
		{Name: "alpha", Host: "h", Port: "5432", Database: "db", User: "u"},
		{Name: "beta", Host: "h", Port: "5432", Database: "db", User: "u"},
	}
	got, ok, err := ResolveBySignature(all, sharedSig(), "beta")
	if err != nil || !ok || got.Name != "beta" {
		t.Fatalf("ResolveBySignature() = (%+v, %v, %v)", got, ok, err)
	}
}

func TestOtherNamesWithSignature(t *testing.T) {
	all := []Connection{
		{Name: "alpha", Host: "h", Port: "5432", Database: "db", User: "u"},
		{Name: "beta", Host: "h", Port: "5432", Database: "db", User: "u"},
	}
	names := OtherNamesWithSignature(all, sharedSig(), "beta")
	if len(names) != 1 || names[0] != "alpha" {
		t.Fatalf("OtherNamesWithSignature() = %v, want [alpha]", names)
	}
}
