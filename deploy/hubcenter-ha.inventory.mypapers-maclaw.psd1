@{
    ClusterSecret = 'replace-with-a-long-random-shared-secret'

    HubCenters = @(
        @{
            NodeID        = 'hc-1'
            NodeName      = 'hubcenter-1'
            PublicBaseURL = 'https://hubs.mypapers.top'
            AdvertiseURL  = 'http://hub.mypapers.top:9388'
            DatabaseDSN   = './data/hubcenter-hc-1.db'
        },
        @{
            NodeID        = 'hc-2'
            NodeName      = 'hubcenter-2'
            PublicBaseURL = 'https://hubs.maclaw.top'
            AdvertiseURL  = 'http://107.172.86.131:9388'
            DatabaseDSN   = './data/hubcenter-hc-2.db'
        },
        @{
            NodeID        = 'hc-3'
            NodeName      = 'hubcenter-3'
            PublicBaseURL = 'https://hubs2.maclaw.top'
            AdvertiseURL  = 'http://66.154.113.63:9388'
            DatabaseDSN   = './data/hubcenter-hc-3.db'
        }
    )

    Hubs = @(
        @{
            FileName             = 'hub-mypapers.yaml'
            PublicBaseURL        = 'https://hub.mypapers.top'
            PrimaryCenterBaseURL = 'https://hubs.mypapers.top'
            CenterBaseURLs       = @(
                'https://hubs.mypapers.top',
                'https://hubs.maclaw.top',
                'https://hubs2.maclaw.top'
            )
            DatabaseDSN = './data/maclaw-hub-mypapers.db'
        },
        @{
            FileName             = 'hub-maclaw.yaml'
            PublicBaseURL        = 'https://hub.maclaw.top'
            PrimaryCenterBaseURL = 'https://hubs.maclaw.top'
            CenterBaseURLs       = @(
                'https://hubs.mypapers.top',
                'https://hubs.maclaw.top',
                'https://hubs2.maclaw.top'
            )
            DatabaseDSN = './data/maclaw-hub-maclaw.db'
        },
        @{
            FileName             = 'hub2-maclaw.yaml'
            PublicBaseURL        = 'https://hub2.maclaw.top'
            PrimaryCenterBaseURL = 'https://hubs2.maclaw.top'
            CenterBaseURLs       = @(
                'https://hubs.mypapers.top',
                'https://hubs.maclaw.top',
                'https://hubs2.maclaw.top'
            )
            DatabaseDSN = './data/maclaw-hub2-maclaw.db'
        }
    )
}
