#pragma once

#include <algorithm>
#include <cmath>
#include <map>
#include <string>
#include <vector>

/**
 * CTC Decoding logic for SenseVoice/FunASR models.
 */
class CTCDecoder {
public:
  struct BeamEntry {
    float p_blank = -INFINITY;     // Probability of path ending in blank
    float p_non_blank = -INFINITY; // Probability of path ending in non-blank

    float score() const {
      if (p_blank == -INFINITY)
        return p_non_blank;
      if (p_non_blank == -INFINITY)
        return p_blank;
      // logsumexp
      return std::max(p_blank, p_non_blank) +
             log1p(exp(-std::abs(p_blank - p_non_blank)));
    }
  };

  /**
   * Greedy Search (Beam size = 1)
   * Performs deduplication and blank (ID=0) removal.
   */
  static std::vector<int32_t> GreedyDecode(const int32_t *argmax_ids, int T) {
    std::vector<int32_t> result;
    int32_t last_id = -1;
    for (int i = 0; i < T; ++i) {
      int32_t id = argmax_ids[i];
      // ID 0 is usually <blank> in FunASR/SenseVoice
      if (id != 0 && id != 60514 && id != last_id) {
        result.push_back(id);
      }
      last_id = id;
    }
    return result;
  }

  /**
   * CTC Prefix Beam Search
   * @param probs Log probabilities [T, V]
   * @param T Number of frames
   * @param V Vocabulary size
   * @param beam_size Number of beams to keep
   */
  static std::vector<int32_t> BeamSearchDecode(const float *log_probs, int T,
                                               int V, int beam_size) {
    if (beam_size <= 1) {
      // Fallback to a manual greedy if called incorrectly
      std::vector<int32_t> result;
      int32_t last_id = -1;
      for (int t = 0; t < T; ++t) {
        int max_id = 0;
        float max_p = -INFINITY;
        for (int v = 0; v < V; ++v) {
          if (log_probs[t * V + v] > max_p) {
            max_p = log_probs[t * V + v];
            max_id = v;
          }
        }
        if (max_id != 0 && max_id != last_id)
          result.push_back(max_id);
        last_id = max_id;
      }
      return result;
    }

    // Map from prefix sequence to probabilities
    std::map<std::vector<int32_t>, BeamEntry> beams;
    std::vector<int32_t> empty_prefix = {};
    beams[empty_prefix].p_blank = 0.0f; // log(1.0)

    for (int t = 0; t < T; ++t) {
      std::map<std::vector<int32_t>, BeamEntry> next_beams;
      const float *current_log_probs = log_probs + (t * V);

      for (auto const &[prefix, entry] : beams) {
        // 1. Terminate with blank
        float p_blank = current_log_probs[0];
        float s = entry.score();
        update_log_prob(next_beams[prefix].p_blank, s + p_blank);

        // 2. Extend with non-blank tokens
        for (int v = 1; v < V; ++v) {
          float p_v = current_log_probs[v];
          std::vector<int32_t> new_prefix = prefix;
          new_prefix.push_back(v);

          if (!prefix.empty() && v == prefix.back()) {
            // Repeating character: "a" + "a"
            // If we stay on "a", it could be from blank transition or same-char
            // transition
            update_log_prob(next_beams[prefix].p_non_blank,
                            entry.p_blank + p_v);
            update_log_prob(next_beams[new_prefix].p_non_blank,
                            entry.p_non_blank + p_v);
          } else {
            update_log_prob(next_beams[new_prefix].p_non_blank, s + p_v);
          }
        }
      }

      // Pruning: keep only top beam_size
      beams = prune_beams(next_beams, beam_size);
    }

    // Return the best prefix
    float best_score = -INFINITY;
    std::vector<int32_t> best_prefix;
    for (auto const &[prefix, entry] : beams) {
      float s = entry.score();
      if (s > best_score) {
        best_score = s;
        best_prefix = prefix;
      }
    }
    return best_prefix;
  }

private:
  static void update_log_prob(float &dest, float val) {
    if (dest == -INFINITY)
      dest = val;
    else if (val == -INFINITY)
      return;
    else {
      dest = std::max(dest, val) + log1p(exp(-std::abs(dest - val)));
    }
  }

  static std::map<std::vector<int32_t>, BeamEntry>
  prune_beams(const std::map<std::vector<int32_t>, BeamEntry> &next_beams,
              int beam_size) {

    std::vector<std::pair<std::vector<int32_t>, float>> scores;
    for (auto const &[prefix, entry] : next_beams) {
      scores.push_back({prefix, entry.score()});
    }

    int actual_beam = std::min((int)scores.size(), beam_size);
    std::nth_element(
        scores.begin(), scores.begin() + actual_beam, scores.end(),
        [](const auto &a, const auto &b) { return a.second > b.second; });

    std::map<std::vector<int32_t>, BeamEntry> pruned;
    for (int i = 0; i < actual_beam; ++i) {
      pruned[scores[i].first] = next_beams.at(scores[i].first);
    }
    return pruned;
  }
};