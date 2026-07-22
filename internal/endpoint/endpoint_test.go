package endpoint

import "testing"

func TestNewInfersType(t *testing.T) {
	cases := []struct {
		target string
		want   RecordType
	}{
		{"10.0.0.1", TypeA},
		{"fd00::5", TypeAAAA},
		{"anchor.example.com", TypeCNAME},
	}
	for _, c := range cases {
		if got := New("x.example.com", []string{c.target}, "", 0).RecordType; got != c.want {
			t.Errorf("target %q: got %s, want %s", c.target, got, c.want)
		}
	}
	if got := New("x", nil, "", 0).RecordType; got != TypeCNAME {
		t.Errorf("empty targets: got %s, want CNAME", got)
	}
	if got := New("x", []string{"10.0.0.1"}, TypeTXT, 0).RecordType; got != TypeTXT {
		t.Errorf("explicit type must win over inference: got %s", got)
	}
}

func TestKeyDistinguishesType(t *testing.T) {
	a := New("x.example.com", []string{"10.0.0.1"}, "", 0)
	aaaa := New("x.example.com", []string{"fd00::1"}, "", 0)
	if a.Key() == aaaa.Key() {
		t.Fatal("same name, different type must yield different keys")
	}
	if a.Key() != "A/x.example.com" {
		t.Fatalf("key = %q, want A/x.example.com", a.Key())
	}
}
