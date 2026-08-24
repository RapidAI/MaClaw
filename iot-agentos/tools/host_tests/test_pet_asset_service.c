/* Host test for Pet Asset's value-only A9 contract. It compiles the shipped
 * implementation and the production cJSON parser; no HTTP, filesystem,
 * allocator, renderer, RTOS, or display implementation is linked. */

#include <stdint.h>
#include <stdio.h>
#include <string.h>

#include "cJSON.h"
#include "services/pet_asset_service.h"

static int fail(const char *message) {
    fprintf(stderr, "FAIL: %s\n", message);
    return 1;
}

static void make_descriptor(pet_asset_descriptor_t *descriptor) {
    static const char hash[] =
        "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";
    memset(descriptor, 0, sizeof(*descriptor));
    snprintf(descriptor->encoding, sizeof(descriptor->encoding), "rgb565a8");
    snprintf(descriptor->revision, sizeof(descriptor->revision), "pet-r1");
    descriptor->width = 256;
    descriptor->height = 256;
    descriptor->frame_ms = 450;
    descriptor->frame_count = 8;
    for (int i = 0; i < descriptor->frame_count; ++i) {
        snprintf(descriptor->sha256[i], sizeof(descriptor->sha256[i]), "%s", hash);
    }
}

static bool parse_descriptor_json(const char *json, pet_asset_descriptor_t *out) {
    cJSON *root = cJSON_Parse(json);
    if (!root) return false;
    const bool parsed = pet_asset_service_parse_hub_descriptor(root, out);
    cJSON_Delete(root);
    return parsed;
}

static int test_hub_descriptor_parser(void) {
    static const char digest[] =
        "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";
    pet_asset_descriptor_t parsed;
    char json[2048];

    const int valid_length = snprintf(
        json, sizeof(json),
        "{\"encoding\":\"rgb565a8\",\"revision\":\"pet-r2\","
        "\"width\":240,\"height\":240,\"frameMs\":300,"
        "\"urls\":[\"/api/im-gateway/v1/media/pet-a\","
        "\"/api/im-gateway/v1/media/pet-b\"],\"sha256\":[\"%s\",\"%s\"]}",
        digest, digest);
    if (valid_length < 0 || (size_t)valid_length >= sizeof(json) ||
        !parse_descriptor_json(json, &parsed) ||
        strcmp(parsed.encoding, "rgb565a8") || strcmp(parsed.revision, "pet-r2") ||
        parsed.width != 240 || parsed.height != 240 || parsed.frame_ms != 300 ||
        parsed.frame_count != 2 ||
        strcmp(parsed.urls[1], "/api/im-gateway/v1/media/pet-b")) {
        return fail("valid Hub descriptor rejected");
    }

    const int default_length = snprintf(
        json, sizeof(json),
        "{\"encoding\":\"rgb565a8\",\"revision\":\"pet-default\","
        "\"width\":32,\"height\":32,\"urls\":[\"/api/im-gateway/v1/media/pet\"],"
        "\"sha256\":[\"%s\"]}", digest);
    if (default_length < 0 || (size_t)default_length >= sizeof(json) ||
        !parse_descriptor_json(json, &parsed) ||
        parsed.frame_ms != PET_ASSET_SERVICE_DEFAULT_FRAME_MS) {
        return fail("missing frameMs did not default");
    }

    static const char *const rejected[] = {
        "[]",
        "{\"encoding\":\"rgb565\",\"revision\":\"r\",\"width\":32,\"height\":32,\"urls\":[],\"sha256\":[]}",
        "{\"encoding\":\"rgb565a8\",\"revision\":\"r\",\"width\":31,\"height\":32,\"urls\":[\"/api/im-gateway/v1/media/p\"],\"sha256\":[\"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\"]}",
        "{\"encoding\":\"rgb565a8\",\"revision\":\"r\",\"width\":256.5,\"height\":32,\"urls\":[\"/api/im-gateway/v1/media/p\"],\"sha256\":[\"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\"]}",
        "{\"encoding\":\"rgb565a8\",\"revision\":\"r\",\"width\":32,\"height\":32,\"urls\":[\"https://example.com/p\"],\"sha256\":[\"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\"]}",
        "{\"encoding\":\"rgb565a8\",\"revision\":\"r\",\"width\":32,\"height\":32,\"urls\":[\"/api/im-gateway/v1/media/p\"],\"sha256\":[\"not-a-sha\"]}",
        "{\"encoding\":\"rgb565a8\",\"revision\":\"r\",\"width\":32,\"height\":32,\"urls\":[\"/api/im-gateway/v1/media/p\"],\"sha256\":[]}",
        "{\"encoding\":\"rgb565a8\",\"revision\":\"r\",\"width\":32,\"height\":32,\"frameMs\":49,\"urls\":[\"/api/im-gateway/v1/media/p\"],\"sha256\":[\"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\"]}",
        "{\"encoding\":\"rgb565a8\",\"revision\":\"r\",\"width\":32,\"height\":32,\"frameMs\":10000.5,\"urls\":[\"/api/im-gateway/v1/media/p\"],\"sha256\":[\"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\"]}",
        "{\"encoding\":\"rgb565a8\",\"revision\":\"r\",\"width\":32,\"height\":32,\"frameMs\":\"450\",\"urls\":[\"/api/im-gateway/v1/media/p\"],\"sha256\":[\"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\"]}",
    };
    for (size_t i = 0; i < sizeof(rejected) / sizeof(rejected[0]); ++i) {
        if (parse_descriptor_json(rejected[i], &parsed)) {
            return fail("invalid Hub descriptor accepted");
        }
    }
    return 0;
}

