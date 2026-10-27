idf_component_register(SRCS "cJSON/cJSON.c"
                            "cJSON/cJSON_Utils.c"
                    INCLUDE_DIRS "cJSON")

# Vendored from espressif/cjson. The original component reads these from its
# Kconfig; as a local component we fix the library defaults instead.
target_compile_definitions(${COMPONENT_LIB} PUBLIC
    CJSON_NESTING_LIMIT=32
    CJSON_CIRCULAR_LIMIT=16)
