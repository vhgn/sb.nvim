| Benchmark | Before | After | Speedup | Mem before | Mem after | Allocs before | Allocs after |
|---|--:|--:|--:|--:|--:|--:|--:|
| Initialize/100 | 13.37 ms | 4.06 ms | **3.3×** | 2.6 MiB | 2.2 MiB | 20922 | 10711 |
| Initialize/1000 | 141.40 ms | 29.60 ms | **4.8×** | 25.8 MiB | 21.8 MiB | 204091 | 103381 |
| Initialize/5000 | 641.00 ms | 110.16 ms | **5.8×** | 128.9 MiB | 108.8 MiB | 1025526 | 516284 |
| CompletionWiki/100 | 520.5 µs | 165.9 µs | **3.1×** | 202.1 KiB | 76.9 KiB | 630 | 429 |
| CompletionWiki/1000 | 24.50 ms | 1.27 ms | **19.3×** | 2.0 MiB | 702.8 KiB | 6035 | 4032 |
| CompletionWiki/5000 | 428.05 ms | 6.55 ms | **65.4×** | 14.9 MiB | 4.1 MiB | 30059 | 20038 |
| CompletionAt/100 | 551.6 µs | 185.0 µs | **3.0×** | 204.9 KiB | 88.5 KiB | 729 | 528 |
| CompletionAt/1000 | 24.78 ms | 1.42 ms | **17.5×** | 2.1 MiB | 750.4 KiB | 7035 | 5031 |
| CompletionAt/5000 | 426.47 ms | 7.15 ms | **59.6×** | 14.1 MiB | 4.4 MiB | 35046 | 25037 |
| TypingCompletion/100 | 678.4 µs | 246.2 µs | **2.8×** | 231.0 KiB | 98.3 KiB | 840 | 539 |
| TypingCompletion/1000 | 25.05 ms | 1.37 ms | **18.3×** | 2.1 MiB | 737.2 KiB | 6261 | 4148 |
| TypingCompletion/5000 | 558.52 ms | 6.65 ms | **84.0×** | 15.9 MiB | 4.2 MiB | 30266 | 20140 |
| CompletionHeading/100 | 44.0 µs | 27.8 µs | **1.6×** | 9.5 KiB | 8.6 KiB | 97 | 94 |
| CompletionHeading/1000 | 262.0 µs | 23.8 µs | **11.0×** | 16.6 KiB | 8.6 KiB | 97 | 94 |
| CompletionHeading/5000 | 1.15 ms | 21.2 µs | **54.1×** | 48.6 KiB | 8.6 KiB | 97 | 94 |
| Definition/100 | 26.0 µs | 8.0 µs | **3.2×** | 2.3 KiB | 1.3 KiB | 32 | 27 |
| Definition/1000 | 322.6 µs | 9.7 µs | **33.1×** | 9.8 KiB | 1.8 KiB | 34 | 31 |
| Definition/5000 | 943.5 µs | 6.4 µs | **146×** | 41.4 KiB | 1.3 KiB | 29 | 26 |
| Hover/100 | 27.7 µs | 10.2 µs | **2.7×** | 3.6 KiB | 2.7 KiB | 35 | 30 |
| Hover/1000 | 333.6 µs | 15.0 µs | **22.3×** | 14.9 KiB | 6.8 KiB | 37 | 34 |
| Hover/5000 | 893.2 µs | 10.4 µs | **86.3×** | 46.8 KiB | 6.7 KiB | 32 | 29 |
| References/100 | 36.08 ms | 171.6 µs | **210×** | 1.9 MiB | 24.5 KiB | 11965 | 175 |
| References/1000 | 3.79 s | 179.4 µs | **21104×** | 129.6 MiB | 22.1 KiB | 115939 | 149 |
| References/5000 | 86.33 s | 150.5 µs | **573753×** | 3147.3 MiB | 18.7 KiB | 582908 | 152 |
| ReferencesHeading/100 | 35.41 ms | 132.5 µs | **267×** | 1.9 MiB | 11.1 KiB | 11933 | 146 |
| ReferencesHeading/1000 | 3.69 s | 163.2 µs | **22594×** | 129.8 MiB | 11.6 KiB | 115913 | 128 |
| ReferencesHeading/5000 | — | 136.3 µs | new | — | 13.0 KiB | — | 138 |
| DocumentSymbol/100 | 17.9 µs | 20.1 µs | 0.89× (slower) | 6.8 KiB | 6.8 KiB | 41 | 41 |
| DocumentSymbol/1000 | 16.6 µs | 17.4 µs | ≈ | 6.8 KiB | 6.8 KiB | 41 | 41 |
| DocumentSymbol/5000 | 15.2 µs | 15.4 µs | ≈ | 6.8 KiB | 6.8 KiB | 41 | 41 |
| WorkspaceSymbol/100 | 141.1 µs | 58.7 µs | **2.4×** | 16.2 KiB | 408 B | 1316 | 13 |
| WorkspaceSymbol/1000 | 1.41 ms | 386.4 µs | **3.7×** | 156.9 KiB | 410 B | 13016 | 13 |
| WorkspaceSymbol/5000 | 8.00 ms | 3.64 ms | **2.2×** | 814.0 KiB | 547 B | 65016 | 13 |
| Parse | 96.9 µs | 17.3 µs | **5.6×** | 16.3 KiB | 9.8 KiB | 169 | 68 |
| Scan/100 | 13.57 ms | 4.03 ms | **3.4×** | 2.6 MiB | 2.1 MiB | 20849 | 10625 |
| Scan/1000 | 138.55 ms | 29.75 ms | **4.7×** | 25.8 MiB | 21.8 MiB | 204015 | 103294 |
| Scan/5000 | 649.51 ms | 110.46 ms | **5.9×** | 128.8 MiB | 108.7 MiB | 1025450 | 516200 |
| ResolveDoc/100 | 181.0 µs | 9.6 µs | **18.8×** | 10.4 KiB | 1.9 KiB | 98 | 54 |
| ResolveDoc/1000 | 3.21 ms | 10.8 µs | **297×** | 123.6 KiB | 2.7 KiB | 111 | 52 |
| ResolveDoc/5000 | 13.88 ms | 5.7 µs | **2441×** | 562.9 KiB | 2.1 KiB | 88 | 39 |
| Backlinks/100 | 34.46 ms | 133.0 µs | **259×** | 1.8 MiB | 11.2 KiB | 11895 | 112 |
| Backlinks/1000 | 4.18 s | 176.9 µs | **23600×** | 128.3 MiB | 12.8 KiB | 115869 | 94 |
| Backlinks/5000 | — | 76.1 µs | new | — | 10.4 KiB | — | 101 |
| ShortNameAll/100 | 122.9 µs | 942 ns | **130×** | 952 B | 0 B | 3 | 0 |
| ShortNameAll/1000 | 11.28 ms | 10.0 µs | **1131×** | 8.1 KiB | 0 B | 3 | 0 |
| ShortNameAll/5000 | 359.72 ms | 70.9 µs | **5071×** | 40.1 KiB | 2 B | 3 | 0 |

| Response size | Before | After | Smaller by |
|---|--:|--:|--:|
| CompletionWiki/100 | 66.8 KiB | 30.4 KiB | **2.2×** |
| CompletionWiki/1000 | 685.5 KiB | 314.8 KiB | **2.2×** |
| CompletionWiki/5000 | 3.4 MiB | 1.6 MiB | **2.2×** |
