package config

import (
	"testing"
)

func TestParseCookieString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantKeys map[string]string
	}{
		{
			name:  "full cookie string with token",
			input: "c_user=61581248120082;xs=16:HwwMNsa3FyKhlA:2:1758253542:-1:-1;fr=0PS1MEwHphr6cms2R.AWfgDmmK6iydsg;datr=5dHMaDdz8w6IaeiJMuw85B5E|EAAAAUaZA8jlABPWAs",
			wantKeys: map[string]string{
				"c_user":       "61581248120082",
				"xs":           "16:HwwMNsa3FyKhlA:2:1758253542:-1:-1",
				"fr":           "0PS1MEwHphr6cms2R.AWfgDmmK6iydsg",
				"datr":         "5dHMaDdz8w6IaeiJMuw85B5E",
				"access_token": "EAAAAUaZA8jlABPWAs",
			},
		},
		{
			name:  "cookie string without token",
			input: "c_user=123;xs=abc;fr=def;datr=ghi",
			wantKeys: map[string]string{
				"c_user": "123",
				"xs":     "abc",
				"fr":     "def",
				"datr":   "ghi",
			},
		},
		{
			name:     "empty string",
			input:    "",
			wantKeys: map[string]string{},
		},
		{
			name:  "with spaces",
			input: " c_user=123 ; xs=abc ; fr=def ",
			wantKeys: map[string]string{
				"c_user": "123",
				"xs":     "abc",
				"fr":     "def",
			},
		},
		{
			name:  "token only",
			input: "|EAAAA123",
			wantKeys: map[string]string{
				"access_token": "EAAAA123",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseCookieString(tt.input)
			if len(got) != len(tt.wantKeys) {
				t.Errorf("ParseCookieString() returned %d keys, want %d. Got: %v", len(got), len(tt.wantKeys), got)
				return
			}
			for k, wantV := range tt.wantKeys {
				if gotV, ok := got[k]; !ok {
					t.Errorf("ParseCookieString() missing key %q", k)
				} else if gotV != wantV {
					t.Errorf("ParseCookieString()[%q] = %q, want %q", k, gotV, wantV)
				}
			}
		})
	}
}
