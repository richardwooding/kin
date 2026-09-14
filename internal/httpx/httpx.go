// Package httpx holds the identification every outbound client sends, so the
// sites kin talks to can see one consistent, contactable user agent.
package httpx

const (
	// Version is the toolkit version reported in the user agent.
	Version = "0.1"
	// UserAgent identifies kin to the services it queries.
	UserAgent = "kin/" + Version + " (+https://github.com/richardwooding/kin)"
	// AppID is the application id sent to the WikiTree API.
	AppID = "kin"
)
