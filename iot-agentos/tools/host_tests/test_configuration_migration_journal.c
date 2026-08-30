#include <stdio.h>
#include <string.h>
#include "configuration_migration_journal.h"
#define CHECK(x) do { if (!(x)) { fprintf(stderr,"failed: %s\n",#x); return 1; } } while (0)
int main(void) {
    configuration_migration_journal_t j;
    bool discard = false;
    CHECK(configuration_migration_journal_begin(&j, 128u, 7u, 1u));
    CHECK(configuration_migration_journal_validate(&j));
    CHECK(!configuration_migration_journal_transition(
        &j, CONFIGURATION_MIGRATION_STAGE_COMMITTED));
    CHECK(!configuration_migration_journal_transition(
        &j, CONFIGURATION_MIGRATION_STAGE_NONE));
    CHECK(configuration_migration_journal_transition(&j, CONFIGURATION_MIGRATION_STAGE_VALIDATED));
    CHECK(configuration_migration_journal_transition(&j, CONFIGURATION_MIGRATION_STAGE_COMMITTED));
    CHECK(!configuration_migration_journal_recovery_is_safe(&j));

    /* VALIDATED is a real crash window: the marker may be durable while the
     * target publication is either still absent or already visible.  The
     * value contract itself remains recoverable in both cases; composition
     * root binds the marker to the observed target before clearing it. */
    CHECK(configuration_migration_journal_begin(&j, 96u, 7u, 4u));
    CHECK(configuration_migration_journal_transition(
        &j, CONFIGURATION_MIGRATION_STAGE_VALIDATED));
    CHECK(configuration_migration_journal_recover(&j, &discard));
    CHECK(discard);
    CHECK(configuration_migration_journal_begin(&j, 128u, 7u, 2u));
    /* PREPARED is also a valid publication-window marker.  The composition
     * root may consume it only after proving the V7 target/revision; the
     * value contract itself must remain recoverable and integrity checked. */
    CHECK(configuration_migration_journal_recover(&j, &discard));
    CHECK(discard);
    unsigned char encoded[sizeof(j)]; configuration_migration_journal_t decoded;
    CHECK(configuration_migration_journal_encode(&j, encoded));
    CHECK(configuration_migration_journal_decode(encoded, sizeof(encoded), &decoded));
    CHECK(decoded.generation == 2u && decoded.stage == CONFIGURATION_MIGRATION_STAGE_PREPARED);
    encoded[0] ^= 0x01u;
    CHECK(!configuration_migration_journal_decode(encoded, sizeof(encoded), &decoded));
    CHECK(configuration_migration_journal_recover(&j, &discard));
    CHECK(discard);
    CHECK(configuration_migration_journal_transition(&j, CONFIGURATION_MIGRATION_STAGE_VALIDATED));
    CHECK(configuration_migration_journal_transition(&j, CONFIGURATION_MIGRATION_STAGE_COMMITTED));
    CHECK(!configuration_migration_journal_transition(&j, CONFIGURATION_MIGRATION_STAGE_COMMITTED));
    CHECK(!configuration_migration_journal_set_generation(&j, 11u));
    j.generation = 0;
    CHECK(!configuration_migration_journal_validate(&j));
    CHECK(!configuration_migration_journal_recovery_is_safe(&j));

    /* Recovery must bind a valid journal to the exact source blob and target
     * schema observed during this boot; otherwise stale evidence could be
     * cleared in front of an unrelated configuration record. */
    CHECK(configuration_migration_journal_begin(&j, 128u, 7u, 3u));
    CHECK(configuration_migration_journal_recover(&j, &discard));
    CHECK(discard);
    CHECK(j.source_bytes == 128u && j.target_version == 7u);
    CHECK(configuration_migration_journal_set_generation(&j, 9u));
    CHECK(j.generation == 9u && configuration_migration_journal_validate(&j));
    j.generation = 10u;
    CHECK(!configuration_migration_journal_validate(&j));
    /* Encoding must reject malformed input instead of repairing its checksum
     * and persisting an invalid recovery record. */
    CHECK(!configuration_migration_journal_encode(&j, encoded));

    /* Publication identity is immutable after VALIDATED: retargeting a
     * durable marker must never manufacture evidence for another revision. */
    CHECK(configuration_migration_journal_begin(&j, 128u, 7u, 20u));
    CHECK(configuration_migration_journal_transition(
        &j, CONFIGURATION_MIGRATION_STAGE_VALIDATED));
    CHECK(!configuration_migration_journal_set_generation(&j, 21u));
    CHECK(j.generation == 20u && configuration_migration_journal_validate(&j));

    /* Scalar-key migrations use an explicit sentinel source identity rather
     * than pretending that a single legacy blob length exists. */
    CHECK(configuration_migration_journal_begin(
        &j, CONFIGURATION_MIGRATION_LEGACY_SCALAR_SOURCE_BYTES, 7u, 12u));
    CHECK(configuration_migration_journal_validate(&j));
    CHECK(configuration_migration_journal_transition(
        &j, CONFIGURATION_MIGRATION_STAGE_VALIDATED));
    CHECK(!configuration_migration_journal_set_generation(&j, 13u));
    CHECK(j.generation == 12u && configuration_migration_journal_validate(&j));
    return 0;
}
