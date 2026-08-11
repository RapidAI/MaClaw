/* Physical acoustic calibration contract for compact direct-I2S profiles.
 *
 * The shared capture and wake state machines consume normalized values only;
 * selected board adapters own the microphone-dependent calibration.
 */
#pragma once

typedef struct {
    unsigned sample_rate;
    unsigned command_silence_ms;
    unsigned command_start_confirm_ms;
    unsigned command_start_level;
    unsigned command_silence_floor;
    unsigned command_silence_margin;
    unsigned command_silence_ceiling;
    float wake_word_detection_threshold;
    unsigned wake_word_gain_num;
    unsigned wake_word_gain_den;
    /* Direct-I2S amplification is implemented in the shared PCM mixer, but
     * its boot default remains a profile calibration fact. */
    unsigned output_volume_default;
} compact_audio_calibration_t;
