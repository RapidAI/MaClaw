cmake_minimum_required(VERSION 3.14)

project(RapidSpeech.cpp VERSION 0.1.0 LANGUAGES C CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

message(STATUS "CMAKE_INSTALL_PREFIX: ${CMAKE_INSTALL_PREFIX}")

# --- Compilation Options ---
option(RS_BUILD_TESTS "Build test executables" ON)
option(RS_BUILD_SERVER "Build REST API server" OFF)
option(RS_CUDA "Enable CUDA acceleration" OFF)
option(RS_VULKAN "Enable Vulkan acceleration" OFF)
option(RS_CANN "Enable CANN acceleration (Huawei)" OFF)
option(RS_OPENCL "Enable OpenCL acceleration" OFF)
option(RS_ENABLE_PYTHON "Enable Python bindings via pybind11" OFF)

list(APPEND CMAKE_MODULE_PATH ${CMAKE_CURRENT_SOURCE_DIR}/cmake)

if (RS_ENABLE_PYTHON)
    include(pybind11)
endif()

# Auto-detect Apple platform to set default value for Metal
if (APPLE)
    set(RS_METAL_DEFAULT ON)
else()
    set(RS_METAL_DEFAULT OFF)
endif()
option(RS_METAL "Enable Metal acceleration (Apple only)" ${RS_METAL_DEFAULT})

# --- Dependencies Configuration ---
set(GGML_PATH "${CMAKE_CURRENT_SOURCE_DIR}/ggml")

if (NOT EXISTS "${GGML_PATH}/CMakeLists.txt")
    message(FATAL_ERROR "ggml not found at ${GGML_PATH}. Please init submodules.")
endif()

# Note: Pass options to ggml before adding the directory
if (RS_CUDA)
    set(GGML_CUDA ON CACHE BOOL "ggml: enable CUDA" FORCE)
endif()

if (RS_METAL AND APPLE)
    set(GGML_METAL ON CACHE BOOL "ggml: enable Metal" FORCE)
endif()

if (RS_VULKAN)
    set(GGML_VULKAN ON CACHE BOOL "ggml: enable Vulkan" FORCE)
endif()

if (RS_CANN)
    set(GGML_CANN ON CACHE BOOL "ggml: enable CANN" FORCE)
endif()

if (RS_OPENCL)
    set(GGML_OPENCL ON CACHE BOOL "ggml: enable OpenCL" FORCE)
endif()

add_subdirectory(${GGML_PATH} ggml)

# Need GNUInstallDirs early for generator expressions in target_include_directories
include(GNUInstallDirs)

# --- Core Source Files ---
set(RS_SOURCES
        rapidspeech/src/core/rs_context.cpp
        rapidspeech/src/core/rs_processor.cpp
        rapidspeech/src/frontend/audio_processor.cpp
        rapidspeech/src/frontend/fftsg.c
        rapidspeech/src/utils/rs_log.cpp
        rapidspeech/src/arch/sensevoice.cpp
        rapidspeech/src/arch/sensevoice_encoder.cpp
        rapidspeech/src/arch/funasr-nano.cpp
        rapidspeech/src/arch/ecapa_tdnn.cpp
        rapidspeech/src/utils/rs_wav.cpp
)

# --- Library Compilation ---
add_library(rapidspeech-core SHARED
        ${RS_SOURCES}
        rapidspeech/src/c_api/rapidspeech_c.cpp
)

target_include_directories(rapidspeech-core
        PUBLIC  $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
                $<INSTALL_INTERFACE:${CMAKE_INSTALL_INCLUDEDIR}>
        PRIVATE ${CMAKE_CURRENT_SOURCE_DIR}/rapidspeech/src
)
target_compile_definitions(rapidspeech-core PRIVATE RAPIDSPEECH_BUILD)
target_link_libraries(rapidspeech-core PRIVATE ggml)

# Set shared library version
set_target_properties(rapidspeech-core PROPERTIES
        VERSION ${PROJECT_VERSION}
        SOVERSION ${PROJECT_VERSION_MAJOR}
        OUTPUT_NAME rapidspeech
        # Windows: export all or rely on RS_API macros
        WINDOWS_EXPORT_ALL_SYMBOLS OFF
        # Visibility
        CXX_VISIBILITY_PRESET hidden
        C_VISIBILITY_PRESET hidden
        VISIBILITY_INLINES_HIDDEN ON
)

# --- Hardware Acceleration Logic ---

# 1. CUDA Configuration
if (RS_CUDA)
    message(STATUS "RapidSpeech: CUDA acceleration enabled.")
    target_compile_definitions(rapidspeech-core PRIVATE RS_USE_CUDA)
    if (TARGET ggml-cuda)
        target_link_libraries(rapidspeech-core PRIVATE ggml-cuda)
    endif()
endif()

# 2. Metal Configuration (Apple Platforms)
if (RS_METAL AND APPLE)
    message(STATUS "RapidSpeech: Metal acceleration enabled.")
    target_compile_definitions(rapidspeech-core PRIVATE RS_USE_METAL)
    if (TARGET ggml-metal)
        target_link_libraries(rapidspeech-core PRIVATE ggml-metal)
    endif()
    target_link_libraries(rapidspeech-core PRIVATE "-framework Foundation" "-framework Metal" "-framework MetalKit")
endif()

# 3. Vulkan Configuration
if (RS_VULKAN)
    message(STATUS "RapidSpeech: Vulkan acceleration enabled.")
    target_compile_definitions(rapidspeech-core PRIVATE RS_USE_VULKAN)
    if (TARGET ggml-vulkan)
        target_link_libraries(rapidspeech-core PRIVATE ggml-vulkan)
    endif()
endif()

# 4. CANN Configuration (Huawei Ascend)
if (RS_CANN)
    message(STATUS "RapidSpeech: CANN acceleration enabled.")
    target_compile_definitions(rapidspeech-core PRIVATE RS_USE_CANN)
    if (TARGET ggml-cann)
        target_link_libraries(rapidspeech-core PRIVATE ggml-cann)
    endif()
endif()

# 5. OpenCL Configuration
if (RS_OPENCL)
    message(STATUS "RapidSpeech: OpenCL acceleration enabled.")
    target_compile_definitions(rapidspeech-core PRIVATE RS_USE_OPENCL)
    if (TARGET ggml-opencl)
        target_link_libraries(rapidspeech-core PRIVATE ggml-opencl)
    endif()
endif()

# --- Install rules for shared library ---
include(CMakePackageConfigHelpers)

install(TARGETS rapidspeech-core
        EXPORT rapidspeech-targets
        RUNTIME DESTINATION ${CMAKE_INSTALL_BINDIR}      # .dll on Windows
        LIBRARY DESTINATION ${CMAKE_INSTALL_LIBDIR}      # .so / .dylib
        ARCHIVE DESTINATION ${CMAKE_INSTALL_LIBDIR}      # .lib / .a
)

install(FILES include/rapidspeech.h
        DESTINATION ${CMAKE_INSTALL_INCLUDEDIR}
)

# Generate and install CMake config for find_package(rapidspeech)
install(EXPORT rapidspeech-targets
        FILE rapidspeech-targets.cmake
        NAMESPACE rapidspeech::
        DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/rapidspeech
)

configure_package_config_file(
        ${CMAKE_CURRENT_SOURCE_DIR}/cmake/rapidspeech-config.cmake.in
        ${CMAKE_CURRENT_BINARY_DIR}/rapidspeech-config.cmake
        INSTALL_DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/rapidspeech
)

write_basic_package_version_file(
        ${CMAKE_CURRENT_BINARY_DIR}/rapidspeech-config-version.cmake
        VERSION ${PROJECT_VERSION}
        COMPATIBILITY SameMajorVersion
)

install(FILES
        ${CMAKE_CURRENT_BINARY_DIR}/rapidspeech-config.cmake
        ${CMAKE_CURRENT_BINARY_DIR}/rapidspeech-config-version.cmake
        DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/rapidspeech
)

# --- Python Bindings ---
if (RS_ENABLE_PYTHON)

    pybind11_add_module(rapidspeech
            rapidspeech/python/pybind_rapidspeech.cpp
    )

    install(TARGETS rapidspeech
            LIBRARY DESTINATION lib
    )

    target_link_libraries(rapidspeech
            PRIVATE rapidspeech-core
    )

    target_include_directories(rapidspeech
            PRIVATE
            ${CMAKE_CURRENT_SOURCE_DIR}
    )

    set_target_properties(rapidspeech PROPERTIES
            CXX_VISIBILITY_PRESET hidden
            VISIBILITY_INLINES_HIDDEN ON
    )

endif()

# --- REST API Server ---
if (RS_BUILD_SERVER)
    message(STATUS "RapidSpeech: Building REST API server.")
    add_executable(rs-server server/rs_server.cpp)
    target_include_directories(rs-server PRIVATE
            ${CMAKE_CURRENT_SOURCE_DIR}/server/third_party
            ${CMAKE_CURRENT_SOURCE_DIR}/include
            ${CMAKE_CURRENT_SOURCE_DIR}/rapidspeech/src
    )
    # Disable OpenSSL in cpp-httplib to avoid unnecessary link dependency
    target_compile_definitions(rs-server PRIVATE CPPHTTPLIB_NO_EXCEPTIONS=0)
    target_link_libraries(rs-server PRIVATE rapidspeech-core)
    if (NOT WIN32)
        find_package(Threads REQUIRED)
        target_link_libraries(rs-server PRIVATE Threads::Threads)
    endif()
    install(TARGETS rs-server RUNTIME DESTINATION ${CMAKE_INSTALL_BINDIR})
endif()

# --- Example Executables ---
add_executable(rs-asr-offline examples/rs-asr-offline.cpp)
target_include_directories(rs-asr-offline PRIVATE
        ${CMAKE_CURRENT_SOURCE_DIR}/include
        ${CMAKE_CURRENT_SOURCE_DIR}/rapidspeech/src
)
target_link_libraries(rs-asr-offline PRIVATE rapidspeech-core)
install(TARGETS rs-asr-offline
        RUNTIME DESTINATION ${CMAKE_INSTALL_BINDIR}
)

# --- Tests ---
if (RS_BUILD_TESTS)
    message(STATUS "Tests are enabled.")
    enable_testing()

    set(TEST_MODULES
            audio_frontend
    )

    foreach(module ${TEST_MODULES})
        set(test_target "test_${module}")
        set(test_source "tests/${test_target}.cpp")

        if (EXISTS "${CMAKE_CURRENT_SOURCE_DIR}/${test_source}")
            add_executable(${test_target} ${test_source})
            target_include_directories(${test_target} PRIVATE
                    ${CMAKE_CURRENT_SOURCE_DIR}/include
                    ${CMAKE_CURRENT_SOURCE_DIR}/rapidspeech/src
            )
            target_link_libraries(${test_target} PRIVATE rapidspeech-core ggml)
            add_test(NAME ${test_target} COMMAND ${test_target})
        endif()
    endforeach()
endif()
