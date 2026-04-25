@{
    ClusterSecret = 'replace-with-a-long-random-shared-secret'

    HubCenters = @(
        @{
            FQDN          = 'hc-1.example.com'
            NodeID        = 'hc-1'
            NodeName      = 'hubcenter-1'
            PublicBaseURL = 'https://hc-1.example.com'
            AdvertiseURL  = 'https://hc-1.example.com'
            DatabaseDSN   = './data/codeclaw-hubcenter.db'
        },
        @{
            FQDN          = 'hc-2.example.com'
            NodeID        = 'hc-2'
            NodeName      = 'hubcenter-2'
            PublicBaseURL = 'https://hc-2.example.com'
            AdvertiseURL  = 'https://hc-2.example.com'
            DatabaseDSN   = './data/codeclaw-hubcenter.db'
        },
        @{
            FQDN          = 'hc-3.example.com'
            NodeID        = 'hc-3'
            NodeName      = 'hubcenter-3'
            PublicBaseURL = 'https://hc-3.example.com'
            AdvertiseURL  = 'https://hc-3.example.com'
            DatabaseDSN   = './data/codeclaw-hubcenter.db'
        }
    )

    Hubs = @(
        @{
            FileName              = 'hub-1.yaml'
            PublicBaseURL         = 'https://hub-1.example.com'
            PrimaryCenterBaseURL  = 'https://hc-1.example.com'
            CenterBaseURLs        = @(
                'https://hc-1.example.com',
                'https://hc-2.example.com',
                'https://hc-3.example.com'
            )
            DatabaseDSN           = './data/codeclaw-hub.db'
            Visibility            = 'shared'
            CorporateEmailDomain  = 'rapidai.tech'
            CorporateEmailDomains = @('rapidai.tech', 'qianxin.com')
            AcceptPublicSignup    = $false
        },
        @{
            FileName              = 'hub-2.yaml'
            PublicBaseURL         = 'https://hub-2.example.com'
            PrimaryCenterBaseURL  = 'https://hc-2.example.com'
            CenterBaseURLs        = @(
                'https://hc-1.example.com',
                'https://hc-2.example.com',
                'https://hc-3.example.com'
            )
            DatabaseDSN           = './data/codeclaw-hub.db'
            Visibility            = 'shared'
            CorporateEmailDomain  = ''
            CorporateEmailDomains = @()
            AcceptPublicSignup    = $true
        },
        @{
            FileName              = 'hub-3.yaml'
            PublicBaseURL         = 'https://hub-3.example.com'
            PrimaryCenterBaseURL  = 'https://hc-3.example.com'
            CenterBaseURLs        = @(
                'https://hc-1.example.com',
                'https://hc-2.example.com',
                'https://hc-3.example.com'
            )
            DatabaseDSN           = './data/codeclaw-hub.db'
            Visibility            = 'shared'
            CorporateEmailDomain  = ''
            CorporateEmailDomains = @()
            AcceptPublicSignup    = $true
        }
    )
}
