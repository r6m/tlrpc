# Benchmark medians

Five samples per case. Negative time change means faster. Raw data and methodology are in [the report](README.md).

| Benchmark | ns/op before → after | Time change | B/op before → after | allocs/op before → after |
| --- | ---: | ---: | ---: | ---: |
| RuntimeApplicationDispatch/small | 1071 → 693.4 | -35.3% | 1364 → 1260 | 39 → 30 |
| RuntimeApplicationDispatch/1KiB | 1561 → 1136 | -27.2% | 5536 → 5432 | 38 → 29 |
| RuntimeApplicationDispatch/64KiB | 23843 → 20669 | -13.3% | 271648 → 271544 | 38 → 29 |
| EncodeMethodResponse/scalar | 81.76 → 41.93 | -48.7% | 168 → 176 | 6 → 2 |
| EncodeMethodResponse/vector | 348.6 → 96.49 | -72.3% | 360 → 200 | 28 → 3 |
| EncodeMethodResponse/nested_vector | 321.6 → 122 | -62.1% | 380 → 200 | 21 → 3 |
| EncodeMethodResponse/bytes/small | 134.3 → 75.16 | -44.0% | 236 → 200 | 9 → 3 |
| EncodeMethodResponse/bytes/1KiB | 254.9 → 183.8 | -27.9% | 1388 → 1288 | 9 → 3 |
| EncodeMethodResponse/bytes/64KiB | 4013 → 4894 | +22.0% | 73964 → 73864 | 9 → 3 |
| GeneratedEchoCodec/encode/small | 78.16 → 45.07 | -42.3% | 80 → 112 | 7 → 1 |
| GeneratedEchoCodec/decode/small | 77.85 → 181.7 | +133.4% | 92 → 344 | 6 → 6 |
| GeneratedEchoCodec/encode/1KiB | 163.9 → 52.86 | -67.7% | 1088 → 112 | 6 → 1 |
| GeneratedEchoCodec/decode/1KiB | 257.5 → 393.2 | +52.7% | 2104 → 2360 | 5 → 6 |
| GeneratedEchoCodec/encode/64KiB | 6141 → 687.9 | -88.8% | 65602 → 112 | 6 → 1 |
| GeneratedEchoCodec/decode/64KiB | 9025 → 10190 | +12.9% | 131128 → 131384 | 5 → 6 |
| GeneratedNestedBoxedVectorCodec/encode | 186.7 → 105.9 | -43.3% | 128 → 112 | 17 → 1 |
| GeneratedNestedBoxedVectorCodec/decode | 379.4 → 455.4 | +20.0% | 472 → 424 | 26 → 9 |
| GeneratedEchoCodecBufferPath/encode/small | 114.1 → 59.44 | -47.9% | 192 → 176 | 9 → 2 |
| GeneratedEchoCodecBufferPath/decode/small | 167 → 93.14 | -44.2% | 276 → 256 | 9 → 3 |
| GeneratedEchoCodecBufferPath/encode/1KiB | 351.3 → 186.3 | -47.0% | 2352 → 1328 | 9 → 3 |
| GeneratedEchoCodecBufferPath/decode/1KiB | 351.2 → 185.8 | -47.1% | 2288 → 1264 | 8 → 3 |
| GeneratedEchoCodecBufferPath/encode/64KiB | 9892 → 4790 | -51.6% | 139440 → 73904 | 9 → 3 |
| GeneratedEchoCodecBufferPath/decode/64KiB | 9599 → 4530 | -52.8% | 131312 → 65776 | 8 → 3 |
| GeneratedNestedBoxedVectorCodecBufferPath/encode | 227 → 135.6 | -40.3% | 240 → 176 | 19 → 2 |
| GeneratedNestedBoxedVectorCodecBufferPath/decode | 556.8 → 238.7 | -57.1% | 672 → 352 | 30 → 7 |
| GeneratedEchoCallback | 14.08 → 14.52 | +3.1% | 16 → 16 | 1 → 1 |
| DecodeEncryptedFrame/small | 690.1 → 691.5 | +0.2% | 1064 → 1000 | 19 → 18 |
| DecodeEncryptedFrame/1KiB | 2637 → 2544 | -3.5% | 4248 → 3096 | 19 → 18 |
| DecodeEncryptedFrame/64KiB | 117488 → 111350 | -5.2% | 213912 → 140184 | 19 → 18 |
