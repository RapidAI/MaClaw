/* Fangtang's standby transport identity.
 *
 * This stores only the selected uplink fact used by Fangtang's product
 * identity. The shared compact renderer remains responsible for synchronization
 * and deciding when an idle/quiet scene may be redrawn.
 */
#include "sdkconfig.h"

#if !CONFIG_MACLAW_BOARD_FANGTANG_4G
#error "Fangtang network identity may only be compiled for the Fangtang profile"
#endif

#include <stdbool.h>

static bool s_fangtang_network_transport_cellular;

bool compact_profile_network_transport_is_cellular(void) {
    return s_fangtang_network_transport_cellular;
}

bool compact_profile_publish_network_transport(bool cellular) {
    const bool changed = s_fangtang_network_transport_cellular != cellular;
    s_fangtang_network_transport_cellular = cellular;
    return changed;
}
