#!/usr/bin/env python3
import argparse
import json
import yaml
import numpy as np
import torch
from pathlib import Path
from gguf import GGUFWriter, GGMLQuantizationType

# Mapping of SenseVoice internal architecture names to GGUF strings
ARCH_MAP = {
    "SenseVoiceSmall": "SenseVoiceSmall",
    "FunASRNano": "FunASRNano"
}
def load_cmvn(mvn_path: str):
    """
    Parses the am.mvn file.
    Usually contains <AddShift> (means) and <Rescale> (vars).
    """
    means = None
    vars = None
    try:
        with open(mvn_path, 'r') as f:
            lines = f.readlines()
            for i, line in enumerate(lines):
                if "<AddShift>" in line:
                    # Extract numbers between [ ]
                    data = lines[i+1].split('[')[1].split(']')[0].split()
                    means = np.array([float(x) for x in data], dtype=np.float32)
                if "<Rescale>" in line:
                    data = lines[i+1].split('[')[1].split(']')[0].split()
                    vars = np.array([float(x) for x in data], dtype=np.float32)
        return means, vars
    except Exception as e:
        print(f"Error loading CMVN from {mvn_path}: {e}")
        return None, None

def get_tensors(model_dir: Path):
    """Loads weights from model.pt and optional silero_vad.pt"""
    checkpoints = ["model.pt", "silero_vad.pt"]
    for ckpt in checkpoints:
        path = model_dir / ckpt
        if not path.exists():
            continue

        print(f"gguf: loading weights from '{ckpt}'")
        state_dict = torch.load(str(path), map_location="cpu", weights_only=True)
        for name, data in state_dict.items():
            yield name, data

