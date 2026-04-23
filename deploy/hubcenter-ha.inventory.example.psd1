@{
    ClusterSecret = 'replace-with-a-long-random-shared-secret'

    HubCenters = @(
        @{
            NodeID        = 'hc-1'
            NodeName      = 'hubcenter-1'
            PublicBaseURL = 'https://hc-1.example.com'
            AdvertiseURL  = 'https://hc-1.example.com'
            DatabaseDSN   = './data/hubcenter-hc-1.db'
        },
        @{
            NodeID        = 'hc-2'
            NodeName      = 'hubcenter-2'
            PublicBaseURL = 'https://hc-2.example.com'
            AdvertiseURL  = 'https://hc-2.example.com'
            DatabaseDSN   = './data/hubcenter-hc-2.db'
        },
        @{
            NodeID        = 'hc-3'
            NodeName      = 'hubcenter-3'
            PublicBaseURL = 'https://hc-3.example.com'
            AdvertiseURL  = 'https://hc-3.example.com'
            DatabaseDSN   = './data/hubcenter-hc-3.db'
        }
    )

    Hubs = @(
        @{
            FileName             = 'hub.yaml'
            PublicBaseURL        = 'https://hub.example.com'
            PrimaryCenterBaseURL = 'https://hc-1.example.com'
            CenterBaseURLs       = @(
                'https://hc-1.example.com',
                'https://hc-2.example.com',
                'https://hc-3.example.com'
            )
            DatabaseDSN = './data/maclaw-hub.db'
        }
    )
}