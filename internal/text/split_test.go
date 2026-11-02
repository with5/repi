package text

import (
	"reflect"
	"testing"
)

func TestSplit(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"Hello world. How are you? Fine!", []string{"Hello world.", "How are you?", "Fine!"}},
		{"Mr. Smith met Dr. Jones. They talked.", []string{"Mr. Smith met Dr. Jones.", "They talked."}},
		{"Pi is 3.14 today. Yes.", []string{"Pi is 3.14 today.", "Yes."}},
		{"He said \"Go away.\" Then left.", []string{"He said \"Go away.\"", "Then left."}},
		{"Wait... what?!  Really.", []string{"Wait...", "what?!", "Really."}},
		{"Line one\nLine two\r\n\r\nLine three.", []string{"Line one", "Line two", "Line three."}},
		{"Use e.g. apples. Or J. K. Rowling.", []string{"Use e.g. apples.", "Or J. K. Rowling."}},
		{"  \n -- \n", nil},
		{"No final punctuation", []string{"No final punctuation"}},
	}
	for _, c := range cases {
		if got := Split(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Split(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
