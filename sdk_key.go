package avsb

import (
	"regexp"
	"strings"
)

// SDK key shape validation at construction.
//
// Real keys are minted as sdk_<environmentKey>_<id>, where the environment key
// matches /^[a-z][a-z0-9_]*$/ (it may itself contain underscores, which is why
// this validates with one anchored pattern instead of splitting on '_') and the
// id is lowercase alphanumerics.
//
// A bad key used to be invisible: the CDN 404s, the fetch fails, and every flag
// silently serves its default. The four most common bad values (a pasted
// dashboard URL, a truncated copy, a snippet key, an API token in the wrong
// field) are all recognisable on sight, so the message names what it got, what
// the shape is, and where the real key lives.
//
// This NEVER fails construction. A key we cannot recognise may still be valid
// (the mint format can grow), so the SDK logs and proceeds with the value it
// was given; the datafile fetch is the real arbiter.

var sdkKeyPattern = regexp.MustCompile(`^sdk_[a-z][a-z0-9_]*_[a-z0-9]{8,}$`)

// exampleSDKKey is real-shaped, so the message is comparable by eye.
const exampleSDKKey = "sdk_production_ttqm0eaj4vth1krcb2xn"

// DescribeSDKKey redacts all but the recognisable head and tail of a key, so a
// message carrying it can be pasted into a bug report. The environment name is
// the useful half ("you used the development key"); the id tail is not.
func DescribeSDKKey(sdkKey string) string {
	if sdkKey == "" {
		return "(empty)"
	}
	if len(sdkKey) <= 12 {
		return sdkKey
	}
	return sdkKey[:12] + "..." + sdkKey[len(sdkKey)-4:]
}

// ValidateSDKKeyShape reports whether the key looks like an SDK key, returning
// the message to log rather than logging itself, so the caller owns the
// transport. An empty message means the shape is fine.
func ValidateSDKKeyShape(sdkKey string) (bool, string) {
	if sdkKeyPattern.MatchString(sdkKey) {
		return true, ""
	}
	return false, "avsb: SDK key \"" + DescribeSDKKey(sdkKey) + "\" does not look like an SDK key: " +
		diagnoseSDKKey(sdkKey) + ". Keys look like " + exampleSDKKey +
		" (sdk_<environment>_<id>). Copy the key for the environment you want from " +
		"app.avsb.cloud, then Settings, then Environments. The SDK will keep going with " +
		"the value you passed, so if the datafile request 404s next, this is why."
}

func diagnoseSDKKey(sdkKey string) string {
	trimmed := strings.TrimSpace(sdkKey)

	switch {
	case trimmed == "":
		return "no SDK key was passed at all"
	case trimmed != sdkKey:
		return "it has leading or trailing whitespace, which usually means a copy-paste picked up a line break"
	case strings.HasPrefix(strings.ToLower(trimmed), "http://"),
		strings.HasPrefix(strings.ToLower(trimmed), "https://"):
		return "it looks like a URL, not a key. Copy the key itself, not the address of the page it is on"
	case strings.HasPrefix(trimmed, "avsb_pat_"), strings.HasPrefix(trimmed, "avsb_svc_"):
		return "it looks like an API token. API tokens talk to the management API; SDKs need the environment SDK key"
	case strings.HasPrefix(trimmed, "sdk-"),
		strings.HasPrefix(trimmed, "pub_"),
		strings.HasPrefix(trimmed, "srv_"):
		return "that key prefix has never existed in A vs B. There is one key format for SDKs"
	case !strings.HasPrefix(trimmed, "sdk_"):
		return "it does not start with \"sdk_\""
	case len(strings.Split(trimmed, "_")) < 3:
		return "it is missing the environment segment, so it may have been truncated"
	default:
		return "the id after the environment name is too short or has characters a generated id never contains, " +
			"so it may have been truncated or edited"
	}
}
