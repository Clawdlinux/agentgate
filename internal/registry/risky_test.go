package registry

import "testing"

func TestRiskyBoolean(t *testing.T) {
	for _, tc := range []struct {
		value   string
		want    bool
		invalid bool
	}{
		{"", false, false}, {"risky: true", true, false}, {"risky: false", false, false},
		{`risky: "true"`, false, true}, {"risky: sometimes", false, true}, {"risky: 1", false, true},
		{"risky: null", false, true}, {"risky: yes", false, true},
	} {
		r := New()
		err := r.LoadBytes([]byte("services:\n  test:\n    base_url: https://example.com\n    actions:\n      write:\n        method: POST\n        path: /write\n        " + tc.value + "\n"))
		if (err != nil) != tc.invalid {
			t.Fatalf("%q: %v", tc.value, err)
		}
		if err == nil {
			_, a, _ := r.GetAction("test", "write")
			if a.Risky != tc.want {
				t.Fatal(tc.value)
			}
		}
	}
}
