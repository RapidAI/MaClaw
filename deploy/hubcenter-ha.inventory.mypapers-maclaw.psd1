@{
    ClusterSecret = 'replace-with-a-long-random-shared-secret'

    HubCenters = @(
        @{
            NodeID        = 'hc-1'
            NodeName      = 'hubcenter-1'
            PublicBaseURL = 'https://hubs.mypapers.top'
            AdvertiseURL  = 'https://hubs.mypapers.top'
            DatabaseDSN   = './data/hubcenter-hc-1.db'
        },
        @{
            NodeID        = 'hc-2'
            NodeName      = 'hubcenter-2'
            PublicBaseURL = 'https://hubs.maclaw.top'
            AdvertiseURL  = 'https://hubs.maclaw.top'
            DatabaseDSN   = './data/hubcenter-hc-2.db'
        },
        @{
            NodeID        = 'hc-3'
            NodeName      = 'hubcenter-3'
            PublicBaseURL = 'https://hubs2.maclaw.top'
            AdvertiseURL  = 'https://hubs2.maclaw.top'
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
        }
    )
}
