package embedding

import (
	"strconv"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding/gguf"
)

// The special-token pre-partition is the one tokenizer fix the golden corpus
// cannot see: all 10 texts it embeds are ordinary prose, with no double space,
// no tab, no newline and no HTML tag, so the partition is a no-op on every one
// of them and the embedding cosine does not move by a digit.  Only an
// adversarial tokenizer differential catches it.  These tests are that
// differential's unit-level counterpart.

// buildTestTokenizer wires a small vocab and an explicit token_type array.
func buildTestTokenizer(tokens []string, types []int32, opt TokenizerOptions) *Tokenizer {
	scores := make([]float32, len(tokens))
	for i := range scores {
		scores[i] = -float32(i)
	}
	return NewTokenizer(tokens, scores, types, opt)
}

// seq renders ids the way `embc.exe -encode-file` and `tokcheck` print them, so
// the expectations below can be pasted straight from the differential's output.
func seq(ids []int) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	return strings.Join(parts, " ")
}

// The cache must be sorted by descending text length, because the partition
// walks it in order and the longest candidate has to win.
func TestSpecialCacheSortedByDescendingLength(t *testing.T) {
	// "<eos>"=5, "<s>"=3, "  "=2, "\n"=1
	tk := buildTestTokenizer(
		[]string{"<s>", "<eos>", "  ", "\n", "\u2581", "a", "ab"},
		[]int32{tokTypeControl, tokTypeControl, tokTypeUserDefined,
			tokTypeUserDefined, tokTypeNormal, tokTypeNormal, tokTypeNormal},
		NewTokenizerOptions())

	got := tk.SpecialIDs()
	want := []int{1, 0, 2, 3} // <eos>, <s>, "  ", "\n"
	if seq(got) != seq(want) {
		t.Fatalf("SpecialIDs = [%s], want [%s]", seq(got), seq(want))
	}
	// NORMAL tokens must not be in the cache.
	for _, id := range got {
		if id >= 4 {
			t.Fatalf("id %d is NORMAL and must not be special", id)
		}
	}
}

// A run of two spaces must resolve to the special token, not to two space
// markers -- and the id it resolves to is one the BPE can never produce,
// because the vocab's entry holds the literal "  " while the text has been
// escaped to "\u2581\u2581".
func TestPrePartitionMatchesRawTextBeforeEscaping(t *testing.T) {
	opt := TokenizerOptions{AddSpacePrefix: false, AddBOS: true, AddEOS: true, BOSID: 0, EOSID: 1}
	tokens := []string{"<s>", "<eos>", "  ", "\n", "\u2581", "a", "ab"}

	withPart := buildTestTokenizer(tokens,
		[]int32{tokTypeControl, tokTypeControl, tokTypeUserDefined,
			tokTypeUserDefined, tokTypeNormal, tokTypeNormal, tokTypeNormal}, opt)

	// "a  ab": BOS, "a", the "  " special, "ab", EOS.
	got := seq(withPart.Encode("a  ab"))
	if want := "0 5 2 6 1"; got != want {
		t.Fatalf("Encode(%q) = [%s], want [%s]", "a  ab", got, want)
	}

	// With no token_type array the partition is off, the text is escaped to
	// "a\u2581\u2581ab", and id 2 is unreachable -- which is exactly the defect
	// this fix removes.  Asserting the *absence* of 2 is the point: a test that
	// only checked the partitioned case would still pass if the partition were
	// silently skipped for the wrong reason.
	noPart := buildTestTokenizer(tokens, nil, opt)
	gotNo := noPart.Encode("a  ab")
	for _, id := range gotNo {
		if id == 2 {
			t.Fatalf("without token_type the escaped text must not reach id 2; got [%s]", seq(gotNo))
		}
	}
	if seq(gotNo) == seq(withPart.Encode("a  ab")) {
		t.Fatalf("partition on and off produced the same ids [%s]; the test cannot see the fix", seq(gotNo))
	}
}

// A tab is USER_DEFINED in this model too, and must likewise be matched raw.
func TestPrePartitionMatchesTab(t *testing.T) {
	opt := TokenizerOptions{AddSpacePrefix: false, AddBOS: true, AddEOS: true, BOSID: 0, EOSID: 1}
	tk := buildTestTokenizer(
		[]string{"<s>", "<eos>", "\t", "\u2581", "a"},
		[]int32{tokTypeControl, tokTypeControl, tokTypeUserDefined, tokTypeNormal, tokTypeNormal},
		opt)

	if got, want := seq(tk.Encode("a\ta")), "0 4 2 4 1"; got != want {
		t.Fatalf("Encode(%q) = [%s], want [%s]", "a\ta", got, want)
	}
}

