import argparse
import array
import struct
import time

import numpy as np
import rapidspeech


def read_wav(file_path):
    with open(file_path, "rb") as f:
        data = f.read()

    # header of wav file
    info = data[:44]
    frames = data[44:]
    (
        name,
        data_lengths,
        _,
        _,
        _,
        _,
        channels,
        sample_rate,
        bit_rate,
        block_length,
        sample_bit,
        _,
        pcm_length,
    ) = struct.unpack_from("<4sL4s4sLHHLLHH4sL", info)
    # shortArray each element is 16bit
    short_array = array.array("h")
    short_array.frombytes(data)
    data = np.array(short_array, dtype="float32") / (1 << 15)

    if channels > 1:
        data = data.reshape(-1, channels)
    return data


def main():
    parser = argparse.ArgumentParser(description="RapidSpeech ASR Python Demo")
    parser.add_argument("--model", required=True, help="模型文件路径 (.gguf)")
    parser.add_argument("--audio", required=True, help="16kHz PCM wav 文件路径")
    parser.add_argument("--threads", type=int, default=4, help="线程数")
    parser.add_argument("--gpu", type=int, default=1, help="是否使用 GPU, 1=用, 0=不用")
    args = parser.parse_args()

    # 初始化上下文
    ctx = rapidspeech.asr_offline(
        model_path=args.model, n_threads=args.threads, use_gpu=bool(args.gpu)
    )

    # 读取 wav
    pcm = read_wav(args.audio)
    pcm = np.ascontiguousarray(pcm, dtype=np.float32)
    # 推送音频
    start = time.time()
    for i in range(10):
        ctx.push_audio(pcm)
        ctx.process()
        print("识别结果:", ctx.get_text())

    print(f"平均每次处理时间: {(time.time() - start)/10}s")


if __name__ == "__main__":
    main()
