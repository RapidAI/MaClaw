#include "services/provisioning_network_owner.h"

#include "esp_err.h"
#include "esp_log.h"
#include "esp_netif.h"
#include "esp_wifi_default.h"

#include "lwip/inet.h"

static const char *TAG = "maclaw_client";

#define SETUP_AP_IP_ADDR "192.168.4.1"
#define DHCPS_OFFER_DNS 0x02

static esp_netif_t *s_setup_ap_netif;
static esp_netif_t *s_station_netif;

static device_status_t status_from_esp_err(esp_err_t err) {
    switch (err) {
        case ESP_OK: return DEVICE_STATUS_OK;
        case ESP_ERR_INVALID_ARG: return DEVICE_STATUS_INVALID_ARGUMENT;
        case ESP_ERR_INVALID_STATE: return DEVICE_STATUS_BUSY;
        case ESP_ERR_TIMEOUT: return DEVICE_STATUS_TIMEOUT;
        case ESP_ERR_NO_MEM: return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        case ESP_ERR_NOT_FOUND: return DEVICE_STATUS_NOT_FOUND;
        default: return DEVICE_STATUS_INTERNAL_ERROR;
    }
}

device_status_t provisioning_network_owner_ensure_setup_ap(void) {
    if (s_setup_ap_netif) return DEVICE_STATUS_OK;
    s_setup_ap_netif = esp_netif_create_default_wifi_ap();
    return s_setup_ap_netif ? DEVICE_STATUS_OK : DEVICE_STATUS_RESOURCE_EXHAUSTED;
}

device_status_t provisioning_network_owner_ensure_station(void) {
    if (s_station_netif) return DEVICE_STATUS_OK;
    s_station_netif = esp_netif_create_default_wifi_sta();
    return s_station_netif ? DEVICE_STATUS_OK : DEVICE_STATUS_RESOURCE_EXHAUSTED;
}

bool provisioning_network_owner_station_ready(void) {
    return s_station_netif != NULL;
}

bool provisioning_network_owner_setup_ap_ready(void) {
    return s_setup_ap_netif != NULL;
}

bool provisioning_network_owner_has_resources(void) {
    return s_setup_ap_netif != NULL || s_station_netif != NULL;
}

device_status_t provisioning_network_owner_configure_setup_ap_dhcp(void) {
    if (!s_setup_ap_netif) return DEVICE_STATUS_UNAVAILABLE;
    esp_netif_ip_info_t ip_info = {0};
    IP4_ADDR(&ip_info.ip, 192, 168, 4, 1);
    IP4_ADDR(&ip_info.gw, 192, 168, 4, 1);
    IP4_ADDR(&ip_info.netmask, 255, 255, 255, 0);
    esp_err_t stop_err = esp_netif_dhcps_stop(s_setup_ap_netif);
    if (stop_err != ESP_OK && stop_err != ESP_ERR_ESP_NETIF_DHCP_ALREADY_STOPPED) {
        ESP_LOGW(TAG, "cannot pause DHCP server to configure setup IP: %s",
                 esp_err_to_name(stop_err));
        return status_from_esp_err(stop_err);
    }
    esp_err_t ip_err = esp_netif_set_ip_info(s_setup_ap_netif, &ip_info);
    esp_netif_dns_info_t dns = {0};
    IP4_ADDR(&dns.ip.u_addr.ip4, 192, 168, 4, 1);
    dns.ip.type = ESP_IPADDR_TYPE_V4;
    uint8_t offer_dns = DHCPS_OFFER_DNS;
    esp_err_t dns_offer_err = ip_err == ESP_OK
                                  ? esp_netif_dhcps_option(s_setup_ap_netif,
                                                           ESP_NETIF_OP_SET,
                                                           ESP_NETIF_DOMAIN_NAME_SERVER,
                                                           &offer_dns,
                                                           sizeof(offer_dns))
                                  : ip_err;
    esp_err_t dns_err = dns_offer_err == ESP_OK
                            ? esp_netif_set_dns_info(s_setup_ap_netif,
                                                     ESP_NETIF_DNS_MAIN, &dns)
                            : dns_offer_err;
    esp_err_t start_err = esp_netif_dhcps_start(s_setup_ap_netif);
    if (ip_err != ESP_OK || dns_err != ESP_OK ||
        (start_err != ESP_OK &&
         start_err != ESP_ERR_ESP_NETIF_DHCP_ALREADY_STARTED)) {
        ESP_LOGW(TAG, "cannot configure setup DHCP server: ip=%s dns=%s start=%s",
                 esp_err_to_name(ip_err), esp_err_to_name(dns_err),
                 esp_err_to_name(start_err));
        if (ip_err != ESP_OK) return status_from_esp_err(ip_err);
        if (dns_err != ESP_OK) return status_from_esp_err(dns_err);
        return status_from_esp_err(start_err);
    }
    ESP_LOGI(TAG, "setup DHCP advertises gateway/DNS=%s", SETUP_AP_IP_ADDR);
    return DEVICE_STATUS_OK;
}

device_status_t provisioning_network_owner_verify_setup_ap_isolation(void) {
    if (!s_setup_ap_netif) return DEVICE_STATUS_UNAVAILABLE;
#if defined(CONFIG_LWIP_IP_FORWARD) || defined(CONFIG_LWIP_IPV4_NAPT) || \
    defined(CONFIG_LWIP_IPV4_NAPT_PORTMAP)
    ESP_LOGE(TAG, "setup AP isolation unavailable: IP forwarding/NAPT is enabled");
    return DEVICE_STATUS_UNAVAILABLE;
#else
    esp_err_t napt_err = esp_netif_napt_disable(s_setup_ap_netif);
    if (napt_err != ESP_OK && napt_err != ESP_ERR_NOT_SUPPORTED) {
        ESP_LOGE(TAG, "cannot disable setup AP NAPT: %s", esp_err_to_name(napt_err));
        return status_from_esp_err(napt_err);
    }
    ESP_LOGI(TAG, "setup AP client isolation verified: no forwarding/NAPT");
    return DEVICE_STATUS_OK;
#endif
}

void provisioning_network_owner_release_setup_ap(void) {
    if (!s_setup_ap_netif) return;
    esp_netif_destroy_default_wifi(s_setup_ap_netif);
    s_setup_ap_netif = NULL;
}

void provisioning_network_owner_release_station(void) {
    if (!s_station_netif) return;
    esp_netif_destroy_default_wifi(s_station_netif);
    s_station_netif = NULL;
}
