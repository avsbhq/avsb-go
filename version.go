package avsb

import "runtime/debug"

// SDKType is the package identifier this SDK reports in heartbeats and
// User-Agent headers. It matches the published module name so the dashboard's
// Environments page can name what connected.
const SDKType = "avsb-go"

// modulePath is the module whose version this SDK reports.
const modulePath = "github.com/avsbhq/avsb-go"

// unknownVersion is what SDKVersion reports when the build carries no module
// metadata. That happens for `go run` of an unversioned main package and
// inside this module's own tests, never for a consumer that ran `go get`.
//
// It is deliberately NOT a hardcoded "1.0.0": a hand-maintained version
// literal drifts from the release the moment someone forgets to bump it, and
// a heartbeat that reports the wrong version is worse than one that admits it
// does not know.
const unknownVersion = "0.0.0-unknown"

// sdkVersion is resolved once at package init from the build's module graph.
var sdkVersion = resolveSDKVersion()

// SDKVersion returns this SDK's version, read from Go module build metadata.
//
//	fmt.Println(avsb.SDKVersion()) // "v1.0.0"
func SDKVersion() string {
	return sdkVersion
}

func resolveSDKVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return unknownVersion
	}
	// Normal case: the consumer's binary depends on this module.
	for _, dep := range info.Deps {
		if dep != nil && dep.Path == modulePath && dep.Version != "" {
			return dep.Version
		}
	}
	// This module IS the main module (its own tests, or a fork built in
	// place). Go reports "(devel)" for an untagged main module.
	if info.Main.Path == modulePath && info.Main.Version != "" &&
		info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return unknownVersion
}
