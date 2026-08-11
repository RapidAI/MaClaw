cmake_minimum_required(VERSION 3.16)
include($ENV{IDF_PATH}/tools/cmake/project.cmake)
# Component Manager's legacy project flow reads the lock *build property*, not
# the similarly named CMake cache variable.  The profile wrapper supplies this
# identity at configure time, so keep one independently reproducible resolved
# closure per board instead of allowing the last local profile build to rewrite
# a shared dependencies.lock.
if(DEFINED MACLAW_PROFILE AND NOT MACLAW_PROFILE STREQUAL "")
    idf_build_set_property(DEPENDENCIES_LOCK
                           "${CMAKE_CURRENT_SOURCE_DIR}/dependencies.lock.${MACLAW_PROFILE}")
endif()
project(maclaw_esp32s3_client)
# The storage partition is reserved for future pet assets. Keep normal builds
# independent of an optional source directory; when assets are present they
# are packed and included in `idf.py flash` automatically.
if(EXISTS "${CMAKE_CURRENT_SOURCE_DIR}/data")
    spiffs_create_partition_image(storage data FLASH_IN_PROJECT)
endif()
