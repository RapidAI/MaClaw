package kokoro

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
)

const (
	DefaultSampleRate = 24000
	DefaultRepoID     = "hexgrad/Kokoro-82M"
)

//go:embed kokoro-v1_0_config.json
var defaultConfigJSON []byte

type Config struct {
	ISTFTNet              ISTFTNetConfig `json:"istftnet"`
	DimIn                 int            `json:"dim_in"`
	Dropout               float32        `json:"dropout"`
	HiddenDim             int            `json:"hidden_dim"`
	MaxConvDim            int            `json:"max_conv_dim"`
	MaxDur                int            `json:"max_dur"`
	Multispeaker          bool           `json:"multispeaker"`
	NLayer                int            `json:"n_layer"`
	NMels                 int            `json:"n_mels"`
	NToken                int            `json:"n_token"`
	StyleDim              int            `json:"style_dim"`
	TextEncoderKernelSize int            `json:"text_encoder_kernel_size"`
	PLBert                PLBertConfig   `json:"plbert"`
	Vocab                 map[string]int `json:"vocab"`
}

type ISTFTNetConfig struct {
	UpsampleKernelSizes []int   `json:"upsample_kernel_sizes"`
	UpsampleRates       []int   `json:"upsample_rates"`
	GenISTFTHopSize     int     `json:"gen_istft_hop_size"`
	GenISTFTNFFT        int     `json:"gen_istft_n_fft"`
	ResblockDilations   [][]int `json:"resblock_dilation_sizes"`
	ResblockKernels     []int   `json:"resblock_kernel_sizes"`
	UpsampleInitialChan int     `json:"upsample_initial_channel"`
}

type PLBertConfig struct {
	HiddenSize            int     `json:"hidden_size"`
	NumAttentionHeads     int     `json:"num_attention_heads"`
	IntermediateSize      int     `json:"intermediate_size"`
	MaxPositionEmbeddings int     `json:"max_position_embeddings"`
	NumHiddenLayers       int     `json:"num_hidden_layers"`
	Dropout               float32 `json:"dropout"`
}

func LoadConfig(path string) (*Config, error) {
	data := defaultConfigJSON
	if path != "" {
		fileData, err := os.ReadFile(path)
		if err != nil {
			if len(defaultConfigJSON) == 0 || !os.IsNotExist(err) {
				return nil, fmt.Errorf("kokoro: read config: %w", err)
			}
		} else {
			data = fileData
		}
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("kokoro: parse config: %w", err)
	}
	if cfg.NToken == 0 || len(cfg.Vocab) == 0 {
		return nil, fmt.Errorf("kokoro: invalid config: missing vocab or token count")
	}
	return &cfg, nil
}
