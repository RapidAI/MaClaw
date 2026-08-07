cmake_minimum_required(VERSION 3.16)
include($ENV{IDF_PATH}/tools/cmake/project.cmake)
project(maclaw_esp32s3_client)
# The storage partition is reserved for future pet assets. Keep normal builds
# independent of an optional source directory; when assets are present they
# are packed and included in `idf.py flash` automatically.
if(EXISTS "${CMAKE_CURRENT_SOURCE_DIR}/data")
    spiffs_create_partition_image(storage data FLASH_IN_PROJECT)
endif()