def write_tensor(writer: GGUFWriter, name: str, data_torch: torch.Tensor, ftype: int):
    """Handles data type conversion and split logic for QKV tensors"""
    # Split merged QKV linear layers as per reference
    if 'linear_q_k_v' in name:
        q_k_v = data_torch.split(data_torch.size(0) // 3)
        write_tensor(writer, name.replace('linear_q_k_v', 'linear_q'), q_k_v[0], ftype)
        write_tensor(writer, name.replace('linear_q_k_v', 'linear_k'), q_k_v[1], ftype)
        write_tensor(writer, name.replace('linear_q_k_v', 'linear_v'), q_k_v[2], ftype)
        return

    # Convert to numpy
    data = data_torch.detach().float().numpy()

    # Specific type handling for FSMN and specific VAD layers as per reference
    if 'fsmn_block.weight' in name or name.startswith('_model.'):
        data = data.astype(np.float16)
    elif ftype == GGMLQuantizationType.F16 and data.ndim == 2 and name.endswith(".weight"):
        data = data.astype(np.float16)
    else:
        data = data.astype(np.float32)

    writer.add_tensor(name, data)

def main():
    parser = argparse.ArgumentParser(description="Convert SenseVoice model to GGUF")
    parser.add_argument("--model-dir", type=str, required=True, help="Directory with config.yaml, model.pt, am.mvn")
    parser.add_argument("--output", type=str, required=True, help="Output .gguf file path")
    parser.add_argument("--out-type", type=str, choices=["f32", "f16"], default="f32")
    parser.add_argument("--without_llm", action="store_true", default=False, help="Exclude LLM weights if present")
    args = parser.parse_args()

    model_dir = Path(args.model_dir)
    ftype = GGMLQuantizationType.F32 if args.out_type == "f32" else GGMLQuantizationType.F16

    # 1. Load Config
    config_path = model_dir / "config.yaml"
    with open(config_path, "r") as f:
        hparams = yaml.safe_load(f)

    arch_name = ARCH_MAP.get(hparams.get("model", "SenseVoiceSmall"), "sensevoice")
    writer = GGUFWriter(args.output, arch_name)

    # 2. Set Parameters from YAML (Frontend & Encoder)
    fconf = hparams.get("frontend_conf", {})
    econf = hparams.get("encoder_conf", {}) if arch_name == 'SenseVoiceSmall' else hparams.get("audio_encoder_conf", {})
    if arch_name == 'FunASRNano':
        ctc_conf = hparams.get("ctc_decoder_conf", {})
        writer.add_int32("ctc.downsample_rate", ctc_conf.get("downsample_rate", 1))
        writer.add_int32("ctc.ffn_dim", ctc_conf.get("ffn_dim", 0))
        writer.add_int32("ctc.llm_dim", ctc_conf.get("llm_dim", 0))
        writer.add_int32("ctc.encoder_dim", ctc_conf.get("encoder_dim", 0))
        writer.add_int32("ctc.n_layer", ctc_conf.get("n_layer", 0))
        writer.add_int32("ctc.odim", ctc_conf.get("odim", 0))

        if not args.without_llm:
            adaptor_conf = hparams.get("audio_adaptor_conf", {})

            writer.add_int32("adaptor.ffn_dim", adaptor_conf.get("ffn_dim", 0))
            writer.add_int32("adaptor.downsample_rate", adaptor_conf.get("downsample_rate", 1))
            writer.add_int32("adaptor.llm_dim:", adaptor_conf.get("llm_dim:", 0))
            writer.add_int32("adaptor.encoder_dim", adaptor_conf.get("encoder_dim", 0))
            writer.add_int32("adaptor.n_layer", adaptor_conf.get("n_layer", 0))



    writer.add_int32("frontend.sample_rate", fconf.get("fs", 16000))
    writer.add_string("frontend.window", fconf.get("window", "hamming"))
    writer.add_int32("frontend.num_mels", fconf.get("n_mels", 80))
    writer.add_int32("frontend.lfr_m", fconf.get("lfr_m", 7))
    writer.add_int32("frontend.lfr_n", fconf.get("lfr_n", 6))

    writer.add_int32("encoder.output_size", econf.get("output_size", 512))
    writer.add_int32("encoder.attention_heads", econf.get("attention_heads", 8))
    writer.add_int32("encoder.linear_units", econf.get("linear_units", 2048))
    writer.add_int32("encoder.num_blocks", econf.get("num_blocks", 50))
    writer.add_int32("encoder.tp_blocks", econf.get("tp_blocks", 10))

    # 3. Load and Add CMVN from am.mvn
    mvn_path = model_dir / "am.mvn"
    if mvn_path.exists():
        means, vars = load_cmvn(str(mvn_path))
        if means is not None and vars is not None:
            print(f"Writing CMVN metadata from {mvn_path}...")
            # Convert NumPy arrays to lists to satisfy GGUFWriter's requirement for a sequence
            writer.add_array("model.cmvn_means", means.tolist())
            writer.add_array("model.cmvn_vars", vars.tolist())

    # 4. Set Vocabulary
    if arch_name == "SenseVoiceSmall":
        vocab_model = model_dir / "chn_jpn_yue_eng_ko_spectok.bpe.model"
        if vocab_model.exists():
            import sentencepiece as spm
            sp = spm.SentencePieceProcessor()
            sp.load(str(vocab_model))
            tokens = [sp.id_to_piece(i).replace(" ", " ") for i in range(sp.vocab_size())]
            writer.add_int32("tokenizer.vocab_size", sp.vocab_size())
            writer.add_token_list(tokens)
            writer.add_string("tokenizer.unk_symbol", "<unk>")

    elif arch_name == "FunASRNano":
        config_path = model_dir / "Qwen3-0.6B/config.yaml"
        vocab_path = model_dir / "multilingual.tiktoken"
        added_tokens = model_dir / "Qwen3-0.6B/tokenizer_config.json"

        if vocab_path.exists():

            from funasr.models.sense_voice.whisper_lib.tokenizer import get_tokenizer

            tokenizer = get_tokenizer(
                multilingual=True,
                num_languages=8479,
                vocab_path=str(vocab_path)
            )

            tokens = [tokenizer.decode([i]) for i in range(tokenizer.encoding.n_vocab)]

            with open(added_tokens, 'r', encoding='utf-8') as f:
                add_vocab = json.load(f)

            added_tokens_list = list(tokens)
            writer.add_int32("tokenizer.vocab_size", len(added_tokens_list))
            writer.add_token_list(added_tokens_list)
            writer.add_string("tokenizer.unk_symbol", "<unk>")
            writer.add_string("tokenizer.chat_template", add_vocab.get("chat_template", ""))

        if config_path.exists() and not args.without_llm:
            with open(config_path, "r") as f:
                hparams = yaml.safe_load(f)
            model_type = hparams.get("model_type", "qwen3")
            writer.add_bool(f"{model_type}.attention_bias", hparams.get("attention_bias", False))
            writer.add_int32(f"{model_type}.bos_token_id", hparams.get("bos_token_id"))
            writer.add_int32(f"{model_type}.eos_token_id", hparams.get("eos_token_id"))
            writer.add_int32(f"{model_type}.head_dim", hparams.get("head_dim"))
            writer.add_string(f"{model_type}.hidden_act", hparams.get("hidden_act"))
            writer.add_int32(f"{model_type}.hidden_size", hparams.get("hidden_size"))
            writer.add_float32(f"{model_type}.initializer_range", hparams.get("initializer_range"))
            writer.add_int32(f"{model_type}.intermediate_size", hparams.get("intermediate_size"))
            writer.add_int32(f"{model_type}.max_position_embeddings", hparams.get("max_position_embeddings", 2048))
            writer.add_int32(f"{model_type}.max_window_layers", hparams.get("max_window_layers"))
            writer.add_int32(f"{model_type}.num_attention_heads", hparams.get("num_attention_heads"))
            writer.add_int32(f"{model_type}.num_hidden_layers", hparams.get("num_hidden_layers"))
            writer.add_int32(f"{model_type}.num_key_value_heads", hparams.get("num_key_value_heads"))
            writer.add_float32(f"{model_type}.rms_norm_eps", hparams.get("rms_norm_eps", 1e-6))



    # 5. Write Tensors
    print("Writing tensors...")
    for name, data in get_tensors(model_dir):
        if not name.endswith(("Loss", "loss")):
            if args.without_llm and (name.startswith("llm.") or name.startswith("audio_adaptor.")):
                continue

            if arch_name == "FunASRNano":
                name = name.replace('audio_encoder', 'encoder')
            write_tensor(writer, name, data, ftype)

    writer.write_header_to_file()
    writer.write_kv_data_to_file()
    writer.write_tensors_to_file()
    writer.close()

    print(f"Successfully exported to {args.output}")

if __name__ == "__main__":
    main()