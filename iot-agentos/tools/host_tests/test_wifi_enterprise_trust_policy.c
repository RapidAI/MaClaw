#include <stdio.h>
#include "wifi_enterprise_trust_policy.h"
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "failed: %s\n", #x); return 1; } } while (0)
int main(void) {
    CHECK(wifi_enterprise_trust_policy_valid_domain("radius.company.com", 128));
    CHECK(wifi_enterprise_trust_policy_valid_domain("RADIUS-1.example.org", 128));
    CHECK(!wifi_enterprise_trust_policy_valid_domain("radius", 128));
    CHECK(!wifi_enterprise_trust_policy_valid_domain(".example.org", 128));
    CHECK(!wifi_enterprise_trust_policy_valid_domain("radius..example.org", 128));
    CHECK(!wifi_enterprise_trust_policy_valid_domain("radius.example.org.", 128));
    CHECK(!wifi_enterprise_trust_policy_valid_domain("radius.example.org:443", 128));
    CHECK(!wifi_enterprise_trust_policy_valid_domain("radius\\example.org", 128));
    puts("PASS enterprise EAP domain trust policy");
    return 0;
}
