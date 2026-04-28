import Foundation

/// Seed HubCenter URLs — must stay in sync with
/// corelib/remote/defaults.go DefaultRemoteHubCenterURLs.
enum AppConfiguration {
    static let hubCenterURLs = [
        "https://hubs.mypapers.top",
        "https://hubs.maclaw.top",
        "https://hubs2.maclaw.top",
    ]
    /// Default: first seed URL.
    static let hubCenterURL = hubCenterURLs[0]
    static let startURL = "bootstrap"
}
