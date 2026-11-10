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

func TestSplitChinese(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"今天天气很好。我们去公园吧！你去吗？", []string{"今天天气很好。", "我们去公园吧！", "你去吗？"}},
		{"他说：“走吧。”然后就走了。", []string{"他说：“走吧。”", "然后就走了。"}},
		{"真的吗？！不会吧……我不信。", []string{"真的吗？！", "不会吧……我不信。"}},
		{"圆周率是 3.14，对吧?对!", []string{"圆周率是 3.14，对吧?", "对!"}},
		{"我在学 Python. 它很好用。", []string{"我在学 Python.", "它很好用。"}},
		{"第一行\n第二行。", []string{"第一行", "第二行。"}},
		{"《红楼梦》很长。「好」", []string{"《红楼梦》很长。", "「好」"}},
	}
	for _, c := range cases {
		if got := Split(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Split(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsChinese(t *testing.T) {
	cases := map[string]bool{
		"今天天气很好。":                                                             true,
		"我在学 machine learning 和 Python。":                                      true,
		"The quick brown fox jumps over the lazy dog.":                        false,
		"The word 你好 means hello in Chinese, and people say it all the time.": false,
		"":         false,
		"3.14 ...": false,
	}
	for in, want := range cases {
		if got := IsChinese(in); got != want {
			t.Errorf("IsChinese(%q) = %v, want %v", in, got, want)
		}
	}
}
