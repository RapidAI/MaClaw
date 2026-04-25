package remote

// DefaultRemoteHubCenterURLs is the ordered list of hub center URLs used for
// discovery. Both MacLaw client and hub server use this list. The first
// reachable node with the highest quality score wins.
var DefaultRemoteHubCenterURLs = []string{
	"https://hubs.mypapers.top",
	"https://hubs.maclaw.top",
	"https://hubs2.maclaw.top",
}

// DefaultRemoteHubCenterURL is kept for backward compatibility.
// New code should use DefaultRemoteHubCenterURLs.
var DefaultRemoteHubCenterURL = DefaultRemoteHubCenterURLs[0]