int main(void) {
    pet_asset_descriptor_t descriptor;
    make_descriptor(&descriptor);
    if (test_hub_descriptor_parser() != 0) return 1;

    size_t bytes = 0;
    if (!pet_asset_service_frame_bytes(256, 256, &bytes) || bytes != 196608u) {
        return fail("RGB565A8 frame geometry");
    }
    if (pet_asset_service_frame_bytes(31, 32, &bytes) ||
        pet_asset_service_frame_bytes(257, 256, &bytes)) {
        return fail("invalid geometry accepted");
    }

    pet_asset_memory_requirements_t requirements = {0};
    if (!pet_asset_service_calculate_memory_requirements(
            &descriptor, 200000u, 100000u, &requirements) ||
        requirements.source_bytes != 1572864u ||
        requirements.total_external_bytes != 1772864u ||
        requirements.max_external_allocation_bytes != 196608u) {
        return fail("memory requirements rewritten");
    }
    if (pet_asset_service_calculate_memory_requirements(
            &descriptor, UINT32_MAX, 0, &requirements)) {
        return fail("memory requirement overflow accepted");
    }

    uint32_t next_count = 0;
    uint32_t next_ms = 0;
    if (!pet_asset_service_next_memory_fallback(
            &descriptor, 8, 8, &next_count, &next_ms) ||
        next_count != 4 || next_ms != 900u) {
        return fail("8-to-4 fallback");
    }
    if (!pet_asset_service_next_memory_fallback(
            &descriptor, 4, 4, &next_count, &next_ms) ||
        next_count != 2 || next_ms != 1800u) {
        return fail("4-to-2 fallback");
    }
    if (!pet_asset_service_next_memory_fallback(
            &descriptor, 2, 1, &next_count, &next_ms) ||
        next_count != 1 || next_ms != 3600u) {
        return fail("2-to-1 fallback");
    }
    if (pet_asset_service_next_memory_fallback(
            &descriptor, 1, 1, &next_count, &next_ms)) {
        return fail("fallback below one frame accepted");
    }

    char metadata[PET_ASSET_SERVICE_CACHE_METADATA_CAPACITY] = {0};
    size_t metadata_length = 0;
    if (!pet_asset_service_format_cache_metadata(
            &descriptor, metadata, sizeof(metadata), &metadata_length) ||
        metadata_length == 0) {
        return fail("cache metadata format");
    }
    pet_asset_descriptor_t parsed;
    if (!pet_asset_service_parse_cache_metadata(metadata, metadata_length, &parsed) ||
        strcmp(parsed.revision, descriptor.revision) || parsed.width != 256 ||
        parsed.height != 256 || parsed.frame_ms != 450 || parsed.frame_count != 8 ||
        parsed.urls[0][0] != '\0') {
        return fail("cache metadata round trip");
    }
    metadata[metadata_length - 1] = 'x';
    if (pet_asset_service_parse_cache_metadata(metadata, metadata_length, &parsed)) {
        return fail("corrupt metadata accepted");
    }
    if (!pet_asset_service_format_cache_metadata(
            &descriptor, metadata, sizeof(metadata), &metadata_length)) {
        return fail("cache metadata before truncation");
    }
    if (pet_asset_service_parse_cache_metadata(metadata, metadata_length - 1, &parsed)) {
        return fail("truncated metadata accepted");
    }
    if (!pet_asset_service_format_cache_metadata(
            &descriptor, metadata, sizeof(metadata), &metadata_length)) {
        return fail("cache metadata reformat");
    }
    metadata[12] = '\0';
    if (pet_asset_service_parse_cache_metadata(metadata, metadata_length, &parsed)) {
        return fail("NUL metadata accepted");
    }
    if (!pet_asset_service_format_cache_metadata(
            &descriptor, metadata, sizeof(metadata), &metadata_length)) {
        return fail("cache metadata second reformat");
    }
    if (metadata_length + 1 >= sizeof(metadata)) return fail("metadata test buffer");
    metadata[metadata_length++] = '\n';
    if (pet_asset_service_parse_cache_metadata(metadata, metadata_length, &parsed)) {
        return fail("metadata with empty extra line accepted");
    }
    if (!pet_asset_service_format_cache_metadata(
            &descriptor, metadata, sizeof(metadata), &metadata_length)) {
        return fail("cache metadata third reformat");
    }
    static const char extra[] = "extra\n";
    if (metadata_length + sizeof(extra) - 1 >= sizeof(metadata)) {
        return fail("metadata extra token buffer");
    }
    memcpy(metadata + metadata_length, extra, sizeof(extra) - 1);
    metadata_length += sizeof(extra) - 1;
    if (pet_asset_service_parse_cache_metadata(metadata, metadata_length, &parsed)) {
        return fail("metadata with extra token accepted");
    }

    uint8_t digest[32];
    char digest_hex[65];
    for (size_t i = 0; i < sizeof(digest); ++i) {
        digest[i] = (uint8_t)i;
        snprintf(digest_hex + i * 2, sizeof(digest_hex) - i * 2, "%02X", digest[i]);
    }
    if (!pet_asset_service_sha256_matches_hex(digest, digest_hex)) {
        return fail("uppercase digest rejected");
    }
    digest_hex[0] = 'f';
    if (pet_asset_service_sha256_matches_hex(digest, digest_hex)) {
        return fail("wrong digest accepted");
    }

    printf("PASS pet_asset value contract\n");
    return 0;
}
