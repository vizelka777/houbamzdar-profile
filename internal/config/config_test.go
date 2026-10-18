package config

import "testing"

func TestCaptureAIValidationDisabledSetting(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"", false}, {"false", false}, {"true", true}, {" true ", true}, {"invalid", false},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("CAPTURE_AI_VALIDATION_DISABLED", tc.value)
			if got := Load().CaptureAIValidationDisabled; got != tc.want {
				t.Fatalf("disabled = %v, want %v", got, tc.want)
			}
		})
	}
}
