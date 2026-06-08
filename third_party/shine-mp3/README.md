# shine-mp3
This is a pure Go implementation of the [shine mp3 encoding library](https://github.com/toots/shine).

> shine is a blazing fast mp3 encoding library implemented in fixed-point arithmetic. The library can thus be used to perform super fast mp3 encoding on architectures without a FPU, such as armel, etc.. It is also super fast on architectures with a FPU!

AFAIK, this is the only pure Go MP3 ***encoding*** library. It produces byte-identical binaries to the original Shine C library.

The code was originally developed in [this project](https://github.com/braheezy/goqoa).

## Usage
The `main.go` file has simple example of reading WAV file and encoding it to MP3. It all comes down to this:
```go
// Create the encoder with the sample rate and number of audio channels
mp3Encoder := NewEncoder(44100, 2)
// Assuming all your audio data is in []int16 slice called decodedData, write it to a file referenced by out
mp3Encoder.Write(out, decodedData)
```

## A Quick Comment on MP3 Encoders
There is essentially one actively maintained MP3 encoding library: [LAME MP3](https://lame.sourceforge.io/). If you want to encode audio files to MP3 using a programming language, you use a library that provides bindings to LAME, which means users of your software must have the LAME MP3 C library installed. You might be able to work around this producing a 100% statically compiled binary and include the LAME files. However, that might prove challenging on all platforms, like Windows.

I found about the Shine MP3 encoder from this [list of alternative encoders on the LAME website](https://lame.sourceforge.io/links.php#Alternatives).
