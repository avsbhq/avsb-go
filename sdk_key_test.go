package avsb

import (
	"strings"
	"testing"
)

// TestValidateSDKKeyShape covers the four bad keys people actually paste, plus
// the shapes that are fine. A bad key used to be invisible: the CDN 404s and
// every flag silently serves its default, so the check exists to turn a dead SDK
// into one line naming what is wrong.
func TestValidateSDKKeyShape(t *testing.T) {
	cases := []struct {
		name        string
		key         string
		wantValid   bool
		wantMention string
	}{
		{
			name:      "a real production key",
			key:       "sdk_production_ttqm0eaj4vth1krcb2xn",
			wantValid: true,
		},
		{
			name:      "an environment name with an underscore",
			key:       "sdk_staging_eu_ttqm0eaj4vth1krcb2xn",
			wantValid: true,
		},
		{
			name:        "empty",
			key:         "",
			wantValid:   false,
			wantMention: "no SDK key was passed",
		},
		{
			name:        "a pasted dashboard URL",
			key:         "https://app.avsb.cloud/settings/environments",
			wantValid:   false,
			wantMention: "looks like a URL",
		},
		{
			name:        "a personal access token in the wrong field",
			key:         "avsb_pat_abcdefghijklmnop",
			wantValid:   false,
			wantMention: "API token",
		},
		{
			name:        "a service token in the wrong field",
			key:         "avsb_svc_abcdefghijklmnop",
			wantValid:   false,
			wantMention: "API token",
		},
		{
			name:        "a prefix that never existed",
			key:         "sdk-production-abc123",
			wantValid:   false,
			wantMention: "never existed",
		},
		{
			name:        "another invented prefix",
			key:         "pub_production_abc123",
			wantValid:   false,
			wantMention: "never existed",
		},
		{
			name:        "no sdk_ prefix at all",
			key:         "production_ttqm0eaj4vth1krcb2xn",
			wantValid:   false,
			wantMention: `does not start with "sdk_"`,
		},
		{
			name:        "truncated to two segments",
			key:         "sdk_production",
			wantValid:   false,
			wantMention: "missing the environment segment",
		},
		{
			name:        "an id that is too short to be generated",
			key:         "sdk_production_ab",
			wantValid:   false,
			wantMention: "too short",
		},
		{
			name:        "a copy-paste that picked up a newline",
			key:         "sdk_production_ttqm0eaj4vth1krcb2xn\n",
			wantValid:   false,
			wantMention: "whitespace",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			valid, message := ValidateSDKKeyShape(tc.key)
			if valid != tc.wantValid {
				t.Fatalf("valid: want %v, got %v (message %q)", tc.wantValid, valid, message)
			}
			if tc.wantValid {
				if message != "" {
					t.Errorf("a valid key needs no message, got %q", message)
				}
				return
			}
			if !strings.Contains(message, tc.wantMention) {
				t.Errorf("the message must explain the problem (%q); got %q", tc.wantMention, message)
			}
			// Every message has to say where the real key lives and that the
			// SDK is carrying on regardless.
			for _, want := range []string{"Environments", "sdk_<environment>_<id>", "keep going"} {
				if !strings.Contains(message, want) {
					t.Errorf("the message must mention %q; got %q", want, message)
				}
			}
		})
	}
}

// TestDescribeSDKKeyRedacts covers the part of the message that gets pasted into
// a bug report: the environment name is the useful half, the id tail is not.
func TestDescribeSDKKeyRedacts(t *testing.T) {
	cases := []struct {
		name string
		key  string
		want string
	}{
		{name: "empty", key: "", want: "(empty)"},
		{name: "short keys are shown whole", key: "sdk_prod", want: "sdk_prod"},
		{
			name: "a long key keeps its head and tail",
			key:  "sdk_production_ttqm0eaj4vth1krcb2xn",
			want: "sdk_producti...b2xn",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DescribeSDKKey(tc.key); got != tc.want {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}

	t.Run("the middle of a real key never appears", func(t *testing.T) {
		key := "sdk_production_ttqm0eaj4vth1krcb2xn"
		got := DescribeSDKKey(key)
		if strings.Contains(got, "ttqm0eaj4vth1krc") {
			t.Errorf("the generated id must be redacted, got %q", got)
		}
	})
}

// TestABadKeyDoesNotStopConstruction covers the deliberate leniency: the mint
// format can grow, so an unrecognised key is logged and used, and the datafile
// fetch is the real arbiter.
func TestABadKeyDoesNotStopConstruction(t *testing.T) {
	server, err := NewServer(ServerOptions{
		SDKKey:          "definitely-not-a-key",
		PollingDisabled: true,
		Events:          EventOptions{Disabled: true},
		Logger:          silentLogger(),
	})
	if err != nil {
		t.Fatalf("a suspicious key must not fail construction: %v", err)
	}
	defer server.Close()
	if server.datafileURL == "" {
		t.Error("the SDK must still build a datafile URL from the value it was given")
	}
	if !strings.Contains(server.datafileURL, "definitely-not-a-key") {
		t.Errorf("the URL uses the key as passed, got %q", server.datafileURL)
	}
}
