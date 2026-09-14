// Package httpx holds the identification every outbound client sends, so the
// sites kin talks to can see one consistent, contactable user agent.
package httpx

// Version is the toolkit version reported in the user agent. The command sets
// it from the value injected at build time; "dev" means a local build.
var Version = "dev"

// AppID is the application id sent to the WikiTree API.
const AppID = "kin"

// UserAgent identifies kin, with its version, to the services it queries.
func UserAgent() string {
	return "kin/" + Version + " (+https://github.com/richardwooding/kin)"
}