// A special token's text is matched against the raw input, so a token that
// looks like markup is matched literally rather than tokenized.
func TestPrePartitionMatchesMultiByteSpecial(t *testing.T) {
	opt := TokenizerOptions{AddSpacePrefix: false, AddBOS: true, AddEOS: true, BOSID: 0, EOSID: 1}
	tk := buildTestTokenizer(
		[]string{"<s>", "<eos>", "<tag>", "\u2581", "a", "<", "t"},
		[]int32{tokTypeControl, tokTypeControl, tokTypeUserDefined,
			tokTypeNormal, tokTypeNormal, tokTypeNormal, tokTypeNormal},
		opt)

	if got, want := seq(tk.Encode("a<tag>a")), "0 4 2 4 1"; got != want {
		t.Fatalf("Encode(%q) = [%s], want [%s]", "a<tag>a", got, want)
	}
}

// The longest special token has to win when two of them match at the same
// offset: "<tag>" must beat "<".
func TestPrePartitionPrefersLongestMatch(t *testing.T) {
	opt := TokenizerOptions{AddSpacePrefix: false, AddBOS: true, AddEOS: true, BOSID: 0, EOSID: 1}
	tk := buildTestTokenizer(
		[]string{"<s>", "<eos>", "<", "<tag>", "\u2581", "a", "t", ">"},
		[]int32{tokTypeControl, tokTypeControl, tokTypeUserDefined, tokTypeUserDefined,
			tokTypeNormal, tokTypeNormal, tokTypeNormal, tokTypeNormal},
		opt)

	got := tk.Encode("<tag>")
	if len(got) != 3 || got[1] != 3 {
		t.Fatalf("Encode(%q) = [%s], want the \"<tag>\" token (id 3) in the middle", "<tag>", seq(got))
	}
}

// An empty input produces no fragment at all, so no dummy prefix is added even
// when add_space_prefix is set -- llama.cpp guards the fragment with
// `if (!raw_text.empty())`.
func TestEmptyInputHasNoDummyPrefix(t *testing.T) {
	opt := TokenizerOptions{AddSpacePrefix: true, AddBOS: true, AddEOS: true, BOSID: 0, EOSID: 1}
	tk := buildTestTokenizer(
		[]string{"<s>", "<eos>", "  ", "\u2581", "a"},
		[]int32{tokTypeControl, tokTypeControl, tokTypeUserDefined, tokTypeNormal, tokTypeNormal},
		opt)

	if got, want := seq(tk.Encode("")), "0 1"; got != want {
		t.Fatalf("Encode(\"\") = [%s], want [%s]", got, want)
	}
}

// Model-backed: the real file's cache is 6414 entries and the ids match the
// ones the verified C++ port produces for the adversarial case.
func TestSpecialCacheAndAdversarialIdsOnRealModel(t *testing.T) {
	modelPath := findModel(t)
	mf, err := gguf.OpenMmap(modelPath)
	if err != nil {
		t.Fatalf("open %s: %v", modelPath, err)
	}
	defer mf.CloseMmap()

	tokens := gguf.GetMetaStrArr(mf.Meta, "tokenizer.ggml.tokens")
	scores := gguf.LastF32Array()
	types := gguf.GetMetaI32Arr(mf.Meta, "tokenizer.ggml.token_type")
	if len(types) == 0 {
		t.Fatalf("tokenizer.ggml.token_type did not parse as an int32 array")
	}
	if len(types) != len(tokens) {
		t.Fatalf("token_type has %d entries, tokens has %d", len(types), len(tokens))
	}

	tk := LoadTokenizerFromGGUF(tokens, scores, types, TokenizerOptionsFromGGUF(mf.Meta))

	if got := len(tk.SpecialIDs()); got != 6414 {
		t.Errorf("special cache size = %d, want 6414 (6251 CONTROL + 163 USER_DEFINED)", got)
	}

	// Vocab entry 138 is the literal two ASCII spaces, marked USER_DEFINED.
	if types[138] != tokTypeUserDefined || tokens[138] != "  " {
		t.Errorf("vocab[138] = %q type %d, want %q type %d",
			tokens[138], types[138], "  ", tokTypeUserDefined)
	}

	// Exact ids, cross-checked against embc.exe -encode-file on the same model.
	cases := []struct{ text, want string }{
		{"  double  space  ", "2 138 7902 138 5780 138 1"},
		{"a\tb", "2 236746 255968 236763 1"},
		{"hello world", "2 23391 1902 1"},
	}
	for _, c := range cases {
		if got := seq(tk.Encode(c.text)); got != c.want {
			t.Errorf("Encode(%q) = [%s], want [%s]", c.text, got, c.want)
		}
	}
}

// Guard the helper itself: seq() is used for every comparison above, so a
// broken seq() would silently make them all pass.
func TestSeqHelper(t *testing.T) {
	if got := seq(nil); got != "" {
		t.Errorf("seq(nil) = %q, want \"\"", got)
	}
	if got := seq([]int{1, 2, 3}); got != "1 2 3" {
		t.Errorf("seq = %q, want \"1 2 3\"", got)
	}
	if got := seq([]int{0, -5}); got != "0 -5" {
		t.Errorf("seq = %q, want \"0 -5\"", got)
	}
}
