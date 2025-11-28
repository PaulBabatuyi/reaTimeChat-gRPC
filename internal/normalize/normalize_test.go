package normalize

import "testing"

func TestEmail(t *testing.T) {
    in := "  John.DOE@Example.COM  "
    want := "john.doe@example.com"
    got := Email(in)
    if got != want {
        t.Fatalf("Normalize.Email(%q) = %q, want %q", in, got, want)
    }
}
