#pragma once

/*
 * Private physical SoftAP network owner for Provisioning Service.
 *
 * This is deliberately not a Device/Platform API: it owns one ESP-NETIF
 * handle and the AP-side DHCP/DNS advertisement only.  Wi-Fi driver mode,
 * station credentials and the ESP-IDF default event loop remain composition
 * root responsibilities until their full restart contract is specified.
 */

#include <stdbool.h>

#include "device_api.h"

device_status_t provisioning_network_owner_ensure_setup_ap(void);
bool provisioning_network_owner_setup_ap_ready(void);
device_status_t provisioning_network_owner_configure_setup_ap_dhcp(void);
device_status_t provisioning_network_owner_verify_setup_ap_isolation(void);
/* The STA ESP-NETIF is likewise a private physical resource.  The root still
 * owns Wi-Fi driver mode, credentials, events, and the encompassing stop
 * transaction; callers receive only availability, never the netif handle. */
device_status_t provisioning_network_owner_ensure_station(void);
bool provisioning_network_owner_station_ready(void);
/* Exposes only the fact that either private ESP-NETIF handle is retained; the
 * physical root uses it to reject an unsafe second network generation. */
bool provisioning_network_owner_has_resources(void);
/* Called only by the physical network-root teardown after the Wi-Fi driver
 * has stopped and application callbacks/portal admission are closed. ESP-IDF
 * exposes default-Wi-Fi-netif destruction as a void API. */
void provisioning_network_owner_release_setup_ap(void);
void provisioning_network_owner_release_station(void);
