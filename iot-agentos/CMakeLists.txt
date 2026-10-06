cmake_minimum_required(VERSION 3.16)
include($ENV{IDF_PATH}/tools/cmake/project.cmake)
# Component Manager's legacy project flow reads the lock *build property*, not
# the similarly named CMake cache variable.  The profile wrapper supplies this
# identity at configure time, so keep one independently reproducible resolved
# closure per board instead of allowing the last local profile build to rewrite
# a shared dependencies.lock.
# ESP-IDF also runs a preliminary `cmake -P` requirements pass where command
# line cache entries are not guaranteed to survive.  The wrapper exports the
# same selected identity in the environment; recover it before setting the
# build property so that pass cannot fall back to the generic lock and prune a
# different profile's managed components (notably Waveshare touch support).
if((NOT DEFINED MACLAW_PROFILE OR MACLAW_PROFILE STREQUAL "") AND
   DEFINED ENV{MACLAW_PROFILE} AND NOT "$ENV{MACLAW_PROFILE}" STREQUAL "")
    set(MACLAW_PROFILE "$ENV{MACLAW_PROFILE}")
endif()
if(DEFINED MACLAW_PROFILE AND NOT MACLAW_PROFILE STREQUAL "")
    idf_build_set_property(DEPENDENCIES_LOCK
                           "${CMAKE_CURRENT_SOURCE_DIR}/dependencies.lock.${MACLAW_PROFILE}")
endif()
# Profile-only managed dependencies live in source-free carrier components under
# profile_components/<name>_deps (e.g. echoear_deps for the echoear-2st board).
# ESP-IDF's Component Manager must see these manifests BEFORE Kconfig exists, so
# the selected profile's carrier directory is registered here via
# EXTRA_COMPONENT_DIRS. This was previously only described in a comment and never
# wired, which made the manager resolve a manifest set that omitted the profile's
# own components -- for EchoEar that dropped esp_lcd_st77916 (and its transitive
# cmake_utilities), producing an incomplete dependencies.lock.<profile> and,
# on the next build, a re-solve that tried to delete those managed components.
set(_maclaw_carrier_dir "")
if(MACLAW_PROFILE STREQUAL "echoear-2st")
    set(_maclaw_carrier_dir "echoear_deps")
elseif(MACLAW_PROFILE STREQUAL "fangtang-4g")
    set(_maclaw_carrier_dir "fangtang_deps")
elseif(MACLAW_PROFILE STREQUAL "waveshare-amoled-1.75c")
    set(_maclaw_carrier_dir "waveshare_deps")
endif()
if(_maclaw_carrier_dir AND EXISTS "${CMAKE_CURRENT_SOURCE_DIR}/profile_components/${_maclaw_carrier_dir}")
    list(APPEND EXTRA_COMPONENT_DIRS "${CMAKE_CURRENT_SOURCE_DIR}/profile_components/${_maclaw_carrier_dir}")
endif()
project(maclaw_esp32s3_client)
# The sdkconfig.defaults in this project carries ESP-IDF 6.0.2-specific S3
# stability workarounds (CONFIG_ESP_COEX_ENABLED=n, the two
# CONFIG_MBEDTLS_*_USE_INTERRUPT=n options, CONFIG_ESP_TLS_USE_DS_PERIPHERAL=n
# and the Wi-Fi AMPDU disable).  They have only been validated on IDF 6.0.x;
# warn on any other version so the settings get re-verified before they are
# trusted there.  IDF_VERSION_* only exists once project() has run.
if((NOT IDF_VERSION_MAJOR EQUAL 6) OR (NOT IDF_VERSION_MINOR EQUAL 0))
    message(WARNING "iot-agentos: ESP-IDF ${IDF_VERSION_MAJOR}.${IDF_VERSION_MINOR}.${IDF_VERSION_PATCH} is not 6.0.x; "
                    "the sdkconfig.defaults workarounds (CONFIG_ESP_COEX_ENABLED=n, "
                    "CONFIG_MBEDTLS_MPI_USE_INTERRUPT=n, CONFIG_MBEDTLS_AES_USE_INTERRUPT=n, "
                    "CONFIG_ESP_TLS_USE_DS_PERIPHERAL=n, Wi-Fi AMPDU disabled) "
                    "have not been verified on this version.")
endif()
# The storage partition is reserved for future pet assets. Keep normal builds
# independent of an optional source directory; when assets are present they
# are packed and included in `idf.py flash` automatically.
if(EXISTS "${CMAKE_CURRENT_SOURCE_DIR}/data")
    spiffs_create_partition_image(storage data FLASH_IN_PROJECT)
endif()
