package canonical

import (
	"strings"
	"testing"
)

func TestCanonicalizeRFC8785SerializationVector(t *testing.T) {
	input := []byte(`{
  "numbers": [333333333.33333329, 1E30, 4.50, 2e-3, 0.000000000000000000000000001],
  "string": "\u20ac$\u000F\u000aA'\u0042\u0022\u005c\\\"\/",
  "literals": [null, true, false]
}`)
	want := `{"literals":[null,true,false],"numbers":[333333333.3333333,1e+30,4.5,0.002,1e-27],"string":"€$\u000f\nA'B\"\\\\\"/"}`

	got, err := Canonicalize(input)
	if err != nil {
		t.Fatalf("canonicalize RFC vector: %v", err)
	}
	if string(got) != want {
		t.Fatalf("unexpected canonical JSON\nwant: %s\n got: %s", want, got)
	}
}

func TestCanonicalizeSortsObjectKeysByUTF16CodeUnits(t *testing.T) {
	input := []byte(`{"דּ":7,"😀":6,"€":5,"ö":4,"\u0080":3,"1":2,"\r":1}`)
	want := "{\"\\r\":1,\"1\":2,\"\u0080\":3,\"ö\":4,\"€\":5,\"😀\":6,\"דּ\":7}"

	got, err := Canonicalize(input)
	if err != nil {
		t.Fatalf("canonicalize UTF-16 ordering vector: %v", err)
	}
	if string(got) != want {
		t.Fatalf("unexpected key order\nwant: %s\n got: %s", want, got)
	}
}

func TestCanonicalizeRejectsDuplicateObjectKeys(t *testing.T) {
	_, err := Canonicalize([]byte(`{"a":1,"\u0061":2}`))
	if err == nil || !strings.Contains(err.Error(), "Duplicate key") {
		t.Fatalf("expected duplicate key rejection, got %v", err)
	}
}

func TestCanonicalizeRejectsInvalidUTF8(t *testing.T) {
	_, err := Canonicalize([]byte{'{', '"', 0xff, '"', ':', '1', '}'})
	if err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("expected invalid UTF-8 rejection, got %v", err)
	}
}
