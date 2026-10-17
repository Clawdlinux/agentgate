package receipt

import (
	"crypto/sha256"
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"testing"

	"github.com/gowebpki/jcs"
)

// legacyParamsDigest preserves the original JCS hash path for differential tests.
func legacyParamsDigest(raw []byte) ([32]byte, error) {
	canonical, err := jcs.Transform(normalizeParams(raw))
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(canonical), nil
}

func TestDigestParams_LegacyCorpus(t *testing.T) {
	corpus := []string{
		"", "null", "{}",
		`{"b":2,"a":1}`, " { \"a\" : 1.0,\n\"b\":2e0 } ",
		`{"nested":{"z":[null,true,false,{},[],{"c":3,"b":2}]},"a":"é"}`,
		`{"unicode":"😀\u2028\u2029","escape":"\b\t\n\f\r\"\\\/\u0000\u001f"}`,
		`{"😀":2,"ﬁ":1,"\r":3,"€":4,"ö":5}`,
		`{"x":9007199254740993,"small":1e-27,"big":1e30,"zero":-0}`,
		`{"x":333333333.33333329,"min":5e-324,"max":1.7976931348623157e308}`,
		`{"array":[-0.0,1E+21,1e-6,1e-7,4.50,2e0],"surrogates":"\ud83d\ude00"}`,
	}
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 1000; i++ {
		corpus = append(corpus, fmt.Sprintf(
			` { "z": [%.17g, {"a":%d,"é":"%c"}], "a": {"b":null,"a":true} } `,
			rng.NormFloat64()*1e20, rng.Int63(), rune(0x100+rng.Intn(0x500))))
		number := math.Float64frombits(rng.Uint64())
		if !math.IsInf(number, 0) && !math.IsNaN(number) {
			corpus = append(corpus, `{"number":`+strconv.FormatFloat(number, 'g', -1, 64)+`}`)
		}
	}

	for _, raw := range corpus {
		want, err := legacyParamsDigest([]byte(raw))
		if err != nil {
			t.Fatalf("legacy digest %q: %v", raw, err)
		}
		got, err := DigestParams([]byte(raw))
		if err != nil || got != want {
			t.Fatalf("digest %q: got=%x want=%x error=%v", raw, got, want, err)
		}
	}
}

func FuzzDigestParamsLegacy(f *testing.F) {
	for _, seed := range []string{
		`{}`, `{"b":2,"a":1}`, `{"nested":[true,null,{"😀":"\u2028\n"}]}`,
		`{"n":-0,"large":1e30,"small":1e-7}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		got, err := DigestParams(raw)
		if err != nil {
			return
		}
		want, err := legacyParamsDigest(raw)
		if err != nil || got != want {
			t.Fatalf("legacy digest mismatch: raw=%q got=%x want=%x error=%v", raw, got, want, err)
		}
	})
}
