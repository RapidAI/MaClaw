package formula

import "testing"

func TestDecodeIDsConcatenates(t *testing.T) {
	vocab, err := parseTokenizer([]byte(`{"model":{"type":"WordLevel","vocab":{"<s>":0,"<pad>":1,"</s>":2,"<unk>":3,"\\frac":4,"{":5,"}":6,"1":7,"2":8}}}`))
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeIDs(vocab, []int64{idBOS, 4, 5, 7, 8, 6, idEOS})
	if err != nil {
		t.Fatal(err)
	}
	if got != `\frac{12}` {
		t.Fatalf("got %q", got)
	}
}

func TestAccelMatchesRuntime(t *testing.T) {
	if Accel() == "" {
		t.Fatal("empty accel")
	}
}
