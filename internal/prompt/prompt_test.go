package prompt

import "testing"

func TestChatML_Render_WithSystem(t *testing.T) {
	got := ChatML{}.Render("be terse", "hello")
	want := "<|im_start|>system\nbe terse<|im_end|>\n<|im_start|>user\nhello<|im_end|>\n<|im_start|>assistant\n"
	if got != want {
		t.Errorf("Render =\n%q\nwant\n%q", got, want)
	}
}

func TestChatML_Render_EmptySystemOmitsSystemTurn(t *testing.T) {
	got := ChatML{}.Render("   ", "hello")
	want := "<|im_start|>user\nhello<|im_end|>\n<|im_start|>assistant\n"
	if got != want {
		t.Errorf("Render with blank system =\n%q\nwant\n%q", got, want)
	}
}

func TestChatML_Stop(t *testing.T) {
	stop := ChatML{}.Stop()
	if len(stop) != 1 || stop[0] != "<|im_end|>" {
		t.Errorf("Stop() = %v, want [<|im_end|>]", stop)
	}
}
