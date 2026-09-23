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
# Profile-only managed dependencies live in source-free carrier components.
# ESP-IDF discovers these before it can read Kconfig, so the selected build
# entry point must pass their directory through EXTRA_COMPONENT_DIRS.
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
